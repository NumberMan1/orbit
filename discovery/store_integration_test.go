//go:build integration

package discovery

import (
	"context"
	"encoding/json/v2"
	"errors"
	"testing"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

func TestEtcdStoreTransactionsRecoveryAndClientOwnership(t *testing.T) {
	client := testEtcd(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	value := func(id, version string) string {
		t.Helper()
		data, err := json.Marshal(descriptor(id, version))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	t.Run("backend revisions and compaction", func(t *testing.T) {
		backend, err := NewEtcdBackend(client, "/backend")
		if err != nil {
			t.Fatal(err)
		}
		initial, err := backend.List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		// Commit before Watch is created, exercising replay across the List/Watch gap.
		txn, err := client.Txn(ctx).Then(clientv3.OpPut("/backend/a", value("a", "1")), clientv3.OpPut("/backend/b", value("b", "1"))).Commit()
		if err != nil {
			t.Fatal(err)
		}
		watcher, err := backend.Watch(ctx, initial.Revision+1)
		if err != nil {
			t.Fatal(err)
		}
		defer watcher.Close()
		batch, err := watcher.Next(ctx)
		if err != nil || batch.Revision != txn.Header.Revision || len(batch.Changes) != 2 {
			t.Fatalf("transaction was split or lost: %+v, %v", batch, err)
		}
		latest, err := client.Put(ctx, "/backend/a", value("a", "2"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.Compact(ctx, latest.Header.Revision); err != nil {
			t.Fatal(err)
		}
		old, err := backend.Watch(ctx, txn.Header.Revision)
		if err != nil {
			t.Fatal(err)
		}
		defer old.Close()
		if _, err := old.Next(ctx); !errors.Is(err, ErrCompacted) {
			t.Fatalf("compaction: %v", err)
		}
	})

	t.Run("managed cache rejects bad data and recovers", func(t *testing.T) {
		registry, err := New(client, "/store")
		if err != nil {
			t.Fatal(err)
		}
		s, err := registry.NewStore(StoreOptions{RetryMin: 10 * time.Millisecond, RetryMax: 50 * time.Millisecond})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Start(ctx); err != nil {
			t.Fatal(err)
		}
		defer s.Stop(ctx)
		await := func(predicate func() bool) {
			t.Helper()
			ticker := time.NewTicker(5 * time.Millisecond)
			defer ticker.Stop()
			for !predicate() {
				select {
				case <-ctx.Done():
					t.Fatalf("waiting for Store: %+v", s.Status())
				case <-ticker.C:
				}
			}
		}
		txn, err := client.Txn(ctx).Then(clientv3.OpPut("/store/a", value("a", "1")), clientv3.OpPut("/store/b", value("b", "1"))).Commit()
		if err != nil {
			t.Fatal(err)
		}
		await(func() bool { return s.Status().Revision == txn.Header.Revision })
		if snapshot := s.Snapshot(); len(snapshot.Services) != 2 || !s.Status().Ready {
			t.Fatalf("incomplete transaction: %+v", snapshot)
		}
		// A valid deletion and an invalid put must never become a partial snapshot.
		if _, err := client.Txn(ctx).Then(clientv3.OpDelete("/store/a"), clientv3.OpPut("/store/b", "invalid-json")).Commit(); err != nil {
			t.Fatal(err)
		}
		await(func() bool { return errors.Is(s.Status().LastError, ErrInvalidData) })
		if snapshot := s.Snapshot(); snapshot.Revision != txn.Header.Revision || len(snapshot.Services) != 2 || s.Status().Ready {
			t.Fatalf("bad data replaced valid snapshot: %+v", snapshot)
		}
		repaired, err := client.Put(ctx, "/store/b", value("b", "2"))
		if err != nil {
			t.Fatal(err)
		}
		await(func() bool { status := s.Status(); return status.Ready && status.Revision == repaired.Header.Revision })
		if snapshot := s.Snapshot(); len(snapshot.Services) != 1 || snapshot.Services["b"].Metadata["value"] != "2" {
			t.Fatalf("recovery did not reload: %+v", snapshot)
		}
		registration, err := registry.Register(ctx, descriptor("leased", "1"), 3)
		if err != nil {
			t.Fatal(err)
		}
		defer registration.Close(ctx)
		await(func() bool { _, ok := s.Get("leased"); return ok })
		if err := registration.Close(ctx); err != nil {
			t.Fatal(err)
		}
		await(func() bool { _, ok := s.Get("leased"); return !ok })
		if err := s.Stop(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := client.Get(ctx, "/store", clientv3.WithPrefix()); err != nil {
			t.Fatalf("Store closed caller-owned client: %v", err)
		}
	})
}
