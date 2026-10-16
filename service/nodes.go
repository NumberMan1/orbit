package service

import (
	"errors"
	"maps"
	"sync"
)

// Nodes is a typed, instance-owned index replacing MultiServerNode's unchecked
// map. It does not own component lifetimes; remove entries before stopping them.
type Nodes[T any] struct {
	mu    sync.RWMutex
	nodes map[string]T
}

func (n *Nodes[T]) Add(id string, value T) error {
	if id == "" {
		return errors.New("service: node ID required")
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, ok := n.nodes[id]; ok {
		return errors.New("service: duplicate node ID")
	}
	if n.nodes == nil {
		n.nodes = make(map[string]T)
	}
	n.nodes[id] = value
	return nil
}
func (n *Nodes[T]) Get(id string) (T, bool) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	v, ok := n.nodes[id]
	return v, ok
}
func (n *Nodes[T]) Remove(id string) (T, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	v, ok := n.nodes[id]
	delete(n.nodes, id)
	return v, ok
}
func (n *Nodes[T]) Snapshot() map[string]T {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return maps.Clone(n.nodes)
}
