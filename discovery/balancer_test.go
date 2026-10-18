package discovery

import (
	"errors"
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/NumberMan1/orbit"
	"github.com/NumberMan1/orbit/socket"
	"github.com/NumberMan1/orbit/socket/tcp"
)

func TestBalancerCircuitEpochProbeAndDrain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := descriptor("a", "first")
		backend := &fakeBackend{result: ListResult{Revision: 1, Services: []Descriptor{d}}}
		store := startedStore(t, backend, options())
		created := 0
		pool, _ := NewPool(store, func(Descriptor) (orbit.Component, error) {
			created++
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
		balancer, _ := NewBalancer(pool, BalancerOptions{FailureThreshold: 1, Cooldown: time.Second})
		old, _ := balancer.Acquire("worker", "")
		failed, _ := balancer.Acquire("worker", "")
		failed.Complete(errors.New("backend timeout"))
		old.Complete(nil)
		if _, err := balancer.Acquire("worker", ""); !errors.Is(err, ErrNoPeer) {
			t.Fatal("stale success closed circuit", err)
		}
		time.Sleep(time.Second)
		synctest.Wait()
		probe, err := balancer.Acquire("worker", "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := balancer.Acquire("worker", ""); !errors.Is(err, ErrNoPeer) {
			t.Fatal("parallel half-open probe", err)
		}
		probe.Complete(nil)
		probe.Complete(errors.New("duplicate completion"))
		next, err := balancer.Acquire("worker", "")
		if err != nil {
			t.Fatal(err)
		}
		next.Complete(nil)
		d.Metadata[DrainingMetadata] = "true"
		backend.watcher(0).input <- watchReply{batch: Batch{Revision: 2, Changes: []Change{{Type: Put, ID: d.ID, Service: d}}}}
		synctest.Wait()
		time.Sleep(time.Second)
		synctest.Wait()
		if created != 1 || next.Session.Context().Err() != nil {
			t.Fatal("drain announcement replaced connection")
		}
		if _, err := balancer.Acquire("worker", ""); !errors.Is(err, ErrNoPeer) {
			t.Fatal("draining backend selected", err)
		}
		picker, _ := NewPicker(pool)
		if _, _, err := picker.Pick("worker", ""); !errors.Is(err, ErrNoPeer) {
			t.Fatal(err)
		}
	})
}
