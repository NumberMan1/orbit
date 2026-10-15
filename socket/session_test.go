package socket_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/NumberMan1/orbit/rpc"
	"github.com/NumberMan1/orbit/socket"
	"github.com/NumberMan1/orbit/socket/tcp"
)

func TestSendCopiesPayloadAndReportsBackpressure(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	s, err := socket.NewSession(t.Context(), tcp.Transport{Conn: a}, socket.Options{SendQueue: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	payload := []byte("original")
	if err := s.Send(rpc.Frame{ID: 1, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	payload[0] = 'X'
	if err := s.Send(rpc.Frame{}); !errors.Is(err, socket.ErrQueueFull) {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.Run(nil) }()
	_ = b.SetReadDeadline(time.Now().Add(2 * time.Second))
	f, err := rpc.Read(b, rpc.MaxPayload)
	if err != nil || string(f.Payload) != "original" {
		t.Fatalf("%+v %v", f, err)
	}
	s.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("session leaked")
	}
	if err := s.Send(rpc.Frame{}); !errors.Is(err, socket.ErrClosed) {
		t.Fatal(err)
	}
}

func TestConcurrentSendClose(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	s, _ := socket.NewSession(t.Context(), tcp.Transport{Conn: a}, socket.Options{SendQueue: 2})
	var wg sync.WaitGroup
	wg.Go(func() { _ = s.Run(nil) })
	for range 10 {
		wg.Go(func() {
			for range 100 {
				_ = s.Send(rpc.Frame{})
				s.Close()
			}
		})
	}
	wg.Wait()
}

func TestHandlerPanicClosesReaderAndWriter(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, _ := socket.NewSession(t.Context(), tcp.Transport{Conn: a}, socket.Options{Logger: logger})
	done := make(chan error, 1)
	go func() { done <- s.Run(func(context.Context, *socket.Session, rpc.Frame) error { panic("test") }) }()
	_ = b.SetWriteDeadline(time.Now().Add(time.Second))
	if err := rpc.Write(b, rpc.Frame{}, rpc.MaxPayload); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("panic swallowed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("panic leaked goroutines")
	}
}

func TestHeartbeatStopsOnClose(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	s, _ := socket.NewSession(t.Context(), tcp.Transport{Conn: a}, socket.Options{})
	done := make(chan error, 1)
	go func() { done <- s.Heartbeat(t.Context(), time.Hour, func() rpc.Frame { return rpc.Frame{} }) }()
	s.Close()
	select {
	case err := <-done:
		if !errors.Is(err, socket.ErrClosed) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("heartbeat leaked")
	}
}

func TestLifecycleHooksOrderedAndOnce(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	var events []string
	s, err := socket.NewSession(t.Context(), tcp.Transport{Conn: a}, socket.Options{
		OnOpen: func(context.Context, *socket.Session) error { events = append(events, "open"); return nil },
		OnClose: func(ctx context.Context, _ *socket.Session, cause error) {
			if ctx.Err() == nil || cause == nil {
				t.Error("close without cancellation cause")
			}
			events = append(events, "close")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = s.Run(func(context.Context, *socket.Session, rpc.Frame) error {
			events = append(events, "message")
			return errors.New("finished")
		})
	}()
	_ = b.SetWriteDeadline(time.Now().Add(time.Second))
	if err := rpc.Write(b, rpc.Frame{}, 10); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("hooks did not finish")
	}
	s.Close()
	if len(events) != 3 || events[0] != "open" || events[1] != "message" || events[2] != "close" {
		t.Fatal(events)
	}
}
