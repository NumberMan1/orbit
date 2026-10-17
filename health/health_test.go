package health

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReadinessLifecycleAndSafeErrors(t *testing.T) {
	r, _ := New(time.Second)
	if r.Probe(t.Context()).Ready {
		t.Fatal("ready before start")
	}
	if err := r.Add("database", func(context.Context) error { return errors.New("secret DSN") }); err != nil {
		t.Fatal(err)
	}
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r.Readiness(w, httptest.NewRequest("GET", "/readyz", nil))
	if w.Code != 503 || strings.Contains(w.Body.String(), "secret") {
		t.Fatal(w.Code, w.Body.String())
	}
	if !r.Live() {
		t.Fatal("dependency failure changed liveness")
	}
	r.Quiesce()
	if r.Probe(t.Context()).Ready || !r.Live() {
		t.Fatal("drain state")
	}
	r.Stop(t.Context())
	if r.Live() {
		t.Fatal("live after stop")
	}
}

func TestProbePanicAndDeadline(t *testing.T) {
	r, _ := New(10 * time.Millisecond)
	_ = r.Start(t.Context())
	_ = r.Add("panic", func(context.Context) error { panic("secret") })
	if r.Probe(t.Context()).Ready {
		t.Fatal("panic ignored")
	}
	r2, _ := New(10 * time.Millisecond)
	_ = r2.Start(t.Context())
	_ = r2.Add("deadline", func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() })
	if r2.Probe(t.Context()).Ready {
		t.Fatal("deadline ignored")
	}
}
