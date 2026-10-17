package socket

import (
	"errors"
	"net"
	"sync"
)

// ErrCapacity means a connection or concurrent request quota was reached.
var ErrCapacity = errors.New("socket: capacity reached")

// Admission bounds live accepted connections, including handshakes. Share one
// instance across listeners for a process-wide quota. It must not be copied.
type Admission struct {
	mu                   sync.Mutex
	max, perHost, active int
	hosts                map[string]int
	rejected             uint64
}

// AdmissionStatus is an instantaneous connection quota snapshot.
type AdmissionStatus struct {
	Active   int
	Rejected uint64
}

// NewAdmission sets total and per-host limits; zero means unlimited. The host
// comes from the transport address, never an untrusted forwarded HTTP header.
func NewAdmission(max, perHost int) (*Admission, error) {
	if max < 0 || perHost < 0 {
		return nil, errors.New("socket: negative connection limit")
	}
	return &Admission{max: max, perHost: perHost, hosts: make(map[string]int)}, nil
}

// Acquire reserves a connection until its idempotent release function is called.
// A nil Admission allows the connection. Invalid addresses are rejected.
func (a *Admission) Acquire(address string) (func(), error) {
	if a == nil {
		return func() {}, nil
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if (a.max > 0 && a.active >= a.max) || (a.perHost > 0 && a.hosts[host] >= a.perHost) {
		a.rejected++
		return nil, ErrCapacity
	}
	a.active++
	a.hosts[host]++
	var once sync.Once
	return func() {
		once.Do(func() {
			a.mu.Lock()
			defer a.mu.Unlock()
			a.active--
			a.hosts[host]--
			if a.hosts[host] == 0 {
				delete(a.hosts, host)
			}
		})
	}, nil
}

// Status returns current usage without exposing per-host identifiers.
func (a *Admission) Status() AdmissionStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	return AdmissionStatus{a.active, a.rejected}
}
