package websocket

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/NumberMan1/orbit/rpc"
	"github.com/NumberMan1/orbit/socket"
	ws "github.com/gorilla/websocket"
)

func TestWebSocketFramesOriginAndShutdown(t *testing.T) {
	s, err := NewServer("127.0.0.1:0", socket.Options{ReadTimeout: time.Second}, func(_ context.Context, session *socket.Session, f rpc.Frame) error { return session.Send(f) }, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer s.Stop(context.Background())
	url := "ws://" + s.Addr().String() + "/"
	if conn, res, err := ws.DefaultDialer.Dial(url, http.Header{"Origin": {"https://untrusted.example"}}); err == nil {
		conn.Close()
		t.Fatal("cross origin accepted")
	} else if res != nil {
		res.Body.Close()
	}
	for _, invalid := range []bool{false, true} {
		conn, _, err := ws.DefaultDialer.Dial(url, nil)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := rpc.Marshal(rpc.Frame{ID: 3, Sequence: 5, Payload: []byte("ok")}, 1024)
		if invalid {
			b = b[:3]
		}
		if err := conn.WriteMessage(ws.BinaryMessage, b); err != nil {
			t.Fatal(err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		kind, result, err := conn.ReadMessage()
		if invalid {
			if err == nil {
				t.Fatal("malformed frame accepted")
			}
		} else if err != nil || kind != ws.BinaryMessage || string(result) != string(b) {
			t.Fatalf("echo %x %v", result, err)
		}
		conn.Close()
	}
	conn, _, err := ws.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("hijacked connection remained open")
	}
}
