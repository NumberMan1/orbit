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

func TestManagedConnectorsOnRealTransports(t *testing.T) {
	for _, kind := range []string{"tcp", "websocket"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			noDelay := true
			options := socket.Options{Network: socket.NetworkOptions{ReadBuffer: 4096, WriteBuffer: 4096, NoDelay: &noDelay}}
			echo := func(_ context.Context, s *socket.Session, f rpc.Frame) error { return s.Send(f) }
			var server interface {
				orbit.Component
				Addr() net.Addr
			}
			var err error
			if kind == "tcp" {
				server, err = tcp.NewServer("127.0.0.1:0", options, echo, nil)
			} else {
				server, err = websocket.NewServer("127.0.0.1:0", options, echo, nil, nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := server.Start(ctx); err != nil {
				t.Fatal(err)
			}
			defer server.Stop(context.Background())
			received := make(chan rpc.Frame, 1)
			handler := func(_ context.Context, _ *socket.Session, f rpc.Frame) error { received <- f; return nil }
			retry := socket.ConnectorOptions{RetryMin: time.Millisecond, RetryMax: 5 * time.Millisecond}
			var connector *socket.Connector
			if kind == "tcp" {
				connector, err = tcp.NewConnector(server.Addr().String(), options, nil, handler, retry)
			} else {
				connector, err = websocket.NewConnector("ws://"+server.Addr().String()+"/", options, nil, handler, retry)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := connector.Start(ctx); err != nil {
				t.Fatal(err)
			}
			defer connector.Stop(context.Background())
			first, err := connector.WaitSession(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if first.RemoteAddr() == nil || first.LocalAddr() == nil {
				t.Fatal("missing addresses")
			}
			if err := connector.Send(rpc.Frame{ID: 1, Sequence: 42, Payload: []byte("message")}); err != nil {
				t.Fatal(err)
			}
			select {
			case f := <-received:
				if f.Sequence != 42 || string(f.Payload) != "message" {
					t.Fatal(f)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			first.Close()
			second, err := connector.WaitSession(ctx)
			if err != nil || first.ID() == second.ID() {
				t.Fatal("did not reconnect with new session", err)
			}
			if err := connector.Stop(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}
