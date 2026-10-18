package request

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/NumberMan1/orbit/observe"
	"github.com/NumberMan1/orbit/rpc"
	"github.com/NumberMan1/orbit/socket"
)

// ErrorHandler encodes a safe application-specific error response. Returning nil
// keeps the session alive; send errors propagate. It must return promptly even
// for an expired ctx. Never serialize arbitrary internal error text to clients.
type ErrorHandler func(context.Context, *socket.Session, rpc.Frame, error) error
type ServerOptions struct {
	RequestIDs    []uint16
	CancelID      uint16
	Metadata      bool
	MaxConcurrent int
	Timeout       time.Duration
	OnError       ErrorHandler
	Observer      observe.Observer
}
type callKey struct {
	session  *socket.Session
	sequence uint32
}
type serverCall struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// Server accepts bounded asynchronous calls so its read loop can receive cancel
// frames. Each session's request sequences must be unique until disconnection.
// Use one Server handler per connection direction; handlers may run concurrently.
// Cancellation is cooperative and accepted work is tracked by Session.Drain.
type Server struct {
	options ServerOptions
	ids     map[uint16]bool
	mu      sync.Mutex
	calls   map[callKey]*serverCall
	async   socket.Handler
}

func NewServer(options ServerOptions, handler socket.Handler) (*Server, error) {
	if options.MaxConcurrent == 0 {
		options.MaxConcurrent = 256
	}
	if options.Timeout == 0 {
		options.Timeout = 5 * time.Second
	}
	if handler == nil || len(options.RequestIDs) == 0 || options.MaxConcurrent < 1 || options.Timeout <= 0 {
		return nil, errors.New("request: invalid server options")
	}
	s := &Server{options: options, ids: make(map[uint16]bool), calls: make(map[callKey]*serverCall)}
	for _, id := range options.RequestIDs {
		if id == 0 || id == options.CancelID || s.ids[id] {
			return nil, errors.New("request: invalid/duplicate request ID")
		}
		s.ids[id] = true
	}
	s.async, _ = socket.Async(options.MaxConcurrent, func(_ context.Context, session *socket.Session, f rpc.Frame) (err error) {
		key := callKey{session, f.Sequence}
		s.mu.Lock()
		call := s.calls[key]
		s.mu.Unlock()
		defer s.remove(key, call)
		started := time.Now()
		var outcome error
		defer func() {
			p := recover()
			if p != nil {
				outcome = errors.New("request: handler panic")
			}
			observe.Emit(options.Observer, observe.Event{Kind: observe.ServerCall, Duration: time.Since(started), MessageID: f.ID, TraceID: observe.TraceID(call.ctx), Err: outcome})
			if p != nil {
				panic(p)
			}
		}()
		err = call.ctx.Err()
		if err == nil {
			err = handler(call.ctx, session, f)
		}
		outcome = err
		if err != nil && options.OnError != nil {
			return options.OnError(call.ctx, session, f, err)
		}
		return err
	})
	return s, nil
}

func (s *Server) remove(key callKey, call *serverCall) {
	call.cancel()
	s.mu.Lock()
	if s.calls[key] == call {
		delete(s.calls, key)
	}
	s.mu.Unlock()
}

func (s *Server) Pending() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.calls) }

func (s *Server) Handler(next socket.Handler) socket.Handler {
	return func(ctx context.Context, session *socket.Session, f rpc.Frame) error {
		key := callKey{session, f.Sequence}
		if s.options.CancelID != 0 && f.ID == s.options.CancelID {
			if f.Sequence == 0 || len(f.Payload) != 0 {
				return errors.New("request: malformed cancel frame")
			}
			s.mu.Lock()
			call := s.calls[key]
			if call != nil {
				call.cancel()
			}
			s.mu.Unlock()
			return nil
		}
		if !s.ids[f.ID] {
			if next != nil {
				return next(ctx, session, f)
			}
			return nil
		}
		if f.Sequence == 0 {
			return errors.New("request: zero request sequence")
		}
		ctx, cancel := context.WithTimeout(ctx, s.options.Timeout)
		if s.options.Metadata {
			var wireCancel context.CancelFunc
			var err error
			ctx, wireCancel, f, err = unpack(ctx, f)
			if err != nil {
				cancel()
				return err
			}
			outer := cancel
			cancel = func() { wireCancel(); outer() }
		}
		call := &serverCall{ctx, cancel}
		s.mu.Lock()
		if s.calls[key] != nil {
			s.mu.Unlock()
			cancel()
			return errors.New("request: duplicate active sequence")
		}
		if len(s.calls) >= s.options.MaxConcurrent {
			s.mu.Unlock()
			cancel()
			if s.options.OnError != nil {
				return s.options.OnError(ctx, session, f, ErrFull)
			}
			return ErrFull
		}
		s.calls[key] = call
		s.mu.Unlock()
		if err := s.async(ctx, session, f); err != nil {
			s.remove(key, call)
			if s.options.OnError != nil {
				return s.options.OnError(ctx, session, f, err)
			}
			return err
		}
		return nil
	}
}
