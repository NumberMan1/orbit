// Package health provides explicit, private HTTP liveness and readiness checks.
package health

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Check must honor ctx and return promptly. Checks run outside registry locks;
// they must support concurrent probes. Errors are never exposed in HTTP bodies.
type Check func(context.Context) error

// Registry is an optional App component. A stopped or quiescing instance is not
// ready, even when its dependencies pass. No global HTTP handlers are installed.
type Registry struct {
	mu                   sync.RWMutex
	checks               map[string]Check
	timeout              time.Duration
	used, live, draining bool
}

// New sets one total deadline for sequential dependency checks.
func New(timeout time.Duration) (*Registry, error) {
	if timeout <= 0 {
		return nil, errors.New("health: positive timeout required")
	}
	return &Registry{timeout: timeout, checks: make(map[string]Check)}, nil
}

// Add registers a uniquely named dependency; names appear in readiness responses.
func (r *Registry) Add(name string, check Check) error {
	if name == "" || check == nil {
		return errors.New("health: name and check required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.checks[name]; ok {
		return errors.New("health: duplicate check")
	}
	r.checks[name] = check
	return nil
}

// Start marks this single-use component live; dependency checks decide readiness.
func (r *Registry) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.used || r.draining {
		return errors.New("health: already started or stopped")
	}
	r.used, r.live = true, true
	return nil
}

// Quiesce marks readiness false while preserving liveness during drain.
func (r *Registry) Quiesce() { r.mu.Lock(); r.draining = true; r.mu.Unlock() }

// Stop marks liveness and readiness false; it is idempotent.
func (r *Registry) Stop(context.Context) error {
	r.mu.Lock()
	r.used, r.draining, r.live = true, true, false
	r.mu.Unlock()
	return nil
}

// Live reports process/component liveness without probing external dependencies.
func (r *Registry) Live() bool { r.mu.RLock(); defer r.mu.RUnlock(); return r.live }

// Report contains safe check outcomes; diagnostic error text stays with callers.
type Report struct {
	Ready  bool            `json:"ready"`
	Checks map[string]bool `json:"checks"`
}

// Probe evaluates dependencies under one deadline and rechecks shutdown state.
// A non-cooperative Check cannot be forcibly stopped; no leaking timeout goroutine is spawned.
func (r *Registry) Probe(parent context.Context) Report {
	ctx, cancel := context.WithTimeout(parent, r.timeout)
	defer cancel()
	r.mu.RLock()
	checks := make(map[string]Check, len(r.checks))
	for name, check := range r.checks {
		checks[name] = check
	}
	live := r.live && !r.draining
	r.mu.RUnlock()
	report := Report{Ready: live, Checks: make(map[string]bool, len(checks))}
	for name, check := range checks {
		ok := live && ctx.Err() == nil && invoke(ctx, check) == nil && ctx.Err() == nil
		report.Checks[name] = ok
		report.Ready = report.Ready && ok
	}
	r.mu.RLock()
	report.Ready = report.Ready && r.live && !r.draining && ctx.Err() == nil
	r.mu.RUnlock()
	return report
}

func invoke(ctx context.Context, check Check) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("health: check panic: %v", p)
		}
	}()
	return check(ctx)
}

// Liveness serves a dependency-independent status; mount on a private mux.
func (r *Registry) Liveness(w http.ResponseWriter, req *http.Request) {
	if !r.Live() {
		http.Error(w, "not live", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// Readiness serves safe JSON outcomes and 503 until ready or while draining.
func (r *Registry) Readiness(w http.ResponseWriter, req *http.Request) {
	report := r.Probe(req.Context())
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if !report.Ready {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.MarshalWrite(w, report)
}
