package socket_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/NumberMan1/orbit"
	"github.com/NumberMan1/orbit/rpc"
	"github.com/NumberMan1/orbit/socket"
	"github.com/NumberMan1/orbit/socket/tcp"
	"github.com/NumberMan1/orbit/socket/websocket"
)

func TestListenerQuotaAndGracefulResponse(t *testing.T) {
	for _, kind := range []string{"tcp", "websocket"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			admission, _ := socket.NewAdmission(1, 1)
			entered, release := make(chan struct{}), make(chan struct{})
			h := func(_ context.Context, s *socket.Session, f rpc.Frame) error {
				close(entered)
				select {
				case <-release:
				case <-ctx.Done():
					return ctx.Err()
				}
				return s.Send(f)
			}
			var server interface {
				orbit.Component
				Addr() net.Addr
				Drain(context.Context) error
				Quiesce()
			}
			var err error
			if kind == "tcp" {
				server, err = tcp.NewServer("127.0.0.1:0", socket.Options{Admission: admission}, h, nil)
			} else {
				server, err = websocket.NewServer("127.0.0.1:0", socket.Options{Admission: admission}, h, nil, nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := server.Start(ctx); err != nil {
				t.Fatal(err)
			}
			defer server.Stop(ctx)
			dial := func() (*socket.Session, error) {
				if kind == "tcp" {
					return tcp.Dial(ctx, server.Addr().String(), socket.Options{}, nil)
				}
				return websocket.Dial(ctx, "ws://"+server.Addr().String(), socket.Options{}, nil)
			}
			client, err := dial()
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			replies := make(chan rpc.Frame, 1)
			done := make(chan struct{})
			go func() {
				defer close(done)
				_ = client.Run(func(_ context.Context, _ *socket.Session, f rpc.Frame) error { replies <- f; return nil })
			}()
			if err := client.Send(rpc.Frame{ID: 1, Sequence: 9}); err != nil {
				t.Fatal(err)
			}
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			second, err := dial()
			if second != nil {
				defer second.Close()
				go func() { _ = second.Run(nil) }()
			}
			if kind == "websocket" && err == nil {
				t.Fatal("upgrade admitted over quota")
			}
			for admission.Status().Rejected == 0 {
				select {
				case <-ctx.Done():
					t.Fatal("connection not rejected")
				case <-time.After(time.Millisecond):
				}
			}
			server.Quiesce()
			drained := make(chan error, 1)
			go func() { drained <- server.Drain(ctx) }()
			close(release)
			select {
			case f := <-replies:
				if f.Sequence != 9 {
					t.Fatal(f)
				}
			case <-ctx.Done():
				t.Fatal("reply lost during drain")
			}
			if err := <-drained; err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("client not closed")
			}
			if admission.Status().Active != 0 {
				t.Fatal("quota reservation leaked", admission.Status())
			}
		})
	}
}
