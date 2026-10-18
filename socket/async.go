package socket

import (
	"bytes"
	"context"
	"errors"
	"sync/atomic"

	"github.com/NumberMan1/orbit/rpc"
)

// Async runs accepted handlers concurrently, with a shared hard limit and no
// waiting queue. Install under Run (not as an independent background submitter).
// Payloads are copied, work is included in Session.Drain, and errors/panics close
// the session. Handlers must honor ctx. Same-session message order is not retained.
func Async(limit int, handler Handler) (Handler, error) {
	if limit < 1 || handler == nil {
		return nil, errors.New("socket: positive async limit and handler required")
	}
	var active atomic.Int64
	return func(ctx context.Context, s *Session, f rpc.Frame) error {
		for {
			n := active.Load()
			if n >= int64(limit) {
				return ErrCapacity
			}
			if active.CompareAndSwap(n, n+1) {
				break
			}
		}
		if !s.retainWork(true) {
			active.Add(-1)
			return ErrClosed
		}
		f.Payload = bytes.Clone(f.Payload)
		go func() {
			defer active.Add(-1)
			defer s.releaseWork()
			defer s.recoverPanic()
			if err := s.handle(handler, ctx, f); err != nil {
				s.close(err)
			}
		}()
		return nil
	}, nil
}
