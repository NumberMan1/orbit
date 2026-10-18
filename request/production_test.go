package request

import (
	"context"
	"errors"
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/NumberMan1/orbit/observe"
	"github.com/NumberMan1/orbit/rpc"
	"github.com/NumberMan1/orbit/socket"
	"github.com/NumberMan1/orbit/socket/tcp"
)

func pair(t *testing.T, options Options, server socket.Handler) (*Client, *socket.Session) {
	t.Helper()
	a, b := net.Pipe()
	front, _ := socket.NewSession(t.Context(), tcp.Transport{Conn: a}, socket.Options{})
	back, _ := socket.NewSession(t.Context(), tcp.Transport{Conn: b}, socket.Options{})
	c, err := New(front, options)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{}, 2)
	go func() { defer func() { done <- struct{}{} }(); _ = front.Run(c.Handler(nil)) }()
	go func() { defer func() { done <- struct{}{} }(); _ = back.Run(server) }()
	t.Cleanup(func() { c.Close(); front.Close(); back.Close(); <-done; <-done })
	return c, back
}

func TestRemoteCancelMetadataAndNextCallSurvives(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started := make(chan context.Context, 1)
		canceled := make(chan error, 1)
		server, err := NewServer(ServerOptions{RequestIDs: []uint16{1}, CancelID: 9, Metadata: true, MaxConcurrent: 2, OnError: func(context.Context, *socket.Session, rpc.Frame, error) error { return nil }}, func(ctx context.Context, s *socket.Session, f rpc.Frame) error {
			if string(f.Payload) == "hold" {
				started <- ctx
				<-ctx.Done()
				canceled <- ctx.Err()
				return ctx.Err()
			}
			return s.Send(rpc.Frame{ID: 2, Sequence: f.Sequence, Payload: f.Payload})
		})
		if err != nil {
			t.Fatal(err)
		}
		client, _ := pair(t, Options{ResponseIDs: []uint16{2}, CancelID: 9, Metadata: true, Timeout: time.Minute}, server.Handler(nil))
		trace, _ := observe.WithTraceID(t.Context(), "0123456789abcdef0123456789abcdef")
		ctx, cancel := context.WithCancel(trace)
		done := make(chan error, 1)
		go func() { _, err := client.Call(ctx, rpc.Frame{ID: 1, Payload: []byte("hold")}, 2); done <- err }()
		remote := <-started
		if observe.TraceID(remote) != observe.TraceID(ctx) {
			t.Fatal("trace not propagated")
		}
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if err := <-canceled; !errors.Is(err, context.Canceled) {
			t.Fatal("control frame did not cancel", err)
		}
		synctest.Wait()
		f, err := client.Call(trace, rpc.Frame{ID: 1, Payload: []byte("next")}, 2)
		if err != nil || string(f.Payload) != "next" {
			t.Fatal(f, err)
		}
		synctest.Wait()
		if server.Pending() != 0 || client.Status().Pending != 0 {
			t.Fatal("leaked calls")
		}
	})
}

func TestRemoteDeadlineWithoutCancelFrame(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ended := make(chan error, 1)
		server, _ := NewServer(ServerOptions{RequestIDs: []uint16{1}, Metadata: true, Timeout: time.Minute, OnError: func(context.Context, *socket.Session, rpc.Frame, error) error { return nil }}, func(ctx context.Context, _ *socket.Session, _ rpc.Frame) error {
			<-ctx.Done()
			ended <- ctx.Err()
			return ctx.Err()
		})
		client, _ := pair(t, Options{ResponseIDs: []uint16{2}, Metadata: true, Timeout: time.Second}, server.Handler(nil))
		_, err := client.Call(t.Context(), rpc.Frame{ID: 1}, 2)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
		if err = <-ended; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
		synctest.Wait()
		if server.Pending() != 0 {
			t.Fatal("deadline leaked request")
		}
	})
}

func TestHeartbeatMatchesPongAndClosesUnresponsiveSession(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var count int
		pong, _ := RespondHeartbeat(3, 4, nil)
		client, _ := pair(t, Options{ResponseIDs: []uint16{2, 4}, Timeout: time.Second}, func(ctx context.Context, s *socket.Session, f rpc.Frame) error {
			count++
			if count == 1 {
				return pong(ctx, s, f)
			}
			// Wrong/late sequences must not keep this connection healthy.
			return s.Send(rpc.Frame{ID: 4, Sequence: 1})
		})
		h, err := NewHeartbeat(client, HeartbeatOptions{PingID: 3, PongID: 4, Interval: time.Second, Timeout: 100 * time.Millisecond, MissLimit: 2, CloseOnFailure: true})
		if err != nil {
			t.Fatal(err)
		}
		if err = h.Run(t.Context()); !errors.Is(err, ErrUnresponsive) {
			t.Fatal(err)
		}
		state := h.Status()
		if state.Replies != 1 || state.Misses != 2 || state.Healthy || client.session.Context().Err() == nil {
			t.Fatal(state)
		}
	})
}

func TestServerCapacityAndCrossSessionCancelIsolation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started := make(chan context.Context, 1)
		release := make(chan struct{})
		server, _ := NewServer(ServerOptions{RequestIDs: []uint16{1}, CancelID: 9, MaxConcurrent: 1, Timeout: time.Minute, OnError: func(_ context.Context, s *socket.Session, f rpc.Frame, _ error) error {
			return s.Send(rpc.Frame{ID: 2, Sequence: f.Sequence, Payload: []byte("busy")})
		}}, func(ctx context.Context, _ *socket.Session, _ rpc.Frame) error { started <- ctx; <-release; return nil })
		a, _ := pair(t, Options{ResponseIDs: []uint16{2}, CancelID: 9, Timeout: time.Minute}, server.Handler(nil))
		b, _ := pair(t, Options{ResponseIDs: []uint16{2}, CancelID: 9, Timeout: time.Minute}, server.Handler(nil))
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { _, err := a.Call(ctx, rpc.Frame{ID: 1}, 2); done <- err }()
		remote := <-started
		f, err := b.Call(t.Context(), rpc.Frame{ID: 1}, 2)
		if err != nil || string(f.Payload) != "busy" {
			t.Fatal(f, err)
		}
		if err = b.session.Send(rpc.Frame{ID: 9, Sequence: 1}); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if remote.Err() != nil {
			t.Fatal("another session canceled this call")
		}
		cancel()
		if err = <-done; !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		synctest.Wait()
		if !errors.Is(remote.Err(), context.Canceled) || server.Pending() != 1 {
			t.Fatal("capacity released before work returned")
		}
		close(release)
		synctest.Wait()
		if server.Pending() != 0 {
			t.Fatal("leaked slot")
		}
	})
}
