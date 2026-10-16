package socket_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/NumberMan1/orbit/codec"
	"github.com/NumberMan1/orbit/rpc"
	"github.com/NumberMan1/orbit/service"
	"github.com/NumberMan1/orbit/socket"
	"github.com/NumberMan1/orbit/socket/tcp"
)

func TestManagedConnectorRetryFreshSessionAndCancel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		attempts := 0
		var peers []net.Conn
		c, err := socket.NewConnector(func(ctx context.Context) (*socket.Session, error) {
			attempts++
			if attempts == 1 {
				return nil, errors.New("unavailable")
			}
			a, b := net.Pipe()
			peers = append(peers, b)
			return socket.NewSession(ctx, tcp.Transport{Conn: a}, socket.Options{})
		}, func(context.Context, *socket.Session, rpc.Frame) error { return nil }, socket.ConnectorOptions{RetryMin: time.Millisecond, RetryMax: 4 * time.Millisecond})
		if err != nil {
			t.Fatal(err)
		}
		if err := c.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		defer func() {
			for _, p := range peers {
				p.Close()
			}
		}()
		synctest.Wait()
		if c.Status().LastError == nil || c.Status().Connected {
			t.Fatal(c.Status())
		}
		time.Sleep(10 * time.Millisecond)
		synctest.Wait()
		first, err := c.WaitSession(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		peers[0].Close()
		synctest.Wait()
		time.Sleep(10 * time.Millisecond)
		synctest.Wait()
		second, err := c.WaitSession(t.Context())
		if err != nil || first.ID() == second.ID() {
			t.Fatal(err, "reused session")
		}
		if err := c.Stop(t.Context()); err != nil {
			t.Fatal(err)
		}
		if c.Status().Connected || c.Err() != nil {
			t.Fatal(c.Status(), c.Err())
		}
		if _, err := c.WaitSession(t.Context()); !errors.Is(err, socket.ErrClosed) {
			t.Fatal(err)
		}
		if err := c.Start(t.Context()); err == nil {
			t.Fatal("connector reused")
		}
	})
}
func TestConnectorStopInterruptsDialAndBackoff(t *testing.T) {
	for _, blocked := range []bool{true, false} {
		synctest.Test(t, func(t *testing.T) {
			c, _ := socket.NewConnector(func(ctx context.Context) (*socket.Session, error) {
				if blocked {
					<-ctx.Done()
				}
				return nil, errors.New("dial failed")
			}, func(context.Context, *socket.Session, rpc.Frame) error { return nil }, socket.ConnectorOptions{RetryMin: time.Hour, RetryMax: time.Hour})
			c.Start(t.Context())
			synctest.Wait()
			before := time.Now()
			if err := c.Stop(t.Context()); err != nil || time.Since(before) != 0 {
				t.Fatal(err, "stop waited for retry")
			}
		})
	}
}

type message struct {
	Value string `json:"value"`
}

func TestTypedQueuedReplySendHookAndSessionState(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	var registry codec.Registry
	registry.Register(1, &message{}, codec.JSON{})
	q, _ := service.NewQueue(4, 0, nil, nil)
	q.Start(t.Context())
	defer q.Stop(context.Background())
	closeSawState := make(chan bool, 1)
	s, _ := socket.NewSession(t.Context(), tcp.Transport{Conn: a}, socket.Options{
		OnOpen:  func(_ context.Context, s *socket.Session) error { return s.SetValue("attached", true) },
		OnClose: func(_ context.Context, s *socket.Session, _ error) { _, ok := s.Value("attached"); closeSawState <- ok },
		BeforeSend: func(_ context.Context, _ *socket.Session, f rpc.Frame) (rpc.Frame, error) {
			f.Sequence++
			return f, nil
		},
	})
	var group socket.Group
	group.Add(s)
	if got, ok := group.Get(s.ID()); !ok || got != s || len(group.Snapshot()) != 1 {
		t.Fatal("session index")
	}
	h, _ := socket.DecodeHandler(&registry, func(_ context.Context, e socket.MessageEvent) error {
		if e.ReceivedAt.IsZero() {
			t.Error("missing receive time")
		}
		return e.Reply(&message{Value: e.Message.(*message).Value})
	})
	h, _ = socket.Queued(q, h)
	go s.Run(h)
	b.SetDeadline(time.Now().Add(2 * time.Second))
	f, _ := registry.Encode(&message{Value: "roundtrip"}, 41)
	if err := rpc.Write(b, f, rpc.MaxPayload); err != nil {
		t.Fatal(err)
	}
	result, err := rpc.Read(b, rpc.MaxPayload)
	if err != nil || result.Sequence != 42 {
		t.Fatal(result, err)
	}
	decoded, err := registry.Decode(result)
	if err != nil || decoded.(*message).Value != "roundtrip" {
		t.Fatal(decoded, err)
	}
	group.Close()
	<-s.Done()
	if !<-closeSawState {
		t.Fatal("session state cleared before OnClose")
	}
	if _, ok := s.Value("attached"); ok {
		t.Fatal("session state retained after Run")
	}
	group.Remove(s)
	if group.Len() != 0 {
		t.Fatal(group.Len())
	}
}
func TestInboxOwnsFramesAndHonorsCancellation(t *testing.T) {
	i, _ := socket.NewInbox(1)
	payload := []byte("a")
	if err := i.Handle(t.Context(), nil, rpc.Frame{Payload: payload}); err != nil {
		t.Fatal(err)
	}
	payload[0] = 'b'
	if err := i.Handle(t.Context(), nil, rpc.Frame{}); !errors.Is(err, socket.ErrQueueFull) {
		t.Fatal(err)
	}
	e, err := i.Receive(t.Context())
	if err != nil || string(e.Frame.Payload) != "a" {
		t.Fatal(e, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := i.Receive(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	i.Close()
	i.Close()
	if err := i.Handle(t.Context(), nil, rpc.Frame{}); !errors.Is(err, socket.ErrClosed) {
		t.Fatal(err)
	}
	if _, err := i.Receive(t.Context()); !errors.Is(err, socket.ErrClosed) {
		t.Fatal(err)
	}
}

func TestCustomSessionIDsCannotReplaceGroupMembers(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	x, y := net.Pipe()
	defer y.Close()
	o := socket.Options{IDGenerator: func() (uint64, error) { return 42, nil }}
	first, _ := socket.NewSession(t.Context(), tcp.Transport{Conn: a}, o)
	defer first.Close()
	second, _ := socket.NewSession(t.Context(), tcp.Transport{Conn: x}, o)
	defer second.Close()
	var group socket.Group
	if !group.Add(first) || group.Add(second) {
		t.Fatal("ID collision accepted")
	}
	group.Remove(second)
	if got, ok := group.Get(42); !ok || got != first {
		t.Fatal("original member lost")
	}
	if second.Context().Err() == nil {
		t.Fatal("rejected session not closed")
	}
	if first.Context().Err() != nil {
		t.Fatal("original session closed")
	}
}
