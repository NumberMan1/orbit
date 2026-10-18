package discovery

import (
	"context"
	"errors"
	"hash/fnv"
	"sort"
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
	failures int
	until    time.Time
	probe    bool
	epoch    uint64
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
// State is pruned to current connected Pool sessions on every Acquire.
type Balancer struct {
	pool     *Pool
	options  BalancerOptions
	mu       sync.Mutex
	circuits map[*socket.Session]*circuit
	next     uint64
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
	var all []candidate
	live := make(map[*socket.Session]bool)
	for _, peer := range b.pool.snapshot() {
		provider, ok := peer.component.(SessionProvider)
		if !peer.started || !ok {
			continue
		}
		s, ok := provider.Session()
		if !ok || s == nil || s.Context().Err() != nil {
			continue
		}
		live[s] = true
		if (name != "" && peer.descriptor.Name != name) || peer.descriptor.Metadata[DrainingMetadata] == "true" || s.Status().Draining {
			continue
		}
		h := fnv.New64a()
		_, _ = h.Write([]byte(affinity))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(peer.descriptor.ID))
		all = append(all, candidate{s, peer.descriptor, h.Sum64()})
	}
	sort.Slice(all, func(i, j int) bool {
		if affinity != "" && all[i].score != all[j].score {
			return all[i].score > all[j].score
		}
		return all[i].descriptor.ID < all[j].descriptor.ID
	})
	b.mu.Lock()
	defer b.mu.Unlock()
	for session := range b.circuits {
		if !live[session] {
			delete(b.circuits, session)
		}
	}
	if len(all) == 0 {
		return nil, ErrNoPeer
	}
	start := 0
	if affinity == "" {
		start = int(b.next % uint64(len(all)))
		b.next++
	}
	for offset := range len(all) {
		chosen := all[(start+offset)%len(all)]
		c := b.circuits[chosen.session]
		if c == nil {
			c = &circuit{}
			b.circuits[chosen.session] = c
		}
		if c.probe || time.Now().Before(c.until) {
			continue
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

// A faulty classifier must not strand a reserved half-open probe.
func classifyFailure(fn func(error) bool, err error) (failed bool) {
	defer func() {
		if recover() != nil {
			failed = true
		}
	}()
	return fn(err)
}
