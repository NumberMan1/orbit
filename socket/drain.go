package socket

import (
	"context"
	"errors"
	"sync"
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

// AllowDuringDrain registers negotiated control/response IDs. Never register
// business request IDs. request.Client and server control middleware register
// their IDs automatically; other protocols may use Options.DrainIDs.
func (s *Session) AllowDuringDrain(ids ...uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(ids) == 0 {
		return
	}
	if s.drainIDs == nil {
		s.drainIDs = make(map[uint16]bool)
	}
	for _, id := range ids {
		s.drainIDs[id] = true
	}
}
func (s *Session) retainFrame(id uint16) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || (s.draining && !s.drainIDs[id]) {
		return false
	}
	s.inFlight++
	return true
}
func (s *Session) waitWork() {
	for {
		s.mu.Lock()
		n, changed := s.inFlight, s.changed
		s.mu.Unlock()
		if n == 0 {
			return
		}
		<-changed
	}
}

// TrackWork retains an outbound operation until finish is called. New work is
// rejected during drain. finish is idempotent; it must run on every return path.
func (s *Session) TrackWork() (finish func(), err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.ctx.Err() != nil {
		return nil, ErrClosed
	}
	if s.draining {
		return nil, ErrDraining
	}
	s.inFlight++
	var once sync.Once
	return func() { once.Do(s.releaseWork) }, nil
}

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

// Drain ignores new business frames while still dispatching registered control
// and response IDs. It waits for accepted callbacks, tracked calls and pending
// writes, then closes. It does not wait for peer ACKs of raw Send operations.
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
