package socket

import (
	"context"
	"errors"
	"math"
	"sync/atomic"
	"time"

	"github.com/NumberMan1/orbit/rpc"
)

// ErrRateLimited means the per-session inbound token bucket was exhausted.
var ErrRateLimited = errors.New("socket: inbound rate exceeded")

// RateLimit controls inbound frames before decoding or queueing. The zero value
// disables limiting; otherwise PerSecond must be finite and positive, Burst >= 1.
// Exhaustion closes the offending session; this is not a blocking throttler.
type RateLimit struct {
	PerSecond float64
	Burst     int
}

func (r RateLimit) validate() error {
	if r == (RateLimit{}) {
		return nil
	}
	if r.PerSecond <= 0 || math.IsNaN(r.PerSecond) || math.IsInf(r.PerSecond, 0) || r.Burst < 1 {
		return errors.New("socket: invalid inbound rate limit")
	}
	return nil
}

type tokenBucket struct {
	tokens float64
	last   time.Time
}

func (b *tokenBucket) allow(r RateLimit, now time.Time) bool {
	if r.PerSecond == 0 {
		return true
	}
	if b.last.IsZero() {
		b.tokens = float64(r.Burst)
		b.last = now
	}
	b.tokens = min(float64(r.Burst), b.tokens+now.Sub(b.last).Seconds()*r.PerSecond)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// LimitConcurrent rejects requests while max callbacks are executing. Place it
// INSIDE Queued so the quota covers execution rather than just enqueueing.
// Returning ErrCapacity follows Handler's normal session-close error policy.
func LimitConcurrent(max int, handler Handler) (Handler, error) {
	if max < 1 || handler == nil {
		return nil, errors.New("socket: positive concurrency limit and handler required")
	}
	var active atomic.Int64
	return func(ctx context.Context, s *Session, f rpc.Frame) error {
		for {
			n := active.Load()
			if n >= int64(max) {
				return ErrCapacity
			}
			if active.CompareAndSwap(n, n+1) {
				break
			}
		}
		defer active.Add(-1)
		return handler(ctx, s, f)
	}, nil
}
