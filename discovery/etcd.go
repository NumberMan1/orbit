// Package discovery registers neutral service descriptors and watches etcd snapshots.
package discovery

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

type Descriptor struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Address  string            `json:"address"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

func (d Descriptor) Validate() error {
	if d.ID == "" || strings.ContainsAny(d.ID, "/\\") || d.Name == "" || d.Address == "" {
		return errors.New("discovery: ID, name and address required; ID cannot contain slashes")
	}
	return nil
}

type Registry struct {
	client *clientv3.Client
	prefix string
}

// New uses a caller-owned client; Registry does not close it.
func New(client *clientv3.Client, prefix string) (*Registry, error) {
	if client == nil || !strings.HasPrefix(prefix, "/") || strings.Trim(prefix, "/") == "" {
		return nil, errors.New("discovery: client and non-root absolute prefix required")
	}
	return &Registry{client: client, prefix: strings.TrimRight(prefix, "/") + "/"}, nil
}

var ErrLeaseLost = errors.New("discovery: lease lost; registration must be renewed")
var ErrIDInUse = errors.New("discovery: service ID already registered")

type Registration struct {
	client  *clientv3.Client
	key, id string
	lease   clientv3.LeaseID
	cancel  context.CancelFunc
	done    chan struct{}
	mu      sync.Mutex
	err     error
}

// Register atomically claims an ID and keeps its lease alive until cancellation.
// Lease loss is observable through Done/Err; callers can retry registration with
// their own backoff and readiness policy. It never overwrites another process.
func (r *Registry) Register(ctx context.Context, d Descriptor, ttl int64) (*Registration, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	if ttl < 3 {
		return nil, errors.New("discovery: TTL must be at least 3 seconds")
	}
	data, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	requestCtx, cancelRequest := context.WithTimeout(ctx, 5*time.Second)
	defer cancelRequest()
	lease, err := r.client.Grant(requestCtx, ttl)
	if err != nil {
		return nil, err
	}
	revoke := func() error {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, err := r.client.Revoke(cleanupCtx, lease.ID)
		return err
	}
	key := r.prefix + d.ID
	res, err := r.client.Txn(requestCtx).If(clientv3.Compare(clientv3.Version(key), "=", 0)).Then(clientv3.OpPut(key, string(data), clientv3.WithLease(lease.ID))).Commit()
	if err != nil {
		return nil, errors.Join(err, revoke())
	}
	if !res.Succeeded {
		return nil, errors.Join(ErrIDInUse, revoke())
	}
	keepCtx, cancel := context.WithCancel(ctx)
	keep, err := r.client.KeepAlive(keepCtx, lease.ID)
	if err != nil {
		cancel()
		return nil, errors.Join(err, revoke())
	}
	registration := &Registration{client: r.client, key: key, id: d.ID, lease: lease.ID, cancel: cancel, done: make(chan struct{})}
	// A live lease alone does not prove that its key still exists or belongs to
	// us. Watch from the successful claim revision to close the setup gap.
	ownership := r.client.Watch(clientv3.WithRequireLeader(keepCtx), key, clientv3.WithRev(res.Header.Revision+1))
	go func() {
		var result error
	loop:
		for {
			select {
			case <-keepCtx.Done():
				break loop
			case response, ok := <-keep:
				if !ok || response == nil || response.TTL <= 0 {
					if keepCtx.Err() == nil {
						result = ErrLeaseLost
					}
					break loop
				}
			case response, ok := <-ownership:
				if keepCtx.Err() != nil {
					break loop
				}
				if !ok || response.Err() != nil {
					result = errors.Join(ErrLeaseLost, response.Err())
					break loop
				}
				for _, event := range response.Events {
					if event.Type == clientv3.EventTypeDelete || event.Kv.Lease != int64(lease.ID) {
						result = ErrLeaseLost
						break loop
					}
				}
			}
		}
		cancel()
		result = errors.Join(result, revoke())
		registration.mu.Lock()
		registration.err = result
		registration.mu.Unlock()
		close(registration.done)
	}()
	return registration, nil
}

func (r *Registration) Done() <-chan struct{} { return r.done }

// Update publishes metadata/address changes only while this lease still owns
// the key. It never overwrites a successor registration or changes the ID.
func (r *Registration) Update(ctx context.Context, d Descriptor) error {
	if err := d.Validate(); err != nil {
		return err
	}
	if d.ID != r.id {
		return errors.New("discovery: registration ID cannot change")
	}
	data, err := json.Marshal(d)
	if err != nil {
		return err
	}
	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result, err := r.client.Txn(requestCtx).If(clientv3.Compare(clientv3.LeaseValue(r.key), "=", int64(r.lease))).Then(clientv3.OpPut(r.key, string(data), clientv3.WithLease(r.lease))).Commit()
	if err != nil {
		return err
	}
	if !result.Succeeded {
		return ErrLeaseLost
	}
	return nil
}
func (r *Registration) Err() error { r.mu.Lock(); defer r.mu.Unlock(); return r.err }
func (r *Registration) Close(ctx context.Context) error {
	r.cancel()
	select {
	case <-r.done:
		return r.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Watch emits a complete snapshot initially and after each change. Each snapshot
// and its metadata belong to the callback. Callbacks are serial and must return
// promptly. Watching starts at the list revision+1, avoiding the list/watch gap.
// Compaction rebuilds the snapshot. Other errors return for caller supervision.
func (r *Registry) Watch(ctx context.Context, update func(map[string]Descriptor) error) error {
	if update == nil {
		return errors.New("discovery: callback required")
	}
	backend := &etcdBackend{registry: r}
	for ctx.Err() == nil {
		listCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		result, err := backend.List(listCtx)
		cancel()
		if err != nil {
			return err
		}
		snapshot, err := snapshotFromList(result, 0)
		if err != nil {
			return err
		}
		if err := update(clone(snapshot.Services)); err != nil {
			return err
		}
		watcher, err := backend.Watch(ctx, snapshot.Revision+1)
		if err != nil {
			return err
		}
		err = func() error {
			defer watcher.Close()
			for {
				batch, err := watcher.Next(ctx)
				if err != nil {
					return err
				}
				next, err := applyBatch(snapshot, batch)
				if err != nil {
					return err
				}
				changed := next != snapshot && len(batch.Changes) > 0
				snapshot = next
				if changed {
					if err := update(clone(snapshot.Services)); err != nil {
						return err
					}
				}
			}
		}()
		if !errors.Is(err, ErrCompacted) {
			return err
		}
	}
	return ctx.Err()
}

func (r *Registry) decode(key string, data []byte) (Descriptor, error) {
	var d Descriptor
	if err := json.Unmarshal(data, &d); err != nil {
		return d, fmt.Errorf("discovery: invalid descriptor at %s: %w", key, err)
	}
	if err := d.Validate(); err != nil {
		return d, err
	}
	if key != r.prefix+d.ID {
		return d, errors.New("discovery: key and descriptor ID differ")
	}
	return d, nil
}

func clone(state map[string]Descriptor) map[string]Descriptor {
	result := maps.Clone(state)
	for id, d := range result {
		d.Metadata = maps.Clone(d.Metadata)
		result[id] = d
	}
	return result
}
