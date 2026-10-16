package discovery

import (
	"context"
	"errors"
	"maps"
	"math/rand/v2"
	"sync"
	"time"
)

type RegistrarOptions struct {
	TTL                int64
	RetryMin, RetryMax time.Duration
}
type RegistrarStatus struct {
	Ready     bool
	LastError error
}

// Registrar is a single-use Component that reacquires a lost lease with bounded
// jittered retries. It uses Register's exclusive ID transaction on EVERY attempt.
// The descriptor is fixed for this component's lifetime. Client is borrowed.
type Registrar struct {
	registry   *Registry
	descriptor Descriptor
	options    RegistrarOptions
	mu         sync.Mutex
	used       bool
	stopping   bool
	cancel     context.CancelFunc
	status     RegistrarStatus
	err        error
	done       chan struct{}
}

func (r *Registry) NewRegistrar(d Descriptor, o RegistrarOptions) (*Registrar, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	if o.TTL == 0 {
		o.TTL = 10
	}
	if o.RetryMin == 0 {
		o.RetryMin = 100 * time.Millisecond
	}
	if o.RetryMax == 0 {
		o.RetryMax = 5 * time.Second
	}
	if o.TTL < 3 || o.RetryMin < 0 || o.RetryMax < o.RetryMin {
		return nil, errors.New("discovery: invalid registrar options")
	}
	d.Metadata = maps.Clone(d.Metadata)
	return &Registrar{registry: r, descriptor: d, options: o, done: make(chan struct{})}, nil
}
func (r *Registrar) Start(parent context.Context) error {
	r.mu.Lock()
	if r.used {
		r.mu.Unlock()
		return errors.New("discovery: registrar already started or stopped")
	}
	r.used = true
	ctx, cancel := context.WithCancel(parent)
	r.cancel = cancel
	r.mu.Unlock()
	registration, err := r.registry.Register(ctx, r.descriptor, r.options.TTL)
	if err != nil {
		cancel()
		r.mu.Lock()
		r.status.LastError = err
		r.err = err
		close(r.done)
		r.mu.Unlock()
		return err
	}
	r.mu.Lock()
	r.status.Ready = ctx.Err() == nil && !r.stopping
	r.mu.Unlock()
	go r.run(ctx, registration)
	return nil
}
func (r *Registrar) Done() <-chan struct{}   { return r.done }
func (r *Registrar) Err() error              { r.mu.Lock(); defer r.mu.Unlock(); return r.err }
func (r *Registrar) Status() RegistrarStatus { r.mu.Lock(); defer r.mu.Unlock(); return r.status }
func (r *Registrar) Stop(ctx context.Context) error {
	r.mu.Lock()
	r.stopping = true
	r.status.Ready = false
	if !r.used {
		r.used = true
		close(r.done)
	}
	if r.cancel != nil {
		r.cancel()
	}
	r.mu.Unlock()
	select {
	case <-r.done:
		return r.Err()
	default:
	}
	select {
	case <-r.done:
		return r.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (r *Registrar) run(ctx context.Context, registration *Registration) {
	defer func() { r.cancel(); r.mu.Lock(); r.status.Ready = false; close(r.done); r.mu.Unlock() }()
	delay := r.options.RetryMin
	for {
		if registration != nil {
			select {
			case <-ctx.Done():
				cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 4*time.Second)
				err := registration.Close(cleanup)
				cancel()
				r.mu.Lock()
				r.err = err
				r.mu.Unlock()
				return
			case <-registration.Done():
			}
			r.mu.Lock()
			r.status = RegistrarStatus{LastError: registration.Err()}
			r.mu.Unlock()
			registration = nil
		}
		if ctx.Err() != nil {
			return
		}
		room := min(delay/2, r.options.RetryMax-delay)
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
		var err error
		registration, err = r.registry.Register(ctx, r.descriptor, r.options.TTL)
		r.mu.Lock()
		r.status = RegistrarStatus{Ready: err == nil && ctx.Err() == nil && !r.stopping, LastError: err}
		r.mu.Unlock()
		if err == nil {
			delay = r.options.RetryMin
		} else if delay > r.options.RetryMax/2 {
			delay = r.options.RetryMax
		} else {
			delay *= 2
		}
	}
}
