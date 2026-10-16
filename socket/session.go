// Package socket provides transport-independent sessions and message routing.
package socket

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	serverlog "github.com/NumberMan1/log"
	"github.com/NumberMan1/log/field"
	"github.com/NumberMan1/orbit/rpc"
)

var (
	ErrClosed    = errors.New("socket: session closed")
	ErrQueueFull = errors.New("socket: send queue full")
)

// Transport has one reader and one writer. Close must unblock both.
type Transport interface {
	ReadFrame(int) (rpc.Frame, error)
	WriteFrame(rpc.Frame, int) error
	SetReadDeadline(time.Time) error
	SetWriteDeadline(time.Time) error
	Close() error
}

type Options struct {
	// IDGenerator optionally supplies nonzero IDs (for example a deployment's
	// distributed ID generator). It must be concurrent-safe and return unique IDs.
	// The default counter is process-local, not globally unique across servers.
	IDGenerator  func() (uint64, error)
	Network      NetworkOptions
	MaxPayload   int
	SendQueue    int
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	// Logger is borrowed. Nil captures serverlog.DefaultLogger at construction;
	// configure its printers first. The caller closes the core after all sessions.
	Logger serverlog.Logger
	// OnOpen runs before the first inbound frame. OnClose runs after both I/O
	// loops exit. Hooks are serial per session and concurrent across sessions.
	OnOpen  func(context.Context, *Session) error
	OnClose func(context.Context, *Session, error)
	// BeforeSend transforms an owned frame before enqueueing. It can run
	// concurrently across Send callers; it must return promptly and not call Send.
	BeforeSend func(context.Context, *Session, rpc.Frame) (rpc.Frame, error)
}

func (o Options) Normalize() (Options, error) {
	var err error
	o.Network, err = o.Network.normalized()
	if err != nil {
		return o, err
	}
	if o.MaxPayload == 0 {
		o.MaxPayload = rpc.MaxPayload
	}
	if o.SendQueue == 0 {
		o.SendQueue = 256
	}
	if o.WriteTimeout == 0 {
		o.WriteTimeout = 10 * time.Second
	}
	if o.Logger == nil {
		o.Logger = serverlog.DefaultLogger()
	}
	if err := rpc.ValidateLimit(o.MaxPayload); err != nil {
		return o, err
	}
	if o.SendQueue < 1 || o.ReadTimeout < 0 || o.WriteTimeout < 0 {
		return o, errors.New("socket: invalid queue or timeout")
	}
	return o, nil
}

// Handler runs serially within one session, concurrently across sessions.
// A non-nil error closes the session. It must honor ctx and return promptly.
type Handler func(context.Context, *Session, rpc.Frame) error

var nextID atomic.Uint64

type Session struct {
	id        uint64
	transport Transport
	options   Options
	queue     chan rpc.Frame
	ctx       context.Context
	cancel    context.CancelCauseFunc
	mu        sync.Mutex
	closed    bool
	values    map[string]any
	used      atomic.Bool
	done      chan struct{}
}

func NewSession(parent context.Context, transport Transport, options Options) (*Session, error) {
	o, err := options.Normalize()
	if err != nil {
		return nil, err
	}
	if transport == nil {
		return nil, errors.New("socket: nil transport")
	}
	id, err := sessionID(o.IDGenerator)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancelCause(parent)
	return &Session{id: id, transport: transport, options: o, queue: make(chan rpc.Frame, o.SendQueue), ctx: ctx, cancel: cancel, done: make(chan struct{})}, nil
}

func sessionID(generator func() (uint64, error)) (id uint64, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("socket: ID generator panic: %v", p)
		}
	}()
	if generator == nil {
		id = nextID.Add(1)
	} else {
		id, err = generator()
	}
	if err == nil && id == 0 {
		err = errors.New("socket: zero session ID")
	}
	return
}

func (s *Session) ID() uint64               { return s.id }
func (s *Session) Context() context.Context { return s.ctx }

// Done closes when Run returns, after all I/O and hooks finish. If Run has not
// been called, use Context().Done() to observe Close instead.
func (s *Session) Done() <-chan struct{} { return s.done }

// Send copies the payload. Success means queued, not acknowledged by the peer.
// There is no replay across reconnections and no silent drop on a full queue.
func (s *Session) Send(f rpc.Frame) error {
	if s.options.BeforeSend != nil {
		var err error
		f, err = s.transformSend(f)
		if err != nil {
			return err
		}
	}
	if len(f.Payload) > s.options.MaxPayload {
		return rpc.ErrTooLarge
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.ctx.Err() != nil {
		return ErrClosed
	}
	f.Payload = bytes.Clone(f.Payload)
	select {
	case s.queue <- f:
		return nil
	default:
		return ErrQueueFull
	}
}

func (s *Session) transformSend(f rpc.Frame) (out rpc.Frame, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("socket: send hook panic: %v", p)
		}
	}()
	if err := s.ctx.Err(); err != nil {
		return rpc.Frame{}, ErrClosed
	}
	f.Payload = bytes.Clone(f.Payload)
	return s.options.BeforeSend(s.ctx, s, f)
}

// SetValue stores per-session application state without reflection or global
// tables. The map is synchronized; mutable stored objects remain caller-owned.
func (s *Session) SetValue(key string, value any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.ctx.Err() != nil {
		return ErrClosed
	}
	if s.values == nil {
		s.values = make(map[string]any)
	}
	s.values[key] = value
	return nil
}
func (s *Session) Value(key string) (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.values[key]
	return v, ok
}
func (s *Session) DeleteValue(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, key)
}

func (s *Session) Close() { s.close(ErrClosed) }

func (s *Session) close(cause error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.cancel(cause)
	s.mu.Unlock()
	_ = s.transport.Close()
}

// Run owns the transport until it returns and may be called exactly once.
// Close aborts pending writes; application-level acknowledgments are separate.
func (s *Session) Run(handler Handler) error {
	if !s.used.CompareAndSwap(false, true) {
		return errors.New("socket: session already run")
	}
	defer close(s.done)
	stop := context.AfterFunc(s.ctx, func() { s.close(context.Cause(s.ctx)) })
	defer stop()
	var wg sync.WaitGroup
	wg.Go(func() {
		defer s.recoverPanic()
		for {
			select {
			case <-s.ctx.Done():
				return
			case f := <-s.queue:
				if s.ctx.Err() != nil {
					return
				}
				if err := s.transport.SetWriteDeadline(deadline(s.options.WriteTimeout)); err != nil {
					s.close(err)
					return
				}
				if err := s.transport.WriteFrame(f, s.options.MaxPayload); err != nil {
					s.close(err)
					return
				}
			}
		}
	})
	func() {
		defer s.recoverPanic()
		if s.options.OnOpen != nil && s.ctx.Err() == nil {
			if err := s.options.OnOpen(s.ctx, s); err != nil {
				s.close(err)
				return
			}
		}
		for s.ctx.Err() == nil {
			if err := s.transport.SetReadDeadline(deadline(s.options.ReadTimeout)); err != nil {
				s.close(err)
				return
			}
			f, err := s.transport.ReadFrame(s.options.MaxPayload)
			if err != nil {
				s.close(err)
				return
			}
			if handler != nil {
				if err := handler(s.ctx, s, f); err != nil {
					s.close(err)
					return
				}
			}
		}
	}()
	s.close(context.Cause(s.ctx))
	wg.Wait()
	if s.options.OnClose != nil {
		func() { defer s.recoverPanic(); s.options.OnClose(s.ctx, s, context.Cause(s.ctx)) }()
	}
	s.mu.Lock()
	clear(s.values)
	s.mu.Unlock()
	return context.Cause(s.ctx)
}

func (s *Session) recoverPanic() {
	if p := recover(); p != nil {
		s.options.Logger.Error("session panic", field.Uint64("id", s.id), field.String("panic", fmt.Sprint(p)), field.String("stack", string(debug.Stack())))
		s.close(fmt.Errorf("socket: panic: %v", p))
	}
}

func deadline(d time.Duration) time.Time {
	if d == 0 {
		return time.Time{}
	}
	return time.Now().Add(d)
}

// Heartbeat blocks until cancellation or a send error; run it under your lifecycle.
// The factory supplies application messages without reserved business IDs.
func (s *Session) Heartbeat(ctx context.Context, interval time.Duration, frame func() rpc.Frame) error {
	if interval <= 0 || frame == nil {
		return errors.New("socket: invalid heartbeat")
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.ctx.Done():
			return context.Cause(s.ctx)
		case <-ticker.C:
			if err := s.Send(frame()); err != nil {
				return err
			}
		}
	}
}
