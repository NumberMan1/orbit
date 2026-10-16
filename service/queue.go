package service

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	serverlog "github.com/NumberMan1/log"
	"github.com/NumberMan1/log/field"
)

var (
	ErrClosed = errors.New("service: queue not running")
	ErrFull   = errors.New("service: queue full")
)

// Queue serializes callbacks. Post never blocks, including from a callback.
// Stop drains accepted callbacks; callbacks must return in bounded time.
type Queue struct {
	mu              sync.Mutex
	jobs            chan func()
	done            chan struct{}
	started, closed bool
	interval        time.Duration
	update          func(time.Time)
	logger          serverlog.Logger
}

// NewQueue borrows logger; nil captures the current default logger. Configure
// its printers before construction and close its core only after Queue.Stop.
func NewQueue(capacity int, interval time.Duration, update func(time.Time), logger serverlog.Logger) (*Queue, error) {
	if capacity < 1 || interval < 0 || (update != nil && interval == 0) {
		return nil, errors.New("service: invalid queue capacity or interval")
	}
	if logger == nil {
		logger = serverlog.DefaultLogger()
	}
	return &Queue{jobs: make(chan func(), capacity), done: make(chan struct{}), interval: interval, update: update, logger: logger}, nil
}

func (q *Queue) Start(ctx context.Context) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.started || q.closed {
		return ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	q.started = true
	go q.run()
	return nil
}

func (q *Queue) Post(fn func()) error {
	if fn == nil {
		return errors.New("service: nil callback")
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.started || q.closed {
		return ErrClosed
	}
	select {
	case q.jobs <- fn:
		return nil
	default:
		return ErrFull
	}
}

// Stop must be called outside a callback; use an application cancel function there.
func (q *Queue) Stop(ctx context.Context) error {
	q.mu.Lock()
	if !q.closed {
		q.closed = true
		close(q.jobs)
		if !q.started {
			close(q.done)
		}
	}
	q.mu.Unlock()
	select {
	case <-q.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (q *Queue) run() {
	defer close(q.done)
	var ticks <-chan time.Time
	if q.update != nil {
		t := time.NewTicker(q.interval)
		defer t.Stop()
		ticks = t.C
	}
	for {
		select {
		case fn, ok := <-q.jobs:
			if !ok {
				return
			}
			q.call(fn)
		case now := <-ticks:
			q.call(func() { q.update(now) })
		}
	}
}

func (q *Queue) call(fn func()) {
	defer func() {
		if p := recover(); p != nil {
			q.logger.Error("event callback panic", field.String("panic", fmt.Sprint(p)), field.String("stack", string(debug.Stack())))
		}
	}()
	fn()
}
