package socket

import (
	"context"
	"errors"
)

// ErrDraining indicates that a session no longer accepts new inbound work.
var ErrDraining = errors.New("socket: draining")

// SessionStatus exposes queue pressure and drain progress without payload data.
type SessionStatus struct {
	Queued, PendingWrites, InFlight int
	Draining, Closed                bool
}

// Status returns an instantaneous snapshot. PendingWrites includes the active write.
func (s *Session) Status() SessionStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return SessionStatus{len(s.queue), s.pendingWrites, s.inFlight, s.draining, s.closed}
}

func (s *Session) signalLocked() { close(s.changed); s.changed = make(chan struct{}) }

// retainWork tracks synchronous and Queued handlers. Nested queued callbacks
// already accepted by a running handler remain valid after draining begins.
func (s *Session) retainWork(nested bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || (s.draining && !nested) {
		return false
	}
	s.inFlight++
	return true
}
func (s *Session) releaseWork() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inFlight--
	s.signalLocked()
}

// Drain rejects new inbound handlers, waits for accepted synchronous/Queued
// callbacks and pending writes, then closes. It does not wait for peer ACKs.
// It must not be called synchronously from this session's own callback. On
// timeout it force-closes and returns ctx.Err(); arbitrary user work cannot be killed.
// Run must be active to flush queued sends. Normal Close remains immediate.
func (s *Session) Drain(ctx context.Context) error {
	for {
		s.mu.Lock()
		s.draining = true
		if s.closed {
			cause := context.Cause(s.ctx)
			idle := s.inFlight == 0 && s.pendingWrites == 0
			s.mu.Unlock()
			if idle || errors.Is(cause, ErrDraining) {
				return nil
			}
			return cause
		}
		if err := ctx.Err(); err != nil {
			s.mu.Unlock()
			s.close(err)
			return err
		}
		if s.inFlight == 0 && s.pendingWrites == 0 {
			s.closed = true
			s.cancel(ErrDraining)
			s.signalLocked()
			s.mu.Unlock()
			return s.transport.Close()
		}
		changed := s.changed
		s.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			s.close(ctx.Err())
			return ctx.Err()
		}
	}
}
