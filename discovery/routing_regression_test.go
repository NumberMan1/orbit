package discovery

import (
	"errors"
	"fmt"
	"github.com/NumberMan1/orbit"
	"github.com/NumberMan1/orbit/socket"
	"github.com/NumberMan1/orbit/socket/tcp"
	"net"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

type readinessPeer struct {
	sessionPeer
	ready atomic.Bool
}

func (p *readinessPeer) Session() (*socket.Session, bool) { return p.s, p.ready.Load() }

func TestReadinessFlapPreservesOpenCircuit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := startedStore(t, &fakeBackend{result: ListResult{Revision: 1, Services: []Descriptor{descriptor("a", "one")}}}, options())
		a, b := net.Pipe()
		defer b.Close()
		s, _ := socket.NewSession(t.Context(), tcp.Transport{Conn: a}, socket.Options{})
		p := &readinessPeer{sessionPeer: sessionPeer{s}}
		p.ready.Store(true)
		pool, _ := NewPool(store, func(Descriptor) (orbit.Component, error) { return p, nil }, PoolOptions{})
		if err := pool.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		defer pool.Stop(t.Context())
		synctest.Wait()
		balancer, _ := NewBalancer(pool, BalancerOptions{FailureThreshold: 1, Cooldown: time.Minute})
		selection, err := balancer.Acquire("worker", "")
		if err != nil {
			t.Fatal(err)
		}
		selection.Complete(errors.New("timeout"))
		p.ready.Store(false)
		if _, err := balancer.Acquire("worker", ""); !errors.Is(err, ErrNoPeer) {
			t.Fatal(err)
		}
		p.ready.Store(true)
		if _, err := balancer.Acquire("worker", ""); !errors.Is(err, ErrNoPeer) {
			t.Fatal("readiness reset cooldown", err)
		}
		time.Sleep(time.Minute)
		selection, err = balancer.Acquire("worker", "")
		if err != nil {
			t.Fatal(err)
		}
		selection.Complete(nil)
	})
}

func TestRoundRobinIndependentAcrossServices(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var descriptors []Descriptor
		for _, name := range []string{"alpha", "beta"} {
			for i := range 2 {
				d := descriptor(fmt.Sprintf("%s%d", name, i), "address")
				d.Name = name
				descriptors = append(descriptors, d)
			}
		}
		store := startedStore(t, &fakeBackend{result: ListResult{Revision: 1, Services: descriptors}}, options())
		pool, _ := NewPool(store, func(Descriptor) (orbit.Component, error) {
			a, b := net.Pipe()
			t.Cleanup(func() { b.Close() })
			s, _ := socket.NewSession(t.Context(), tcp.Transport{Conn: a}, socket.Options{})
			return &sessionPeer{s}, nil
		}, PoolOptions{})
		if err := pool.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		defer pool.Stop(t.Context())
		synctest.Wait()
		picker, _ := NewPicker(pool)
		balancer, _ := NewBalancer(pool, BalancerOptions{FailureThreshold: 1, Cooldown: time.Second})
		for _, kind := range []string{"picker", "balancer"} {
			seen := map[string]int{}
			for range 4 {
				for _, name := range []string{"alpha", "beta"} {
					var d Descriptor
					if kind == "picker" {
						_, v, err := picker.Pick(name, "")
						if err != nil {
							t.Fatal(err)
						}
						d = v
					} else {
						s, err := balancer.Acquire(name, "")
						if err != nil {
							t.Fatal(err)
						}
						d = s.Descriptor
						s.Complete(nil)
					}
					seen[d.ID]++
				}
			}
			if len(seen) != 4 {
				t.Errorf("%s starved nodes: %v", kind, seen)
			}
			for id, n := range seen {
				if n != 2 {
					t.Errorf("%s: %s got %d", kind, id, n)
				}
			}
		}
	})
}
