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
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		s.Close()
		return false
	}
	if g.sessions == nil {
		g.sessions = make(map[uint64]*Session)
	}
	g.sessions[s.ID()] = s
	return true
}
func (g *Group) Remove(s *Session) { g.mu.Lock(); defer g.mu.Unlock(); delete(g.sessions, s.ID()) }
func (g *Group) Len() int          { g.mu.Lock(); defer g.mu.Unlock(); return len(g.sessions) }
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
