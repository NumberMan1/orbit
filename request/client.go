// Package request adds bounded request/response correlation to a socket session.
// Application response IDs and payload/error formats remain explicit.
package request

import (
	"bytes"
	"context"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/NumberMan1/orbit/observe"
	"github.com/NumberMan1/orbit/rpc"
	"github.com/NumberMan1/orbit/socket"
)

var (
	// ErrClosed means the client was closed or its bound session disconnected.
	ErrClosed = errors.New("request: closed")
	// ErrFull means MaxPending calls are already outstanding.
	ErrFull = errors.New("request: pending limit reached")
	// ErrSequenceExhausted requires a fresh session/client, preventing ID reuse.
	ErrSequenceExhausted = errors.New("request: sequence exhausted")
	// ErrResponseID means the response type does not match the pending call.
	ErrResponseID = errors.New("request: unexpected response ID")
)

// Options bounds memory and waiting. ResponseIDs must be nonempty and distinct
// from inbound request/push IDs; these IDs are consumed even for late responses.
type Options struct {
	// Metadata wraps request bodies with versioned deadline/correlation metadata.
	// Both endpoints must enable it explicitly. Response bodies are unchanged.
	Metadata bool
	// RawRequestIDs exempts negotiated control requests (for example heartbeat)
	// from metadata wrapping while sharing this client's sequence space.
	RawRequestIDs []uint16
	// CancelID is a dedicated, negotiated control ID; zero disables remote cancel.
	// Cancellation is best effort and never retried if the send queue is full.
	CancelID    uint16
	Observer    observe.Observer
	MaxPending  int           // zero defaults to 256
	Timeout     time.Duration // zero defaults to 5s; applied even without a caller deadline
	ResponseIDs []uint16
}
type result struct {
	frame rpc.Frame
	err   error
}
type pending struct {
	responseID uint16
	done       chan result
}

// Client borrows a single session. Use exactly one Client per session and run
// that session with Handler; create a NEW
// Client on every reconnect. No requests are replayed or retried automatically.
type Client struct {
	session                                  *socket.Session
	options                                  Options
	mu                                       sync.Mutex
	responses                                map[uint16]bool
	raw                                      map[uint16]bool
	pending                                  map[uint32]*pending
	next                                     uint64
	closed                                   error
	stop                                     func() bool
	completed, expired, late, cancelFailures uint64
}

// New validates configuration and arranges immediate cleanup on disconnection.
// Close must be called when no longer needed; it does not close the borrowed session.
func New(session *socket.Session, options Options) (*Client, error) {
	if options.MaxPending == 0 {
		options.MaxPending = 256
	}
	if options.Timeout == 0 {
		options.Timeout = 5 * time.Second
	}
	if session == nil || options.MaxPending < 1 || options.Timeout < 0 || len(options.ResponseIDs) == 0 {
		return nil, errors.New("request: invalid options")
	}
	c := &Client{session: session, options: options, responses: make(map[uint16]bool), pending: make(map[uint32]*pending)}
	for _, id := range options.ResponseIDs {
		if id == 0 || c.responses[id] || id == options.CancelID {
			return nil, errors.New("request: invalid/duplicate response ID")
		}
		c.responses[id] = true
	}
	c.raw = make(map[uint16]bool)
	for _, id := range options.RawRequestIDs {
		if id == 0 || id == options.CancelID || c.responses[id] || c.raw[id] {
			return nil, errors.New("request: invalid raw request IDs")
		}
		c.raw[id] = true
	}
	c.stop = context.AfterFunc(session.Context(), func() { c.fail(errors.Join(ErrClosed, context.Cause(session.Context()))) })
	session.AllowDuringDrain(options.ResponseIDs...)
	return c, nil
}

// Status exposes bounded call pressure and cumulative outcomes.
type Status struct {
	Pending                  int
	Completed, Expired, Late uint64
	Closed                   bool
	CancelFailures           uint64
}

// Status returns a coherent snapshot; Expired counts caller cancellations/timeouts.
func (c *Client) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Status{len(c.pending), c.completed, c.expired, c.late, c.closed != nil, c.cancelFailures}
}

// Call assigns a session-unique sequence and waits for expectedResponseID. A nil
// error means a matching frame arrived, not that application processing succeeded.
// Never call it on the same session's synchronous read handler: that blocks replies.
func (c *Client) Call(parent context.Context, frame rpc.Frame, expectedResponseID uint16) (response rpc.Frame, callErr error) {
	finish, err := c.session.TrackWork()
	if err != nil {
		if errors.Is(err, socket.ErrClosed) {
			err = errors.Join(ErrClosed, err)
		}
		return rpc.Frame{}, err
	}
	defer finish()
	ctx, cancel := context.WithTimeout(parent, c.options.Timeout)
	defer cancel()
	started := time.Now()
	if c.options.Metadata && !c.raw[frame.ID] {
		var err error
		ctx, err = observe.EnsureTrace(ctx)
		if err != nil {
			return rpc.Frame{}, err
		}
	}
	defer func() {
		observe.Emit(c.options.Observer, observe.Event{Kind: observe.ClientCall, Duration: time.Since(started), MessageID: frame.ID, TraceID: observe.TraceID(ctx), Err: callErr})
	}()
	if frame.ID == 0 || frame.ID == c.options.CancelID || c.responses[frame.ID] {
		return rpc.Frame{}, errors.New("request: request ID conflicts with control/response IDs")
	}
	if err := ctx.Err(); err != nil {
		return rpc.Frame{}, err
	}
	if c.options.Metadata && !c.raw[frame.ID] {
		var err error
		frame, err = pack(ctx, frame)
		if err != nil {
			return rpc.Frame{}, err
		}
	}
	c.mu.Lock()
	if c.closed != nil {
		err := c.closed
		c.mu.Unlock()
		return rpc.Frame{}, err
	}
	if !c.responses[expectedResponseID] {
		c.mu.Unlock()
		return rpc.Frame{}, ErrResponseID
	}
	if len(c.pending) >= c.options.MaxPending {
		c.mu.Unlock()
		return rpc.Frame{}, ErrFull
	}
	if c.next >= math.MaxUint32 {
		c.mu.Unlock()
		return rpc.Frame{}, ErrSequenceExhausted
	}
	c.next++
	frame.Sequence = uint32(c.next)
	p := &pending{expectedResponseID, make(chan result, 1)}
	c.pending[frame.Sequence] = p
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		if c.pending[frame.Sequence] == p {
			delete(c.pending, frame.Sequence)
		}
		c.mu.Unlock()
	}()
	if err := c.session.Send(frame); err != nil {
		return rpc.Frame{}, err
	}
	select {
	case r := <-p.done:
		return r.frame, r.err
	case <-ctx.Done():
		c.mu.Lock()
		c.expired++
		c.mu.Unlock()
		if c.options.CancelID != 0 {
			if err := c.session.Send(rpc.Frame{ID: c.options.CancelID, Sequence: frame.Sequence}); err != nil {
				c.mu.Lock()
				c.cancelFailures++
				c.mu.Unlock()
			}
		}
		return rpc.Frame{}, ctx.Err()
	case <-c.session.Context().Done():
		return rpc.Frame{}, errors.Join(ErrClosed, context.Cause(c.session.Context()))
	}
}

// Handler consumes configured response IDs and passes all other frames to next.
// Unknown/late response sequences are ignored. Nil next ignores unsolicited frames.
func (c *Client) Handler(next socket.Handler) socket.Handler {
	return func(ctx context.Context, s *socket.Session, frame rpc.Frame) error {
		if s != c.session {
			return errors.New("request: handler bound to another session")
		}
		if !c.responses[frame.ID] {
			if next != nil {
				return next(ctx, s, frame)
			}
			return nil
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		p := c.pending[frame.Sequence]
		if p == nil {
			c.late++
			return nil
		}
		delete(c.pending, frame.Sequence)
		if p.responseID != frame.ID {
			p.done <- result{err: ErrResponseID}
			return nil
		}
		frame.Payload = bytes.Clone(frame.Payload)
		c.completed++
		p.done <- result{frame: frame}
		return nil
	}
}

func (c *Client) fail(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed != nil {
		return
	}
	c.closed = err
	for seq, p := range c.pending {
		delete(c.pending, seq)
		p.done <- result{err: err}
	}
}

// Close fails pending calls and removes the disconnect hook; it is idempotent.
func (c *Client) Close() { c.stop(); c.fail(ErrClosed) }
