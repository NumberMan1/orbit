// Package service coordinates component lifecycles and serial event execution.
package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/NumberMan1/orbit"
)

type App struct {
	components []orbit.Component
	timeout    time.Duration
	used       atomic.Bool
}

func NewApp(shutdownTimeout time.Duration, components ...orbit.Component) (*App, error) {
	if shutdownTimeout <= 0 {
		return nil, errors.New("service: positive shutdown timeout required")
	}
	for _, c := range components {
		if c == nil {
			return nil, errors.New("service: nil component")
		}
	}
	return &App{components: append([]orbit.Component(nil), components...), timeout: shutdownTimeout}, nil
}

// Run starts components in order, waits for cancellation, then stops in reverse.
// Startup failure rolls back every successfully started component.
func (a *App) Run(ctx context.Context) error {
	if !a.used.CompareAndSwap(false, true) {
		return errors.New("service: app already run")
	}
	runCtx, cancelRun := context.WithCancelCause(ctx)
	defer cancelRun(nil)
	var monitors sync.WaitGroup
	started := 0
	var result error
	for i, c := range a.components {
		if err := runCtx.Err(); err != nil {
			result = context.Cause(runCtx)
			break
		}
		if err := c.Start(runCtx); err != nil {
			result = fmt.Errorf("start component %d: %w", i, err)
			break
		}
		started++
		if monitored, ok := c.(interface {
			Done() <-chan struct{}
			Err() error
		}); ok {
			monitors.Go(func() {
				select {
				case <-runCtx.Done():
				case <-monitored.Done():
					if runCtx.Err() == nil {
						err := monitored.Err()
						if err == nil {
							err = errors.New("component exited unexpectedly")
						}
						cancelRun(fmt.Errorf("component %d: %w", i, err))
					}
				}
			})
		}
	}
	if result == nil {
		<-runCtx.Done()
		if ctx.Err() == nil {
			result = context.Cause(runCtx)
		}
	}
	cancelRun(result)
	monitors.Wait()
	// Keep values but remove the canceled run deadline for bounded cleanup.
	stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.timeout)
	defer cancel()
	for i := started - 1; i >= 0; i-- {
		if err := a.components[i].Stop(stopCtx); err != nil {
			result = errors.Join(result, fmt.Errorf("stop component %d: %w", i, err))
		}
	}
	return result
}
