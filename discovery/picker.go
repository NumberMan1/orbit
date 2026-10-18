package discovery

import (
	"errors"
	"hash/fnv"
	"sort"
	"sync/atomic"

	"github.com/NumberMan1/orbit/socket"
)

// ErrNoPeer means no connected peer matched, or the directory is not ready.
var ErrNoPeer = errors.New("discovery: no ready peer")

// SessionProvider is implemented by socket.Connector. Components may add their
// own application readiness check before reporting a session as available.
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
	pool *Pool
	next atomic.Uint64
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
	type candidate struct {
		session    *socket.Session
		descriptor Descriptor
	}
	var candidates []candidate
	for _, peer := range p.pool.snapshot() {
		if !peer.started || peer.descriptor.Metadata[DrainingMetadata] == "true" || (name != "" && name != peer.descriptor.Name) {
			continue
		}
		provider, ok := peer.component.(SessionProvider)
		if !ok {
			continue
		}
		s, ok := provider.Session()
		if ok && s != nil && s.Context().Err() == nil && !s.Status().Draining {
			candidates = append(candidates, candidate{s, peer.descriptor})
		}
	}
	if len(candidates) == 0 {
		return nil, Descriptor{}, ErrNoPeer
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].descriptor.ID < candidates[j].descriptor.ID })
	index := 0
	if affinity == "" {
		index = int((p.next.Add(1) - 1) % uint64(len(candidates)))
	} else {
		var best uint64
		for i, candidate := range candidates {
			h := fnv.New64a()
			_, _ = h.Write([]byte(affinity))
			_, _ = h.Write([]byte{0})
			_, _ = h.Write([]byte(candidate.descriptor.ID))
			if score := h.Sum64(); i == 0 || score > best {
				index, best = i, score
			}
		}
	}
	chosen := candidates[index]
	return chosen.session, copyDescriptor(chosen.descriptor), nil
}
