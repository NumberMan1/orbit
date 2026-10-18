// Package observe provides optional, bounded telemetry without a global registry.
package observe

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Kind uint8

const (
	Handler Kind = iota
	QueueWait
	ClientCall
	ServerCall
	kindCount
)

// Event has no payload. Observers must be concurrent-safe and return promptly.
// TraceID is an opaque correlation ID, not a W3C span or an authentication token.
type Event struct {
	Kind      Kind
	Duration  time.Duration
	MessageID uint16
	TraceID   string
	Err       error
}
type Observer interface{ Observe(Event) }
type Func func(Event)

func (f Func) Observe(e Event) { f(e) }

// Emit isolates observer panics. It cannot interrupt a blocking observer.
func Emit(o Observer, e Event) {
	if o != nil {
		func() { defer func() { _ = recover() }(); o.Observe(e) }()
	}
}

type traceKey struct{}

func TraceID(ctx context.Context) string { id, _ := ctx.Value(traceKey{}).(string); return id }

// WithTraceID accepts 32 lowercase hex characters with at least one nonzero byte.
func WithTraceID(ctx context.Context, id string) (context.Context, error) {
	b, err := hex.DecodeString(id)
	if err != nil || len(b) != 16 || strings.ToLower(id) != id || id == strings.Repeat("0", 32) {
		return nil, fmt.Errorf("observe: invalid trace ID")
	}
	return context.WithValue(ctx, traceKey{}, id), nil
}
func EnsureTrace(ctx context.Context) (context.Context, error) {
	if TraceID(ctx) != "" {
		return ctx, nil
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, err
	}
	return WithTraceID(ctx, hex.EncodeToString(b[:]))
}

var bounds = [...]time.Duration{time.Millisecond, 5 * time.Millisecond, 10 * time.Millisecond, 50 * time.Millisecond, 100 * time.Millisecond, 500 * time.Millisecond, time.Second, 5 * time.Second}
var names = [...]string{"handler", "queue_wait", "client_call", "server_call"}

type series struct {
	Buckets       [len(bounds)]uint64
	Count, Errors uint64
	Seconds       float64
}

// Metrics has exactly four histograms; IDs, payloads, peers and errors never
// become labels. Zero value is ready. Scrapes copy under lock before writing.
type Metrics struct {
	mu     sync.Mutex
	values [kindCount]series
}

func (m *Metrics) Observe(e Event) {
	if e.Kind >= kindCount {
		return
	}
	d := max(e.Duration, 0)
	m.mu.Lock()
	defer m.mu.Unlock()
	s := &m.values[e.Kind]
	s.Count++
	s.Seconds += d.Seconds()
	if e.Err != nil {
		s.Errors++
	}
	for i, bound := range bounds {
		if d <= bound {
			s.Buckets[i]++
		}
	}
}

// ServeHTTP exports Prometheus text 0.0.4; mount explicitly on a private mux.
func (m *Metrics) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	m.mu.Lock()
	values := m.values
	m.mu.Unlock()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	for k, s := range values {
		name := "orbit_" + names[k] + "_duration_seconds"
		fmt.Fprintf(w, "# TYPE %s histogram\n", name)
		for i, b := range bounds {
			fmt.Fprintf(w, "%s_bucket{le=\"%g\"} %d\n", name, b.Seconds(), s.Buckets[i])
		}
		fmt.Fprintf(w, "%s_bucket{le=\"+Inf\"} %d\n%s_sum %g\n%s_count %d\n", name, s.Count, name, s.Seconds, name, s.Count)
		errName := "orbit_" + names[k] + "_errors_total"
		fmt.Fprintf(w, "# TYPE %s counter\n%s %d\n", errName, errName, s.Errors)
	}
}
