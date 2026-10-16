package socket

import "sync"

// Group owns sessions; closing seals it against racing accepts/upgrades.
// The zero value is ready to use. A closed group cannot be reused.
type Group struct {
	mu       sync.Mutex
	sessions map[uint64]*Session
	closed   bool
}

func (g *Group) Add(s *Session) bool {
	if s == nil {
		return false
	}
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		s.Close()
		return false
	}
	if previous, exists := g.sessions[s.ID()]; exists {
		g.mu.Unlock()
		if previous != s {
			s.Close()
		}
		return false
	}
	if g.sessions == nil {
		g.sessions = make(map[uint64]*Session)
	}
	g.sessions[s.ID()] = s
	g.mu.Unlock()
	return true
}
func (g *Group) Remove(s *Session) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if s != nil && g.sessions[s.ID()] == s {
		delete(g.sessions, s.ID())
	}
}
func (g *Group) Len() int { g.mu.Lock(); defer g.mu.Unlock(); return len(g.sessions) }
func (g *Group) Get(id uint64) (*Session, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	s, ok := g.sessions[id]
	return s, ok
}

// Snapshot copies membership; sessions themselves remain shared live objects.
func (g *Group) Snapshot() []*Session {
	g.mu.Lock()
	defer g.mu.Unlock()
	all := make([]*Session, 0, len(g.sessions))
	for _, s := range g.sessions {
		all = append(all, s)
	}
	return all
}
func (g *Group) Close() {
	g.mu.Lock()
	g.closed = true
	all := make([]*Session, 0, len(g.sessions))
	for _, s := range g.sessions {
		all = append(all, s)
	}
	g.mu.Unlock()
	for _, s := range all {
		s.Close()
	}
}
