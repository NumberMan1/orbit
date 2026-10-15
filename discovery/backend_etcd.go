package discovery

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type etcdBackend struct{ registry *Registry }

// NewEtcdBackend borrows client. It never closes that client. Reads are
// linearizable; watches require a leader and are resumed from explicit revisions.
func NewEtcdBackend(client *clientv3.Client, prefix string) (Backend, error) {
	r, err := New(client, prefix)
	if err != nil {
		return nil, err
	}
	return &etcdBackend{registry: r}, nil
}

// NewStore creates a managed cache for the same directory as this Registry.
// Lease registration remains independent; Store.Stop does not revoke leases.
func (r *Registry) NewStore(options StoreOptions) (*Store, error) {
	return NewStore(&etcdBackend{registry: r}, options)
}

func (b *etcdBackend) List(ctx context.Context) (ListResult, error) {
	r := b.registry
	response, err := r.client.Get(clientv3.WithRequireLeader(ctx), r.prefix, clientv3.WithPrefix())
	if err != nil {
		return ListResult{}, err
	}
	result := ListResult{Revision: response.Header.Revision, Services: make([]Descriptor, 0, len(response.Kvs))}
	for _, kv := range response.Kvs {
		d, err := r.decode(string(kv.Key), kv.Value)
		if err != nil {
			return ListResult{}, fmt.Errorf("%w: %w", ErrInvalidData, err)
		}
		result.Services = append(result.Services, d)
	}
	return result, nil
}

func (b *etcdBackend) Watch(ctx context.Context, revision int64) (Watcher, error) {
	if revision < 1 {
		return nil, fmt.Errorf("%w: positive watch revision required", ErrInvalidData)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	watchCtx, cancel := context.WithCancel(clientv3.WithRequireLeader(ctx))
	r := b.registry
	responses := r.client.Watch(watchCtx, r.prefix, clientv3.WithPrefix(), clientv3.WithRev(revision), clientv3.WithProgressNotify())
	return &etcdWatcher{registry: r, responses: responses, cancel: cancel, ctx: watchCtx}, nil
}

type etcdWatcher struct {
	registry  *Registry
	responses clientv3.WatchChan
	cancel    context.CancelFunc
	ctx       context.Context
	pending   []Batch // only accessed by Next
}

func (w *etcdWatcher) Close() { w.cancel() }

func (w *etcdWatcher) Next(ctx context.Context) (Batch, error) {
	for {
		if err := ctx.Err(); err != nil {
			return Batch{}, err
		}
		if err := w.ctx.Err(); err != nil {
			return Batch{}, err
		}
		if len(w.pending) > 0 {
			batch := w.pending[0]
			w.pending[0] = Batch{}
			w.pending = w.pending[1:]
			return batch, nil
		}
		select {
		case <-ctx.Done():
			return Batch{}, ctx.Err()
		case <-w.ctx.Done():
			return Batch{}, w.ctx.Err()
		case response, ok := <-w.responses:
			if !ok {
				return Batch{}, ErrWatchClosed
			}
			if response.CompactRevision > 0 || errors.Is(response.Err(), rpctypes.ErrCompacted) {
				return Batch{}, ErrCompacted
			}
			if err := response.Err(); err != nil {
				return Batch{}, err
			}
			// Created headers may be ahead of replayed events. They are NOT a
			// progress watermark and must never advance the stored revision.
			if response.Created {
				continue
			}
			if len(response.Events) == 0 {
				if response.IsProgressNotify() {
					return Batch{Revision: response.Header.Revision}, nil
				}
				continue
			}
			batches, err := w.decodeBatches(response.Events)
			if err != nil {
				return Batch{}, err
			}
			w.pending = batches
		}
	}
}

func (w *etcdWatcher) decodeBatches(events []*clientv3.Event) ([]Batch, error) {
	var batches []Batch
	for _, event := range events {
		if event == nil || event.Kv == nil || event.Kv.ModRevision < 1 {
			return nil, ErrInvalidData
		}
		revision := event.Kv.ModRevision
		if len(batches) == 0 || batches[len(batches)-1].Revision != revision {
			if len(batches) > 0 && revision < batches[len(batches)-1].Revision {
				return nil, ErrInvalidData
			}
			batches = append(batches, Batch{Revision: revision})
		}
		key := string(event.Kv.Key)
		if !strings.HasPrefix(key, w.registry.prefix) {
			return nil, ErrInvalidData
		}
		id := strings.TrimPrefix(key, w.registry.prefix)
		if id == "" || strings.ContainsAny(id, "/\\") {
			return nil, ErrInvalidData
		}
		change := Change{ID: id}
		switch event.Type {
		case clientv3.EventTypeDelete:
			change.Type = Delete
		case clientv3.EventTypePut:
			d, err := w.registry.decode(key, event.Kv.Value)
			if err != nil {
				return nil, fmt.Errorf("%w: %w", ErrInvalidData, err)
			}
			change.Type, change.Service = Put, d
		default:
			return nil, ErrInvalidData
		}
		index := len(batches) - 1
		batches[index].Changes = append(batches[index].Changes, change)
	}
	return batches, nil
}
