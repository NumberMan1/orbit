package discovery

import (
	"fmt"
	"github.com/NumberMan1/orbit/socket"
	"github.com/NumberMan1/orbit/socket/tcp"
	"net"
	"testing"
	"time"
)

func BenchmarkBalancerAcquire(b *testing.B) {
	for _, size := range []int{100, 1000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			store := &Store{options: StoreOptions{MaxStaleness: time.Hour}}
			store.state.Store(&storeState{running: true, lastSync: time.Now(), snapshot: &Snapshot{}})
			pool := &Pool{store: store, peers: make(map[string]peer)}
			for i := range size {
				a, other := net.Pipe()
				s, err := socket.NewSession(b.Context(), tcp.Transport{Conn: a}, socket.Options{})
				if err != nil {
					b.Fatal(err)
				}
				b.Cleanup(func() { s.Close(); other.Close() })
				id := fmt.Sprint(i)
				pool.peers[id] = peer{descriptor: Descriptor{ID: id, Name: "worker"}, component: &sessionPeer{s}, started: true}
			}
			balancer, _ := NewBalancer(pool, BalancerOptions{FailureThreshold: 1, Cooldown: time.Second})
			for range size {
				selection, err := balancer.Acquire("worker", "")
				if err != nil {
					b.Fatal(err)
				}
				selection.Complete(nil)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				selection, err := balancer.Acquire("worker", "")
				if err != nil {
					b.Fatal(err)
				}
				selection.Complete(nil)
			}
		})
	}
}
