package discovery

import (
	"context"
	"errors"
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/NumberMan1/orbit"
	"github.com/NumberMan1/orbit/socket"
	"github.com/NumberMan1/orbit/socket/tcp"
)

type sessionPeer struct{ s *socket.Session }

func (p *sessionPeer) Start(context.Context) error      { return nil }
func (p *sessionPeer) Stop(context.Context) error       { p.s.Close(); return nil }
func (p *sessionPeer) Session() (*socket.Session, bool) { return p.s, p.s.Context().Err() == nil }

func TestPickerFiltersDisconnectedAndStableAffinity(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		backend := &fakeBackend{result: ListResult{Revision: 1, Services: []Descriptor{descriptor("a", "first"), descriptor("b", "second")}}}
		store := startedStore(t, backend, options())
		peers := map[string]*sessionPeer{}
		pool, err := NewPool(store, func(d Descriptor) (orbit.Component, error) {
			a, b := net.Pipe()
			t.Cleanup(func() { b.Close() })
			s, err := socket.NewSession(t.Context(), tcp.Transport{Conn: a}, socket.Options{})
			if err != nil {
				return nil, err
			}
			p := &sessionPeer{s}
			peers[d.ID] = p
			return p, nil
		}, PoolOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if err := pool.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		defer pool.Stop(t.Context())
		synctest.Wait()
		picker, _ := NewPicker(pool)
		_, a, err := picker.Pick("", "")
		if err != nil {
			t.Fatal(err)
		}
		_, b, err := picker.Pick("", "")
		if err != nil || a.ID == b.ID {
			t.Fatal(a, b, err)
		}
		_, chosen, err := picker.Pick("", "entity-42")
		if err != nil {
			t.Fatal(err)
		}
		for range 10 {
			_, d, err := picker.Pick("", "entity-42")
			if err != nil || d.ID != chosen.ID {
				t.Fatal(d, err)
			}
		}
		if _, _, err := picker.Pick("missing", ""); !errors.Is(err, ErrNoPeer) {
			t.Fatal(err)
		}
		peers[chosen.ID].s.Close()
		_, remaining, err := picker.Pick("", "entity-42")
		if err != nil || remaining.ID == chosen.ID {
			t.Fatal(remaining, err)
		}
		backend.watcher(0).input <- watchReply{err: ErrWatchClosed}
		synctest.Wait()
		if _, _, err := picker.Pick("", ""); !errors.Is(err, ErrNoPeer) {
			t.Fatal("unhealthy directory selected", err)
		}
		time.Sleep(time.Millisecond)
		synctest.Wait()
	})
}
