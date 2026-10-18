package discovery

import (
	"errors"
	"sync"

	"github.com/NumberMan1/orbit/socket"
)

// ErrNoPeer means no connected peer matched, or the directory is not ready.
var ErrNoPeer = errors.New("discovery: no ready peer")

// SessionProvider is implemented by socket.Connector. Components may add their
// own application readiness check before reporting a session as available.
// Session must return promptly and must not reenter the selector.
type SessionProvider interface {
	Session() (*socket.Session, bool)
}

// DrainingMetadata is an opt-in directory convention. Publish "true" before
// draining; selectors exclude that descriptor without stopping existing calls.
const DrainingMetadata = "orbit.draining"

// Picker selects connected Pool members. Empty affinity uses round robin;
// nonempty affinity uses rendezvous hashing over stable descriptor IDs.
// Do not copy after use. Picking never retries or replays a request.
type Picker struct {
	pool   *Pool
	mu     sync.Mutex
	cursor routeCursor
}

// NewPicker binds selection to the pool's current managed peers and Store health.
func NewPicker(pool *Pool) (*Picker, error) {
	if pool == nil {
		return nil, errors.New("discovery: pool required")
	}
	return &Picker{pool: pool}, nil
}

// Pick selects by service name (empty means any). The borrowed session can close
// immediately afterward; callers must handle Send/Call errors without blind retry.
// Affinity is routing only, not a guarantee of state ownership or migration.
func (p *Picker) Pick(name, affinity string) (*socket.Session, Descriptor, error) {
	if !p.pool.store.Status().Ready {
		return nil, Descriptor{}, ErrNoPeer
	}
	r := p.pool.routes()
	p.mu.Lock()
	defer p.mu.Unlock()
	start := p.cursor.start(r, name)
	peers := r.groups[name]
	var chosen *socket.Session
	var descriptor Descriptor
	var best uint64
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
		if affinity == "" {
			p.cursor.next[name] = index + 1
			return s, copyDescriptor(peer.descriptor), nil
		}
		if score := routeScore(affinity, peer.descriptor.ID); chosen == nil || score > best {
			chosen, descriptor, best = s, peer.descriptor, score
		}
	}
	if chosen == nil {
		return nil, Descriptor{}, ErrNoPeer
	}
	return chosen, copyDescriptor(descriptor), nil
}
