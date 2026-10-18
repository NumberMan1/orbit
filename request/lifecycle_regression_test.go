package request

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/NumberMan1/orbit/rpc"
	"github.com/NumberMan1/orbit/service"
	"github.com/NumberMan1/orbit/socket"
)

func TestQueuedRequestRetainsContextUntilNestedWorkCompletes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		shards, _ := service.NewShards(1, 4, nil)
		if err := shards.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		defer shards.Stop(t.Context())
		gate := make(chan struct{})
		if err := shards.Post("key", func() { <-gate }); err != nil {
			t.Fatal(err)
		}
		inner, _ := socket.Queued(shards.For("key"), func(ctx context.Context, s *socket.Session, f rpc.Frame) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			return s.Send(rpc.Frame{ID: 2, Sequence: f.Sequence, Payload: []byte("done")})
		})
		handler, _ := socket.Queued(shards.For("key"), inner)
		server, _ := NewServer(ServerOptions{RequestIDs: []uint16{1}}, handler)
		client, _ := pair(t, Options{ResponseIDs: []uint16{2}}, server.Handler(nil))
		done := make(chan error, 1)
		go func() {
			f, err := client.Call(t.Context(), rpc.Frame{ID: 1}, 2)
			if err == nil && string(f.Payload) != "done" {
				err = errors.New("missing reply")
			}
			done <- err
		}()
		synctest.Wait()
		pending := server.Pending()
		close(gate)
		if pending != 1 {
			t.Errorf("queued request lost ownership: pending=%d", pending)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if server.Pending() != 0 {
			t.Fatal("request leaked")
		}
	})
}

func TestDrainContinuesRemoteCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started, ended := make(chan struct{}), make(chan struct{})
		server, _ := NewServer(ServerOptions{RequestIDs: []uint16{1}, CancelID: 9, Timeout: time.Minute, OnError: func(context.Context, *socket.Session, rpc.Frame, error) error { return nil }}, func(ctx context.Context, _ *socket.Session, _ rpc.Frame) error {
			close(started)
			<-ctx.Done()
			close(ended)
			return ctx.Err()
		})
		client, back := pair(t, Options{ResponseIDs: []uint16{2}, CancelID: 9, Timeout: time.Minute}, server.Handler(nil))
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() { _, err := client.Call(ctx, rpc.Frame{ID: 1}, 2); done <- err }()
		<-started
		drain := make(chan error, 1)
		go func() {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			drain <- back.Drain(ctx)
		}()
		synctest.Wait()
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if err := <-drain; err != nil {
			t.Fatal("drain did not receive cancel", err)
		}
		<-ended
	})
}

func TestDrainWaitsForOutboundResponse(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started, release := make(chan struct{}), make(chan struct{})
		server, _ := NewServer(ServerOptions{RequestIDs: []uint16{1}}, func(ctx context.Context, s *socket.Session, f rpc.Frame) error {
			close(started)
			<-release
			return s.Send(rpc.Frame{ID: 2, Sequence: f.Sequence})
		})
		client, _ := pair(t, Options{ResponseIDs: []uint16{2}}, server.Handler(nil))
		done := make(chan error, 1)
		go func() { _, err := client.Call(t.Context(), rpc.Frame{ID: 1}, 2); done <- err }()
		<-started
		drain := make(chan error, 1)
		go func() {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			drain <- client.session.Drain(ctx)
		}()
		synctest.Wait()
		_, err := client.Call(t.Context(), rpc.Frame{ID: 1}, 2)
		close(release)
		if !errors.Is(err, socket.ErrDraining) {
			t.Errorf("new call during drain: %v", err)
		}
		if err := <-done; err != nil {
			t.Fatal("response lost during drain", err)
		}
		if err := <-drain; err != nil {
			t.Fatal(err)
		}
	})
}

func TestQueuedErrorsReachServerErrorHandler(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		shards, _ := service.NewShards(1, 4, nil)
		if err := shards.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		defer shards.Stop(t.Context())
		expected := errors.New("business failure")
		queued, _ := socket.Queued(shards.For("key"), func(ctx context.Context, s *socket.Session, f rpc.Frame) error {
			if string(f.Payload) == "fail" {
				return expected
			}
			return s.Send(rpc.Frame{ID: 2, Sequence: f.Sequence, Payload: []byte("ok")})
		})
		server, _ := NewServer(ServerOptions{RequestIDs: []uint16{1}, OnError: func(_ context.Context, s *socket.Session, f rpc.Frame, err error) error {
			if !errors.Is(err, expected) {
				return err
			}
			return s.Send(rpc.Frame{ID: 2, Sequence: f.Sequence, Payload: []byte("rejected")})
		}}, queued)
		client, _ := pair(t, Options{ResponseIDs: []uint16{2}}, server.Handler(nil))
		for _, tc := range []struct{ in, want string }{{"fail", "rejected"}, {"next", "ok"}} {
			f, err := client.Call(t.Context(), rpc.Frame{ID: 1, Payload: []byte(tc.in)}, 2)
			if err != nil || string(f.Payload) != tc.want {
				t.Fatal("queued error bypassed OnError or closed session", f, err)
			}
		}
	})
}

func TestQueuedCancellationKeepsPendingUntilQueueReleases(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		shards, _ := service.NewShards(1, 4, nil)
		if err := shards.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		defer shards.Stop(t.Context())
		release := make(chan struct{})
		shards.Post("key", func() { <-release })
		ran := false
		queued, _ := socket.Queued(shards.For("key"), func(context.Context, *socket.Session, rpc.Frame) error { ran = true; return nil })
		server, _ := NewServer(ServerOptions{RequestIDs: []uint16{1}, CancelID: 9, Timeout: time.Minute, OnError: func(context.Context, *socket.Session, rpc.Frame, error) error { return nil }}, queued)
		client, _ := pair(t, Options{ResponseIDs: []uint16{2}, CancelID: 9, Timeout: time.Minute}, server.Handler(nil))
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() { _, err := client.Call(ctx, rpc.Frame{ID: 1}, 2); done <- err }()
		synctest.Wait()
		cancel()
		<-done
		synctest.Wait()
		pending := server.Pending()
		close(release)
		synctest.Wait()
		if pending != 1 || server.Pending() != 0 || ran {
			t.Fatalf("cancel ownership: pending before=%d after=%d ran=%v", pending, server.Pending(), ran)
		}
	})
}
