package discovery

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math/rand/v2"
	"runtime/debug"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	serverlog "github.com/NumberMan1/log"
	"github.com/NumberMan1/log/field"
)

type StoreOptions struct {
	RequestTimeout time.Duration    // List deadline, default 5s
	ResyncInterval time.Duration    // full verification even on idle watches, default 30s
	MaxStaleness   time.Duration    // Ready expires without verified data, default 1m
	RetryMin       time.Duration    // default 100ms
	RetryMax       time.Duration    // default 5s, inclusive of random jitter
	Logger         serverlog.Logger // borrowed; nil captures the current configured default
	Observer       func(StoreEvent) // serial, prompt; must not call Stop and wait for itself
}

func (o StoreOptions) normalize() (StoreOptions, error) {
	if o.RequestTimeout == 0 {
		o.RequestTimeout = 5 * time.Second
	}
	if o.ResyncInterval == 0 {
		o.ResyncInterval = 30 * time.Second
	}
	if o.MaxStaleness == 0 {
		o.MaxStaleness = time.Minute
	}
	if o.RetryMin == 0 {
		o.RetryMin = 100 * time.Millisecond
	}
	if o.RetryMax == 0 {
		o.RetryMax = 5 * time.Second
	}
	if o.Logger == nil {
		o.Logger = serverlog.DefaultLogger()
	}
	if o.RequestTimeout < 0 || o.ResyncInterval < 0 || o.MaxStaleness <= o.ResyncInterval || o.RetryMin < 0 || o.RetryMax < o.RetryMin {
		return o, errors.New("discovery: invalid timeouts, staleness or retry bounds")
	}
	return o, nil
}

// Status describes availability, independently of whether cached data exists.
// Ready means a recent verified snapshot and no known synchronization failure;
// it does not guarantee that every listed service is reachable.
type Status struct {
	Revision  int64
	Count     int
	LastSync  time.Time
	LastError error
	Running   bool
	Ready     bool
}

type StoreEvent struct {
	Kind   string // loaded, updated, progress, retry, stopped
	Status Status
}

type storeState struct {
	snapshot  *Snapshot
	lastSync  time.Time
	lastError error
	terminal  error
	running   bool
}

// Store is a single-use, managed read cache implementing orbit.Component.
// Backend/client ownership stays with the caller. Read APIs return owned copies.
// Invalid batches and reload failures preserve the last complete valid snapshot.
type Store struct {
	backend     Backend
	options     StoreOptions
	state       atomic.Pointer[storeState]
	stateMu     sync.Mutex
	lifecycleMu sync.Mutex
	used        bool
	cancel      context.CancelFunc
	stopping    atomic.Bool
	done        chan struct{}
}

func NewStore(backend Backend, options StoreOptions) (*Store, error) {
	if backend == nil {
		return nil, errors.New("discovery: backend required")
	}
	o, err := options.normalize()
	if err != nil {
		return nil, err
	}
	s := &Store{backend: backend, options: o, done: make(chan struct{})}
	s.state.Store(&storeState{snapshot: &Snapshot{Services: make(map[string]Descriptor)}})
	return s, nil
}

// Start performs a bounded initial List before returning, then supervises watch
// and reload failures in the background. A failed Start cannot be retried on the
// same Store; create another Store with the same caller-owned Backend instead.
func (s *Store) Start(parent context.Context) (result error) {
	s.lifecycleMu.Lock()
	if s.used {
		s.lifecycleMu.Unlock()
		return errors.New("discovery: store already started or stopped")
	}
	s.used = true
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	s.lifecycleMu.Unlock()
	defer func() {
		if p := recover(); p != nil {
			result = fmt.Errorf("discovery: backend startup panic: %v", p)
			cancel()
			s.finish(result)
			s.options.Logger.Error("discovery startup panic", field.Any("panic", p), field.String("stack", string(debug.Stack())))
		}
	}()
	if err := s.reload(ctx); err != nil {
		cancel()
		s.finish(err)
		return err
	}
	if err := ctx.Err(); err != nil {
		cancel()
		s.finish(err)
		return err
	}
	s.modify(func(state *storeState) { state.running = true })
	go s.run(ctx)
	return nil
}

// Stop cancels blocked I/O and retry waits, and waits up to ctx's deadline. An
// observer/backend must honor its documented contract; Go cannot kill callbacks.
func (s *Store) Stop(ctx context.Context) error {
	s.stopping.Store(true)
	s.lifecycleMu.Lock()
	if !s.used {
		s.used = true
		close(s.done)
	} else if s.cancel != nil {
		s.cancel()
	}
	s.lifecycleMu.Unlock()
	select {
	case <-s.done:
		return nil
	default:
	}
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Store) Done() <-chan struct{} { return s.done }

// Err returns a terminal error, not retryable outages (which appear in Status).
func (s *Store) Err() error { return s.state.Load().terminal }

func (s *Store) Status() Status {
	state := s.state.Load()
	ready := state.running && !s.stopping.Load() && state.lastError == nil && !state.lastSync.IsZero() && time.Since(state.lastSync) < s.options.MaxStaleness
	return Status{Revision: state.snapshot.Revision, Count: len(state.snapshot.Services), LastSync: state.lastSync, LastError: state.lastError, Running: state.running, Ready: ready}
}

func (s *Store) Snapshot() Snapshot {
	snapshot := s.state.Load().snapshot
	return Snapshot{Revision: snapshot.Revision, Services: clone(snapshot.Services)}
}

func (s *Store) Get(id string) (Descriptor, bool) {
	d, ok := s.state.Load().snapshot.Services[id]
	d.Metadata = maps.Clone(d.Metadata)
	return d, ok
}

// All returns services sorted by ID, all from the same revision.
func (s *Store) All() []Descriptor {
	snapshot := s.state.Load().snapshot
	result := make([]Descriptor, 0, len(snapshot.Services))
	for _, d := range snapshot.Services {
		d.Metadata = maps.Clone(d.Metadata)
		result = append(result, d)
	}
	slices.SortFunc(result, func(a, b Descriptor) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return result
}

func (s *Store) modify(fn func(*storeState)) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	next := *s.state.Load()
	fn(&next)
	s.state.Store(&next)
}

func (s *Store) finish(err error) {
	s.modify(func(state *storeState) { state.running = false; state.terminal = err; state.lastError = err })
	close(s.done)
}

func (s *Store) reload(ctx context.Context) error {
	requestCtx, cancel := context.WithTimeout(ctx, s.options.RequestTimeout)
	defer cancel()
	result, err := s.backend.List(requestCtx)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	snapshot, err := snapshotFromList(result, s.state.Load().snapshot.Revision)
	if err != nil {
		return err
	}
	s.modify(func(state *storeState) { state.snapshot = snapshot; state.lastSync = time.Now(); state.lastError = nil })
	return nil
}

func (s *Store) run(ctx context.Context) {
	var terminal error
	defer func() {
		if p := recover(); p != nil {
			terminal = fmt.Errorf("discovery: backend panic: %v", p)
			s.options.Logger.Error("discovery store panic", field.Any("panic", p), field.String("stack", string(debug.Stack())))
		}
		s.cancel()
		s.modify(func(state *storeState) {
			state.running = false
			state.terminal = terminal
			if terminal != nil {
				state.lastError = terminal
			}
		})
		s.notify("stopped")
		close(s.done)
	}()
	s.notify("loaded")
	delay := s.options.RetryMin
	reload := false
	nextSync := time.Now().Add(s.options.ResyncInterval)
	for ctx.Err() == nil {
		if reload || !time.Now().Before(nextSync) {
			if err := s.reload(ctx); err != nil {
				if !s.retry(ctx, err, &delay) {
					return
				}
				reload = true
				continue
			}
			reload = false
			nextSync = time.Now().Add(s.options.ResyncInterval)
			delay = s.options.RetryMin
			s.notify("loaded")
		}
		watcher, err := s.backend.Watch(ctx, s.state.Load().snapshot.Revision+1)
		if err != nil {
			reload = errors.Is(err, ErrCompacted) || errors.Is(err, ErrInvalidData)
			if !s.retry(ctx, err, &delay) {
				return
			}
			continue
		}
		if watcher == nil {
			if !s.retry(ctx, errors.New("discovery: backend returned nil watcher"), &delay) {
				return
			}
			continue
		}
		err = s.consume(ctx, watcher, nextSync, &delay)
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, errResync) {
			reload = true
			continue
		}
		reload = errors.Is(err, ErrCompacted) || errors.Is(err, ErrInvalidData)
		if !s.retry(ctx, err, &delay) {
			return
		}
	}
}

var errResync = errors.New("discovery: scheduled resync")

func (s *Store) consume(ctx context.Context, watcher Watcher, nextSync time.Time, delay *time.Duration) error {
	defer watcher.Close()
	for {
		readCtx, cancel := context.WithDeadline(ctx, nextSync)
		batch, err := watcher.Next(readCtx)
		deadlineExpired := errors.Is(readCtx.Err(), context.DeadlineExceeded)
		cancel()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if deadlineExpired {
			return errResync
		}
		if err != nil {
			return err
		}
		current := s.state.Load().snapshot
		next, err := applyBatch(current, batch)
		if err != nil {
			return err
		}
		// Ignore replayed old events; a valid progress watermark at the current
		// revision is still evidence of a live watcher and refreshes health.
		if batch.Revision < current.Revision || (batch.Revision == current.Revision && len(batch.Changes) > 0) {
			continue
		}
		s.modify(func(state *storeState) { state.snapshot = next; state.lastSync = time.Now(); state.lastError = nil })
		*delay = s.options.RetryMin
		if len(batch.Changes) == 0 {
			s.notify("progress")
		} else {
			s.notify("updated")
		}
	}
}

func (s *Store) retry(ctx context.Context, err error, delay *time.Duration) bool {
	if ctx.Err() != nil {
		return false
	}
	s.modify(func(state *storeState) { state.lastError = err })
	s.notify("retry")
	wait := *delay
	// Add up to 50% jitter without exceeding RetryMax or overflowing Duration.
	room := min(wait/2, s.options.RetryMax-wait)
	if room > 0 {
		wait += time.Duration(rand.Int64N(int64(room) + 1))
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
	}
	if *delay > s.options.RetryMax/2 {
		*delay = s.options.RetryMax
	} else {
		*delay *= 2
	}
	return true
}

func (s *Store) notify(kind string) {
	if s.options.Observer == nil {
		return
	}
	defer func() {
		if p := recover(); p != nil {
			s.options.Logger.Error("discovery observer panic", field.Any("panic", p), field.String("stack", string(debug.Stack())))
		}
	}()
	s.options.Observer(StoreEvent{Kind: kind, Status: s.Status()})
}
