package discovery

import (
	"hash/fnv"
	"sort"

	"github.com/NumberMan1/orbit/socket"
)

// Immutable catalog: rebuilt only when managed peers or routing metadata change.
type routes struct {
	groups      map[string][]peer
	generations map[string]uint64
	byID        map[string]peer
}

func (p *Pool) routes() *routes {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.routing != nil {
		return p.routing
	}
	r := &routes{groups: make(map[string][]peer), generations: make(map[string]uint64), byID: make(map[string]peer)}
	all := make([]peer, 0, len(p.peers))
	for _, v := range p.peers {
		all = append(all, v)
		r.generations[v.descriptor.ID] = v.generation
		r.byID[v.descriptor.ID] = v
	}
	sort.Slice(all, func(i, j int) bool { return all[i].descriptor.ID < all[j].descriptor.ID })
	r.groups[""] = all
	for _, v := range all {
		if v.descriptor.Name != "" {
			r.groups[v.descriptor.Name] = append(r.groups[v.descriptor.Name], v)
		}
	}
	p.routing = r
	return r
}

func readySession(p peer) *socket.Session {
	if !p.started || p.descriptor.Metadata[DrainingMetadata] == "true" {
		return nil
	}
	provider, ok := p.component.(SessionProvider)
	if !ok {
		return nil
	}
	s, ok := provider.Session()
	if !ok || s == nil || s.Context().Err() != nil || s.Status().Draining {
		return nil
	}
	return s
}
func routeScore(affinity, id string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(affinity))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(id))
	return h.Sum64()
}

// Cursors are independent for each service and bounded by the current catalog.
type routeCursor struct {
	catalog *routes
	next    map[string]int
}

func (c *routeCursor) start(r *routes, name string) int {
	if c.next == nil {
		c.next = make(map[string]int)
	}
	if c.catalog != r {
		for name := range c.next {
			if len(r.groups[name]) == 0 {
				delete(c.next, name)
			}
		}
		c.catalog = r
	}
	if n := len(r.groups[name]); n > 0 {
		return c.next[name] % n
	}
	return 0
}
