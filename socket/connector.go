package socket

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/NumberMan1/orbit/rpc"
)

var ErrDisconnected = errors.New("socket: connector disconnected")

type Dialer func(context.Context) (*Session, error)
type ConnectorOptions struct {
	RetryMin, RetryMax time.Duration
	StableFor          time.Duration // successful lifetime before resetting backoff; default 30s
}
type ConnectorStatus struct {
	Connected bool
	Attempts  uint64
	LastError error
}

// Connector manages fresh sessions across retries for either TCP or WebSocket.
// Start begins dialing asynchronously; WaitSession observes actual connection.
// Dial and session handlers must honor cancellation. No messages are replayed.
type Connector struct {
	dial     Dialer
	handler  Handler
	options  ConnectorOptions
	mu       sync.Mutex
	used     bool
	stopping bool
	cancel   context.CancelFunc
	session  *Session
	status   ConnectorStatus
	err      error
	changed  chan struct{}
	done     chan struct{}
}

func NewConnector(dial Dialer, handler Handler, options ConnectorOptions) (*Connector, error) {
	if options.RetryMin == 0 {
		options.RetryMin = 100 * time.Millisecond
	}
	if options.RetryMax == 0 {
		options.RetryMax = 5 * time.Second
	}
	if options.StableFor == 0 {
		options.StableFor = 30 * time.Second
	}
	if dial == nil || handler == nil || options.RetryMin < 0 || options.RetryMax < options.RetryMin || options.StableFor < 0 {
		return nil, errors.New("socket: invalid connector options")
	}
	return &Connector{dial: dial, handler: handler, options: options, changed: make(chan struct{}), done: make(chan struct{})}, nil
}
func (c *Connector) Start(parent context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.used {
		return errors.New("socket: connector already started or stopped")
	}
	if err := parent.Err(); err != nil {
		return err
	}
	c.used = true
	ctx, cancel := context.WithCancel(parent)
	c.cancel = cancel
	go c.run(ctx)
	return nil
}
func (c *Connector) Stop(ctx context.Context) error {
	c.mu.Lock()
	c.stopping = true
	c.status.Connected = false
	if !c.used {
		c.used = true
		close(c.done)
		c.signal()
	}
	if c.cancel != nil {
		c.cancel()
	}
	c.mu.Unlock()
	select {
	case <-c.done:
		return nil
	default:
	}
	select {
	case <-c.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (c *Connector) Done() <-chan struct{}   { return c.done }
func (c *Connector) Err() error              { c.mu.Lock(); defer c.mu.Unlock(); return c.err }
func (c *Connector) Status() ConnectorStatus { c.mu.Lock(); defer c.mu.Unlock(); return c.status }
func (c *Connector) Session() (*Session, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.session, !c.stopping && c.session != nil && c.session.Context().Err() == nil
}
func (c *Connector) Send(f rpc.Frame) error {
	s, ok := c.Session()
	if !ok {
		return ErrDisconnected
	}
	return s.Send(f)
}
func (c *Connector) WaitSession(ctx context.Context) (*Session, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		c.mu.Lock()
		s, changed := c.session, c.changed
		stopping := c.stopping
		c.mu.Unlock()
		if stopping {
			return nil, ErrClosed
		}
		if s != nil && s.Context().Err() == nil {
			return s, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-c.done:
			return nil, ErrClosed
		case <-changed:
		}
	}
}
func (c *Connector) signal() { close(c.changed); c.changed = make(chan struct{}) } // mu held
func (c *Connector) run(ctx context.Context) {
	defer func() {
		c.cancel()
		c.mu.Lock()
		if p := recover(); p != nil {
			c.err = fmt.Errorf("socket: connector panic: %v", p)
			c.status.LastError = c.err
		}
		c.session = nil
		c.status.Connected = false
		c.signal()
		close(c.done)
		c.mu.Unlock()
	}()
	delay := c.options.RetryMin
	for ctx.Err() == nil {
		c.mu.Lock()
		c.status.Attempts++
		c.mu.Unlock()
		s, err := c.dial(ctx)
		if err == nil && s == nil {
			err = errors.New("socket: dial returned nil session")
		}
		if err == nil {
			started := time.Now()
			func() {
				defer s.Close()
				// Enforce owner cancellation even if a custom dialer used a different parent.
				stop := context.AfterFunc(ctx, s.Close)
				defer stop()
				c.mu.Lock()
				c.session = s
				c.status.Connected = !c.stopping && ctx.Err() == nil
				c.status.LastError = nil
				c.signal()
				c.mu.Unlock()
				err = s.Run(c.handler)
			}()
			if time.Since(started) >= c.options.StableFor {
				delay = c.options.RetryMin
			}
		} else if s != nil {
			s.Close()
		}
		c.mu.Lock()
		c.session = nil
		c.status.Connected = false
		c.status.LastError = err
		c.signal()
		c.mu.Unlock()
		if ctx.Err() != nil {
			return
		}
		room := min(delay/2, c.options.RetryMax-delay)
		wait := delay
		if room > 0 {
			wait += time.Duration(rand.Int64N(int64(room) + 1))
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if delay > c.options.RetryMax/2 {
			delay = c.options.RetryMax
		} else {
			delay *= 2
		}
	}
}
