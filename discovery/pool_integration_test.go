//go:build integration

package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/NumberMan1/orbit"
	"github.com/NumberMan1/orbit/rpc"
	"github.com/NumberMan1/orbit/socket"
	"github.com/NumberMan1/orbit/socket/tcp"
)

func TestPoolConnectsAndClosesDiscoveredTCPPeer(t *testing.T) {
	client := testEtcd(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	server, err := tcp.NewServer("127.0.0.1:0", socket.Options{}, func(_ context.Context, s *socket.Session, f rpc.Frame) error {
		return s.Send(f)
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer server.Stop(context.Background())
	registry, err := New(client, "/pool-transport")
	if err != nil {
		t.Fatal(err)
	}
	registration, err := registry.Register(ctx, Descriptor{ID: "echo", Name: "tcp", Address: server.Addr().String()}, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer registration.Close(context.Background())
	store, err := registry.NewStore(StoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer store.Stop(context.Background())
	inbox, err := socket.NewInbox(1)
	if err != nil {
		t.Fatal(err)
	}
	defer inbox.Close()
	pool, err := NewPool(store, func(d Descriptor) (orbit.Component, error) {
		return tcp.NewConnector(d.Address, socket.Options{}, nil, inbox.Handle, socket.ConnectorOptions{})
	}, PoolOptions{Interval: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer pool.Stop(context.Background())
	wait := func(predicate func() bool) {
		t.Helper()
		for !predicate() {
			select {
			case <-ctx.Done():
				t.Fatal("pool reconciliation timed out", pool.Status(), store.Status())
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	var connector *socket.Connector
	wait(func() bool {
		component, ok := pool.Get("echo")
		if ok {
			connector = component.(*socket.Connector)
			return connector.Status().Connected
		}
		return false
	})
	if err := connector.Send(rpc.Frame{ID: 7, Sequence: 42, Payload: []byte("discovered peer")}); err != nil {
		t.Fatal(err)
	}
	event, err := inbox.Receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if event.Frame.ID != 7 || event.Frame.Sequence != 42 || string(event.Frame.Payload) != "discovered peer" {
		t.Fatal(event.Frame)
	}
	if err := registration.Close(ctx); err != nil {
		t.Fatal(err)
	}
	wait(func() bool { return pool.Status().Count == 0 })
	select {
	case <-connector.Done():
	default:
		t.Fatal("removed peer connector still running")
	}
	wait(func() bool { return server.SessionCount() == 0 })
}
