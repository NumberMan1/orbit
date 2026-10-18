package socket_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/NumberMan1/orbit/observe"
	"github.com/NumberMan1/orbit/rpc"
	"github.com/NumberMan1/orbit/service"
	"github.com/NumberMan1/orbit/socket"
	"github.com/NumberMan1/orbit/socket/tcp"
)

func TestAsyncDrainKeepsAcceptedReply(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, b := net.Pipe()
		defer b.Close()
		s, _ := socket.NewSession(t.Context(), tcp.Transport{Conn: a}, socket.Options{})
		defer s.Close()
		entered, release := make(chan struct{}), make(chan struct{})
		h, _ := socket.Async(1, func(ctx context.Context, s *socket.Session, f rpc.Frame) error {
			close(entered)
			<-release
			return s.Send(f)
		})
		done := make(chan error, 1)
		go func() { done <- s.Run(h) }()
		if err := rpc.Write(b, rpc.Frame{ID: 1, Sequence: 42}, rpc.MaxPayload); err != nil {
			t.Fatal(err)
		}
		<-entered
		drain := make(chan error, 1)
		go func() { drain <- s.Drain(t.Context()) }()
		synctest.Wait()
		select {
		case err := <-drain:
			t.Fatalf("premature drain: %v", err)
		default:
		}
		close(release)
		f, err := rpc.Read(b, rpc.MaxPayload)
		if err != nil || f.Sequence != 42 {
			t.Fatal(f, err)
		}
		if err = <-drain; err != nil {
			t.Fatal(err)
		}
		if err = <-done; !errors.Is(err, socket.ErrDraining) {
			t.Fatal(err)
		}
	})
}

func TestQueuedObserverMeasuresWaitingSeparately(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q, _ := service.NewQueue(2, 0, nil, nil)
		if err := q.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		defer q.Stop(t.Context())
		gate := make(chan struct{})
		entered := make(chan struct{})
		_ = q.Post(func() { close(entered); <-gate })
		<-entered
		a, b := net.Pipe()
		defer b.Close()
		events := make(chan observe.Event, 10)
		s, _ := socket.NewSession(t.Context(), tcp.Transport{Conn: a}, socket.Options{Observer: observe.Func(func(e observe.Event) { events <- e })})
		defer s.Close()
		h, _ := socket.Queued(q, func(context.Context, *socket.Session, rpc.Frame) error { return nil })
		if err := h(t.Context(), s, rpc.Frame{ID: 7}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
		close(gate)
		synctest.Wait()
		var wait, execution time.Duration
		for len(events) > 0 {
			e := <-events
			if e.Kind == observe.QueueWait {
				wait = e.Duration
			}
			if e.Kind == observe.Handler {
				execution = e.Duration
			}
		}
		if wait != 100*time.Millisecond || execution != 0 {
			t.Fatal(wait, execution)
		}
	})
}
