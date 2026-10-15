package discovery

import (
	"context"
	"encoding/json/v2"
	"errors"
	"testing"

	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

func wireEvent(t *testing.T, revision int64, id string) *clientv3.Event {
	t.Helper()
	data, err := json.Marshal(descriptor(id, "value"))
	if err != nil {
		t.Fatal(err)
	}
	return &clientv3.Event{Type: clientv3.EventTypePut, Kv: &mvccpb.KeyValue{Key: []byte("/test/" + id), Value: data, ModRevision: revision}}
}

func TestEtcdWatcherIgnoresCreatedAndPreservesTransactionRevisions(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	responses := make(chan clientv3.WatchResponse, 3)
	w := &etcdWatcher{registry: &Registry{prefix: "/test/"}, responses: responses, ctx: ctx, cancel: cancel}
	created := clientv3.WatchResponse{Created: true}
	created.Header.Revision = 100
	responses <- created
	responses <- clientv3.WatchResponse{Events: []*clientv3.Event{wireEvent(t, 11, "a"), wireEvent(t, 11, "b"), wireEvent(t, 12, "a")}}
	progress := clientv3.WatchResponse{}
	progress.Header.Revision = 13
	responses <- progress
	for _, want := range []struct {
		revision int64
		count    int
	}{{11, 2}, {12, 1}, {13, 0}} {
		batch, err := w.Next(t.Context())
		if err != nil || batch.Revision != want.revision || len(batch.Changes) != want.count {
			t.Fatalf("%+v %v", batch, err)
		}
	}
	w.Close()
	w.Close()
	if _, err := w.Next(t.Context()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestEtcdWatcherRejectsMalformedDataAndMapsCompaction(t *testing.T) {
	for _, malformed := range [][]*clientv3.Event{
		{nil},
		{{Type: clientv3.EventTypePut, Kv: &mvccpb.KeyValue{Key: []byte("/test/a"), Value: []byte("bad-json"), ModRevision: 1}}},
		{{Type: clientv3.EventTypeDelete, Kv: &mvccpb.KeyValue{Key: []byte("/other/a"), ModRevision: 1}}},
		{wireEvent(t, 2, "a"), wireEvent(t, 1, "b")},
	} {
		w := &etcdWatcher{registry: &Registry{prefix: "/test/"}}
		if _, err := w.decodeBatches(malformed); !errors.Is(err, ErrInvalidData) {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	responses := make(chan clientv3.WatchResponse, 1)
	responses <- clientv3.WatchResponse{CompactRevision: 20, Canceled: true}
	w := &etcdWatcher{registry: &Registry{prefix: "/test/"}, responses: responses, ctx: ctx, cancel: cancel}
	if _, err := w.Next(t.Context()); !errors.Is(err, ErrCompacted) {
		t.Fatal(err)
	}
}
