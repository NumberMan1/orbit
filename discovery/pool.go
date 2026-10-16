package discovery

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/NumberMan1/orbit"
)

type PoolOptions struct {
	Interval    time.Duration         // reconciliation interval, default 1s
	StopTimeout time.Duration         // total shutdown budget, default 5s
	Select      func(Descriptor) bool // optional application selection, prompt and nonblocking
}
type PoolStatus struct {
	Count     int
	LastError error
}
type peer struct {
	descriptor Descriptor
	component  orbit.Component
	started    bool
}

// Pool reconciles a Store directory into caller-created components (typically
// socket.Connectors). Store must start first. IDs, names and metadata selection
// are application-defined; Pool has no game roles, zones or routing policy.
// On an unhealthy Store it retains existing peers and reports degraded status.
type Pool struct {
	store          *Store
	factory        func(Descriptor) (orbit.Component, error)
	options        PoolOptions
	mu             sync.Mutex
	peers          map[string]peer
	used           bool
	cancel         context.CancelFunc
	done           chan struct{}
	lastError, err error
}

func NewPool(store *Store, factory func(Descriptor) (orbit.Component, error), options PoolOptions) (*Pool, error) {
	if options.Interval == 0 {
		options.Interval = time.Second
	}
	if options.StopTimeout == 0 {
		options.StopTimeout = 5 * time.Second
	}
	if store == nil || factory == nil || options.Interval < 0 || options.StopTimeout < 0 {
		return nil, errors.New("discovery: invalid pool options")
	}
	return &Pool{store: store, factory: factory, options: options, peers: make(map[string]peer), done: make(chan struct{})}, nil
}
func (p *Pool) Start(parent context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.used {
		return errors.New("discovery: pool already started or stopped")
	}
	if err := parent.Err(); err != nil {
		return err
	}
	if !p.store.Status().Ready {
		return errors.New("discovery: start Store before Pool")
	}
	ctx, cancel := context.WithCancel(parent)
	p.cancel = cancel
	p.used = true
	go p.run(ctx)
	return nil
}
func (p *Pool) Done() <-chan struct{} { return p.done }
func (p *Pool) Err() error            { p.mu.Lock(); defer p.mu.Unlock(); return p.err }
func (p *Pool) Status() PoolStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	return PoolStatus{len(p.peers), p.lastError}
}

// Get returns a borrowed component. A concurrent directory change may stop it.
// Count/Get describe managed components, not successful peer connections.
func (p *Pool) Get(id string) (orbit.Component, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	peer, ok := p.peers[id]
	return peer.component, ok
}
func (p *Pool) Stop(ctx context.Context) error {
	p.mu.Lock()
	if !p.used {
		p.used = true
		close(p.done)
	}
	if p.cancel != nil {
		p.cancel()
	}
	p.mu.Unlock()
	select {
	case <-p.done:
		return p.Err()
	default:
	}
	select {
	case <-p.done:
		return p.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (p *Pool) run(ctx context.Context) {
	defer func() {
		var terminal error
		if value := recover(); value != nil {
			terminal = fmt.Errorf("discovery: pool factory/selector panic: %v", value)
		}
		p.cancel()
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), p.options.StopTimeout)
		defer cancel()
		for _, peer := range p.snapshot() {
			terminal = errors.Join(terminal, stopPeer(cleanup, peer.component))
		}
		p.mu.Lock()
		clear(p.peers)
		p.err = terminal
		if terminal != nil {
			p.lastError = terminal
		}
		close(p.done)
		p.mu.Unlock()
	}()
	ticker := time.NewTicker(p.options.Interval)
	defer ticker.Stop()
	for {
		err := p.reconcile(ctx)
		p.mu.Lock()
		p.lastError = err
		p.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (p *Pool) snapshot() map[string]peer {
	p.mu.Lock()
	defer p.mu.Unlock()
	return maps.Clone(p.peers)
}
func copyDescriptor(d Descriptor) Descriptor { d.Metadata = maps.Clone(d.Metadata); return d }
func stopPeer(ctx context.Context, c orbit.Component) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("discovery: peer Stop panic: %v", p)
		}
	}()
	return c.Stop(ctx)
}
func (p *Pool) reconcile(ctx context.Context) error {
	if !p.store.Status().Ready {
		return errors.New("discovery: directory unavailable; retaining peers")
	}
	desired := p.store.Snapshot().Services
	for id, d := range desired {
		if p.options.Select != nil && !p.options.Select(copyDescriptor(d)) {
			delete(desired, id)
		}
	}
	var result error
	for id, old := range p.snapshot() {
		d, ok := desired[id]
		alive := old.started
		if monitored, ok := old.component.(interface{ Done() <-chan struct{} }); ok {
			select {
			case <-monitored.Done():
				alive = false
			default:
			}
		}
		if alive && ok && d.Name == old.descriptor.Name && d.Address == old.descriptor.Address && maps.Equal(d.Metadata, old.descriptor.Metadata) {
			continue
		}
		stopCtx, cancel := context.WithTimeout(ctx, p.options.StopTimeout)
		err := stopPeer(stopCtx, old.component)
		cancel()
		if err != nil {
			result = errors.Join(result, fmt.Errorf("stop %s: %w", id, err))
			continue
		}
		p.mu.Lock()
		delete(p.peers, id)
		p.mu.Unlock()
	}
	ids := slices.Sorted(maps.Keys(desired))
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return errors.Join(result, err)
		}
		if _, exists := p.Get(id); exists {
			continue
		}
		d := desired[id]
		component, err := p.factory(copyDescriptor(d))
		if err == nil && component == nil {
			err = errors.New("nil peer component")
		}
		if err != nil {
			result = errors.Join(result, fmt.Errorf("create %s: %w", id, err))
			continue
		}
		// Index before Start so even a panic is cleaned up by the run defer.
		p.mu.Lock()
		p.peers[id] = peer{descriptor: d, component: component}
		p.mu.Unlock()
		if err := component.Start(ctx); err != nil {
			stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), p.options.StopTimeout)
			cleanupErr := stopPeer(stopCtx, component)
			cancel()
			if cleanupErr == nil {
				p.mu.Lock()
				delete(p.peers, id)
				p.mu.Unlock()
			}
			result = errors.Join(result, fmt.Errorf("start %s: %w", id, err), cleanupErr)
		} else {
			p.mu.Lock()
			p.peers[id] = peer{descriptor: d, component: component, started: true}
			p.mu.Unlock()
		}
	}
	return result
}
