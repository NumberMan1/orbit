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
	return a.run(ctx, false)
}

// RunGraceful retains component contexts during normal shutdown: all Quiesce
// hooks run first, then Drain hooks in reverse dependency order, then cancellation
// and Stop. One timeout bounds the entire shutdown. Startup cancellation and
// component failure still abort immediately. An already-canceled context starts
// no components. Custom hooks must honor their budget.
// Components without Drain retain the existing Stop contract.
func (a *App) RunGraceful(ctx context.Context) error { return a.run(ctx, true) }

// Quiescer stops admission promptly without waiting for in-flight work.
type Quiescer interface{ Quiesce() }

// Drainer finishes accepted work within ctx while dependencies remain alive.
type Drainer interface{ Drain(context.Context) error }

func (a *App) run(ctx context.Context, graceful bool) error {
	if !a.used.CompareAndSwap(false, true) {
		return errors.New("service: app already run")
	}
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	parent := ctx
	if graceful {
		parent = context.WithoutCancel(ctx)
	}
	runCtx, cancelRun := context.WithCancelCause(parent)
	defer cancelRun(nil)
	stopStartup := context.AfterFunc(ctx, func() { cancelRun(context.Cause(ctx)) })
	defer stopStartup()
	monitorCtx, stopMonitor := context.WithCancel(context.Background())
	defer stopMonitor()
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
				case <-monitorCtx.Done():
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
	stopStartup()
	if result == nil {
		select {
		case <-ctx.Done():
		case <-runCtx.Done():
		}
		if ctx.Err() == nil {
			result = context.Cause(runCtx)
		}
	}
	stopMonitor()
	monitors.Wait()
	// Keep values but remove the canceled run deadline for bounded cleanup.
	stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.timeout)
	defer cancel()
	if graceful && result == nil && runCtx.Err() == nil {
		for i := started - 1; i >= 0; i-- {
			if q, ok := a.components[i].(Quiescer); ok {
				q.Quiesce()
			}
		}
		for i := started - 1; i >= 0; i-- {
			if d, ok := a.components[i].(Drainer); ok {
				if err := d.Drain(stopCtx); err != nil {
					result = errors.Join(result, fmt.Errorf("drain component %d: %w", i, err))
				}
			}
		}
	}
	cancelRun(result)
	for i := started - 1; i >= 0; i-- {
		if err := a.components[i].Stop(stopCtx); err != nil {
			result = errors.Join(result, fmt.Errorf("stop component %d: %w", i, err))
		}
	}
	return result
}
