package discovery

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/NumberMan1/orbit"
)

type testPeer struct{ starts, stops int }

func (p *testPeer) Start(context.Context) error { p.starts++; return nil }
func (p *testPeer) Stop(context.Context) error  { p.stops++; return nil }
func TestPoolReconcilesAndRetainsPeersDuringOutage(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		backend := &fakeBackend{result: ListResult{Revision: 1, Services: []Descriptor{descriptor("a", "first")}}}
		store := startedStore(t, backend, options())
		synctest.Wait()
		var created []*testPeer
		pool, err := NewPool(store, func(d Descriptor) (orbit.Component, error) {
			d.Metadata["value"] = "factory mutation"
			peer := &testPeer{}
			created = append(created, peer)
			return peer, nil
		}, PoolOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if err := pool.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		defer pool.Stop(t.Context())
		synctest.Wait()
		time.Sleep(2 * time.Second)
		synctest.Wait()
		if len(created) != 1 || pool.Status().Count != 1 {
			t.Fatal("unchanged peer replaced", len(created), pool.Status())
		}
		backend.watcher(0).input <- watchReply{batch: Batch{Revision: 2, Changes: []Change{put("a", "new"), put("b", "new")}}}
		synctest.Wait()
		time.Sleep(time.Second)
		synctest.Wait()
		if len(created) != 3 || created[0].stops != 1 || pool.Status().Count != 2 {
			t.Fatal("replacement failed", len(created), pool.Status())
		}
		backend.watcher(0).input <- watchReply{err: ErrWatchClosed}
		synctest.Wait()
		time.Sleep(time.Second)
		synctest.Wait()
		if pool.Status().LastError == nil || pool.Status().Count != 2 {
			t.Fatal("outage deleted peers", pool.Status())
		}
		backend.watcher(1).input <- watchReply{batch: Batch{Revision: 3, Changes: []Change{{Type: Delete, ID: "b"}}}}
		synctest.Wait()
		time.Sleep(time.Second)
		synctest.Wait()
		if pool.Status().Count != 1 || pool.Status().LastError != nil {
			t.Fatal(pool.Status())
		}
		pool.Stop(t.Context())
		if pool.Status().Count != 0 || created[1].stops != 1 || created[2].stops != 1 {
			t.Fatal("peer shutdown failed")
		}
	})
}

type failingPeer struct{ stops int }

func (p *failingPeer) Start(context.Context) error { return errors.New("start failed") }
func (p *failingPeer) Stop(context.Context) error {
	p.stops++
	if p.stops < 3 {
		return errors.New("still stopping")
	}
	return nil
}
func TestPoolDoesNotReplacePeerUntilCleanupCompletes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := startedStore(t, &fakeBackend{result: ListResult{Revision: 1, Services: []Descriptor{descriptor("a", "first")}}}, options())
		attempts := 0
		failed := &failingPeer{}
		pool, _ := NewPool(store, func(Descriptor) (orbit.Component, error) {
			attempts++
			if attempts == 1 {
				return failed, nil
			}
			return &testPeer{}, nil
		}, PoolOptions{})
		pool.Start(t.Context())
		defer pool.Stop(t.Context())
		synctest.Wait()
		time.Sleep(time.Second)
		synctest.Wait()
		if attempts != 1 || pool.Status().LastError == nil {
			t.Fatal("replaced before cleanup", attempts, pool.Status())
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if attempts != 2 || failed.stops != 3 || pool.Status().LastError != nil {
			t.Fatal(attempts, failed.stops, pool.Status())
		}
	})
}
