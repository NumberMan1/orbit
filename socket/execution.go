package socket

import (
	"context"
	"sync"

	"github.com/NumberMan1/orbit/rpc"
)

type executionKey struct{}
type execution struct {
	session    *Session
	work       sync.WaitGroup
	mu         sync.Mutex
	err        error
	panicValue any
}

// Execute waits for handler and all Queued/Async work it accepts. Errors return
// to the owner instead of closing the session; panics are rethrown after cleanup.
// Cancellation does not release ownership until callbacks actually return.
// Call from outside the target queue, never from that queue's own callback.
func Execute(ctx context.Context, s *Session, f rpc.Frame, handler Handler) error {
	e := &execution{session: s}
	ctx = context.WithValue(ctx, executionKey{}, e)
	e.work.Add(1)
	e.run(func() error { return handler(ctx, s, f) })
	e.work.Wait()
	if e.panicValue != nil {
		panic(e.panicValue)
	}
	return e.err
}

func (e *execution) run(fn func() error) {
	defer e.work.Done()
	var err error
	defer func() {
		p := recover()
		e.mu.Lock()
		defer e.mu.Unlock()
		if e.err == nil {
			e.err = err
		}
		if e.panicValue == nil {
			e.panicValue = p
		}
	}()
	err = fn()
}

// Reserve before enqueueing so Wait cannot finish between parent and child.
func (s *Session) tracked(ctx context.Context, fn func() error) (run func(), reject func()) {
	if e, ok := ctx.Value(executionKey{}).(*execution); ok && e.session == s {
		e.work.Add(1)
		return func() { e.run(fn) }, e.work.Done
	}
	return func() {
		defer s.recoverPanic()
		if err := fn(); err != nil {
			s.close(err)
		}
	}, func() {}
}
