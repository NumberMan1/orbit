package discovery

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/NumberMan1/orbit/socket"
)

type BalancerOptions struct {
	FailureThreshold int
	Cooldown         time.Duration
	// Failed classifies infrastructure failures. Default excludes caller cancel.
	// Business errors should normally be excluded by an application classifier.
	Failed func(error) bool
}
type circuit struct {
	id         string
	generation uint64
	failures   int
	until      time.Time
	probe      bool
	epoch      uint64
}

// Selection reserves a half-open probe when applicable. Complete MUST be called
// once after use, including route/setup errors. It is idempotent and safe to race.
// A selection never retries requests; session liveness can change after Acquire.
type Selection struct {
	Session    *socket.Session
	Descriptor Descriptor
	complete   func(error)
}

func (s *Selection) Complete(err error) { s.complete(err) }

// Balancer adds per-session circuit breaking to directory-aware selection.
// Reconnects get fresh circuits; late results cannot close a newer circuit epoch.
// Readiness changes preserve circuits; removal and reconnect discard stale state.
type Balancer struct {
	pool     *Pool
	options  BalancerOptions
	mu       sync.Mutex
	circuits map[*socket.Session]*circuit
	cursor   routeCursor
	catalog  *routes
}

func NewBalancer(pool *Pool, options BalancerOptions) (*Balancer, error) {
	if pool == nil || options.FailureThreshold < 1 || options.Cooldown <= 0 {
		return nil, errors.New("discovery: invalid balancer options")
	}
	if options.Failed == nil {
		options.Failed = func(err error) bool { return err != nil && !errors.Is(err, context.Canceled) }
	}
	return &Balancer{pool: pool, options: options, circuits: make(map[*socket.Session]*circuit)}, nil
}

func (b *Balancer) Acquire(name, affinity string) (*Selection, error) {
	if !b.pool.store.Status().Ready {
		return nil, ErrNoPeer
	}
	type candidate struct {
		session    *socket.Session
		descriptor Descriptor
		score      uint64
	}
	r := b.pool.routes()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.catalog != r {
		b.prune(r)
		b.catalog = r
	}
	start := b.cursor.start(r, name)
	peers := r.groups[name]
	var chosen candidate
	var generation uint64
	chosenIndex := 0
	now := time.Now()
	for offset := range len(peers) {
		index := (start + offset) % len(peers)
		if affinity != "" {
			index = offset
		}
		peer := peers[index]
		s := readySession(peer)
		if s == nil {
			continue
		}
		c := b.circuits[s]
		if c != nil && (c.probe || now.Before(c.until)) {
			continue
		}
		var score uint64
		if affinity != "" {
			score = routeScore(affinity, peer.descriptor.ID)
		}
		if chosen.session == nil || score > chosen.score {
			chosen = candidate{s, peer.descriptor, score}
			generation = peer.generation
			chosenIndex = index
		}
		if affinity == "" {
			break
		}
	}
	if chosen.session != nil {
		if affinity == "" {
			b.cursor.next[name] = chosenIndex + 1
		}
		c := b.circuits[chosen.session]
		if c == nil {
			b.prune(r)
			c = &circuit{id: chosen.descriptor.ID, generation: generation}
			b.circuits[chosen.session] = c
		}
		if !c.until.IsZero() {
			c.probe = true
		}
		epoch := c.epoch
		var once sync.Once
		selection := &Selection{Session: chosen.session, Descriptor: copyDescriptor(chosen.descriptor)}
		selection.complete = func(err error) {
			once.Do(func() {
				failed := err != nil && classifyFailure(b.options.Failed, err)
				b.mu.Lock()
				defer b.mu.Unlock()
				if b.circuits[chosen.session] != c || c.epoch != epoch {
					return
				}
				if c.probe {
					c.probe = false
					c.epoch++
					if failed || errors.Is(err, context.Canceled) {
						c.until = time.Now().Add(b.options.Cooldown)
						return
					}
					c.failures = 0
					c.until = time.Time{}
					return
				}
				if !failed {
					if err == nil {
						c.failures = 0
					}
					return
				}
				c.failures++
				if c.failures >= b.options.FailureThreshold {
					c.until = time.Now().Add(b.options.Cooldown)
					c.epoch++
				}
			})
		}
		return selection, nil
	}
	return nil, ErrNoPeer
}

func (b *Balancer) prune(r *routes) {
	for s, c := range b.circuits {
		generation, exists := r.generations[c.id]
		if !exists || generation != c.generation || s.Context().Err() != nil {
			delete(b.circuits, s)
			continue
		}
		if provider, ok := r.byID[c.id].component.(SessionProvider); ok {
			// A temporarily unready provider may return nil. Only a different,
			// concrete session proves replacement while the old session is alive.
			if current, _ := provider.Session(); current != nil && current != s {
				delete(b.circuits, s)
			}
		}
	}
}

// A faulty classifier must not strand a reserved half-open probe.
func classifyFailure(fn func(error) bool, err error) (failed bool) {
	defer func() {
		if recover() != nil {
			failed = true
		}
	}()
	return fn(err)
}
