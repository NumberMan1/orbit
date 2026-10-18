package observe

import (
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMetricsBoundedHistogramAndTrace(t *testing.T) {
	var m Metrics
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			for range 10 {
				m.Observe(Event{Kind: ClientCall, Duration: 5 * time.Millisecond, MessageID: 65535, TraceID: "never-a-label", Err: errors.New("never-export-error")})
			}
		})
	}
	wg.Wait()
	w := httptest.NewRecorder()
	m.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	body := w.Body.String()
	for _, want := range []string{"orbit_client_call_duration_seconds_bucket{le=\"0.001\"} 0", "orbit_client_call_duration_seconds_bucket{le=\"0.005\"} 100", "orbit_client_call_duration_seconds_bucket{le=\"+Inf\"} 100", "orbit_client_call_duration_seconds_count 100", "orbit_client_call_errors_total 100"} {
		if !strings.Contains(body, want) {
			t.Fatal(want, body)
		}
	}
	if strings.Contains(body, "never-") || strings.Contains(body, "65535") {
		t.Fatal("high cardinality data exported")
	}
	ctx, err := EnsureTrace(t.Context())
	if err != nil || len(TraceID(ctx)) != 32 {
		t.Fatal(err)
	}
	again, err := EnsureTrace(ctx)
	if err != nil || TraceID(again) != TraceID(ctx) {
		t.Fatal(err)
	}
	if _, err := WithTraceID(ctx, strings.Repeat("0", 32)); err == nil {
		t.Fatal("zero trace accepted")
	}
	Emit(Func(func(Event) { panic("observer") }), Event{})
}
