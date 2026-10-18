// Package util provides compatibility entries for numbox and Orbit log adapters.
package util

import (
	"context"
	numtime "github.com/NumberMan1/numbox/pkg/time"
	"time"
)

// Every runs callbacks serially until cancellation; slow callbacks coalesce ticks.
func Every(ctx context.Context, interval time.Duration, fn func(context.Context, time.Time) error) error {
	return numtime.Every(ctx, interval, fn)
}

// After waits once; cancellation never waits for the delay to expire.
func After(ctx context.Context, delay time.Duration, fn func(context.Context) error) error {
	return numtime.After(ctx, delay, fn)
}
