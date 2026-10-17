package socket_test

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/NumberMan1/orbit/rpc"
	"github.com/NumberMan1/orbit/service"
	"github.com/NumberMan1/orbit/socket"
	"github.com/NumberMan1/orbit/socket/tcp"
)

func TestAdmissionQuotasAndRelease(t *testing.T) {
	a, err := socket.NewAdmission(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	one, err := a.Acquire("127.0.0.1:1234")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Acquire("127.0.0.1:1235"); !errors.Is(err, socket.ErrCapacity) {
		t.Fatal(err)
	}
	two, err := a.Acquire("127.0.0.2:1234")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Acquire("127.0.0.3:1234"); !errors.Is(err, socket.ErrCapacity) {
		t.Fatal(err)
	}
	one()
	one()
	two()
	if got := a.Status(); got.Active != 0 || got.Rejected != 2 {
		t.Fatal(got)
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			for range 100 {
				if release, err := a.Acquire("127.0.0.1:4"); err == nil {
					release()
				}
			}
		})
	}
	wg.Wait()
	if a.Status().Active != 0 {
		t.Fatal("leaked connection reservation")
	}
}

func TestDrainWaitsForQueuedReplyAndMetrics(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	a, b := net.Pipe()
	defer b.Close()
	metrics := &socket.Metrics{}
	s, err := socket.NewSession(ctx, tcp.Transport{Conn: a}, socket.Options{Metrics: metrics})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	q, _ := service.NewQueue(8, 0, nil, nil)
	if err := q.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer q.Stop(ctx)
	entered, release := make(chan struct{}), make(chan struct{})
	h, _ := socket.Queued(q, func(_ context.Context, s *socket.Session, f rpc.Frame) error {
		close(entered)
		<-release
		return s.Send(f)
	})
	run := make(chan error, 1)
	go func() { run <- s.Run(h) }()
	if err := rpc.Write(b, rpc.Frame{ID: 1, Sequence: 77, Payload: []byte("accepted")}, rpc.MaxPayload); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	drained := make(chan error, 1)
	go func() { drained <- s.Drain(ctx) }()
	for !s.Status().Draining {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	select {
	case err := <-drained:
		t.Fatalf("drained before handler completion: %v", err)
	default:
	}
	close(release)
	_ = b.SetReadDeadline(time.Now().Add(time.Second))
	f, err := rpc.Read(b, rpc.MaxPayload)
	if err != nil || f.Sequence != 77 || string(f.Payload) != "accepted" {
		t.Fatal(f, err)
	}
	if err := <-drained; err != nil {
		t.Fatal(err)
	}
	if err := <-run; !errors.Is(err, socket.ErrDraining) {
		t.Fatal(err)
	}
	got := metrics.Snapshot()
	if got.Active != 0 || got.Received != 1 || got.Sent != 1 || got.SentBytes != 8 {
		t.Fatal(got)
	}
}

func TestDrainDeadlineClosesBlockedWriter(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	s, _ := socket.NewSession(t.Context(), tcp.Transport{Conn: a}, socket.Options{})
	done := make(chan error, 1)
	go func() { done <- s.Run(nil) }()
	if err := s.Send(rpc.Frame{ID: 1}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := s.Drain(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("writer not unblocked")
	}
}

func TestInboundRateRejectsBeforeHandler(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	m := &socket.Metrics{}
	s, _ := socket.NewSession(t.Context(), tcp.Transport{Conn: a}, socket.Options{Metrics: m, InboundRate: socket.RateLimit{PerSecond: 0.001, Burst: 1}})
	calls := 0
	done := make(chan error, 1)
	go func() { done <- s.Run(func(context.Context, *socket.Session, rpc.Frame) error { calls++; return nil }) }()
	for range 2 {
		if err := rpc.Write(b, rpc.Frame{ID: 1}, rpc.MaxPayload); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case err := <-done:
		if !errors.Is(err, socket.ErrRateLimited) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("limiter failed")
	}
	if calls != 1 || m.Snapshot().Rejected != 1 {
		t.Fatal(calls, m.Snapshot())
	}
}

func TestConcurrentRequestQuotaReleasesAfterPanic(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	h, _ := socket.LimitConcurrent(1, func(context.Context, *socket.Session, rpc.Frame) error {
		close(entered)
		<-release
		panic("user callback")
	})
	done := make(chan struct{})
	go func() { defer close(done); defer func() { _ = recover() }(); _ = h(t.Context(), nil, rpc.Frame{}) }()
	<-entered
	if err := h(t.Context(), nil, rpc.Frame{}); !errors.Is(err, socket.ErrCapacity) {
		t.Fatal(err)
	}
	close(release)
	<-done
	// A second invocation reaches the callback (which panics on close), not quota rejection.
	func() {
		defer func() {
			if recover() == nil {
				t.Error("quota was not released")
			}
		}()
		_ = h(t.Context(), nil, rpc.Frame{})
	}()
}
