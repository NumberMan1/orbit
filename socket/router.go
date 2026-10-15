package socket

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/NumberMan1/orbit/rpc"
)

// Middleware wraps handlers; the first supplied wrapper is the outermost.
type Middleware func(Handler) Handler

func Chain(handler Handler, middleware ...Middleware) Handler {
	for i := len(middleware) - 1; i >= 0; i-- {
		handler = middleware[i](handler)
	}
	return handler
}

type Router struct {
	mu       sync.RWMutex
	handlers map[uint16]Handler
}

func (r *Router) Register(id uint16, h Handler) error {
	if id == 0 || h == nil {
		return errors.New("socket: nonzero ID and handler required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.handlers[id]; exists {
		return fmt.Errorf("socket: duplicate handler %d", id)
	}
	if r.handlers == nil {
		r.handlers = make(map[uint16]Handler)
	}
	r.handlers[id] = h
	return nil
}

func (r *Router) Handle(ctx context.Context, s *Session, f rpc.Frame) error {
	r.mu.RLock()
	h := r.handlers[f.ID]
	r.mu.RUnlock()
	if h == nil {
		return fmt.Errorf("socket: unknown message ID %d", f.ID)
	}
	return h(ctx, s, f)
}
