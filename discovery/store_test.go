package discovery

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	serverlog "github.com/NumberMan1/log"
)

type watchReply struct {
	batch Batch
	err   error
}
type fakeWatcher struct {
	input  chan watchReply
	closed chan struct{}
	once   sync.Once
}

func (w *fakeWatcher) Next(ctx context.Context) (Batch, error) {
	select {
	case <-ctx.Done():
		return Batch{}, ctx.Err()
	case <-w.closed:
		return Batch{}, ErrWatchClosed
	case r, ok := <-w.input:
		if !ok {
			return Batch{}, ErrWatchClosed
		}
		return r.batch, r.err
	}
}
func (w *fakeWatcher) Close() { w.once.Do(func() { close(w.closed) }) }

type fakeBackend struct {
	mu        sync.Mutex
	result    ListResult
	listErr   error
	listFn    func(context.Context, int) (ListResult, error)
	lists     int
	watchers  []*fakeWatcher
	revisions []int64
}

func (b *fakeBackend) List(ctx context.Context) (ListResult, error) {
	b.mu.Lock()
	b.lists++
	call, fn, result, err := b.lists, b.listFn, b.result, b.listErr
	b.mu.Unlock()
	if fn != nil {
		return fn(ctx, call)
	}
	return result, err
}
func (b *fakeBackend) Watch(ctx context.Context, revision int64) (Watcher, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	w := &fakeWatcher{input: make(chan watchReply, 32), closed: make(chan struct{})}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.watchers = append(b.watchers, w)
	b.revisions = append(b.revisions, revision)
	return w, nil
}
func (b *fakeBackend) watcher(index int) *fakeWatcher {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.watchers[index]
}
func (b *fakeBackend) counts() (int, []int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lists, append([]int64(nil), b.revisions...)
}
func (b *fakeBackend) setList(result ListResult, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.result, b.listErr = result, err
}

func descriptor(id, value string) Descriptor {
	return Descriptor{ID: id, Name: "worker", Address: "127.0.0.1:1", Metadata: map[string]string{"value": value}}
}
func put(id, value string) Change { return Change{Type: Put, ID: id, Service: descriptor(id, value)} }
func options() StoreOptions {
	return StoreOptions{ResyncInterval: time.Minute, MaxStaleness: 2 * time.Minute, RetryMin: 10 * time.Millisecond, RetryMax: 20 * time.Millisecond, Logger: serverlog.NewServerLogger(nil)}
}
func startedStore(t *testing.T, b Backend, o StoreOptions) *Store {
	t.Helper()
	s, err := NewStore(b, o)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return s
}

func TestStoreSnapshotsAreOwnedAndTransactionsAreAtomic(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := &fakeBackend{result: ListResult{Revision: 10, Services: []Descriptor{descriptor("b", "old"), descriptor("a", "old")}}}
		var observed []Snapshot
		o := options()
		var s *Store
		o.Observer = func(e StoreEvent) {
			if e.Kind == "updated" {
				observed = append(observed, s.Snapshot())
			}
		}
		s = startedStore(t, b, o)
		synctest.Wait()
		if !s.Status().Ready {
			t.Fatal(s.Status())
		}
		all := s.All()
		if all[0].ID != "a" || all[1].ID != "b" {
			t.Fatal(all)
		}
		all[0].Metadata["value"] = "mutated"
		one, _ := s.Get("a")
		one.Metadata["value"] = "mutated"
		copy := s.Snapshot()
		delete(copy.Services, "b")
		copy.Services["a"].Metadata["value"] = "mutated"
		if got, _ := s.Get("a"); got.Metadata["value"] != "old" || len(s.All()) != 2 {
			t.Fatal("read API aliases stored state")
		}
		b.watcher(0).input <- watchReply{batch: Batch{Revision: 11, Changes: []Change{put("a", "new"), put("b", "new")}}}
		synctest.Wait()
		if len(observed) != 1 || observed[0].Revision != 11 || observed[0].Services["a"].Metadata["value"] != "new" || observed[0].Services["b"].Metadata["value"] != "new" {
			t.Fatal(observed)
		}
		b.watcher(0).input <- watchReply{batch: Batch{Revision: 10, Changes: []Change{put("a", "old")}}}
		b.watcher(0).input <- watchReply{batch: Batch{Revision: 11, Changes: []Change{{Type: Delete, ID: "b"}}}}
		synctest.Wait()
		if len(observed) != 1 || len(s.All()) != 2 {
			t.Fatal("replayed events changed snapshot")
		}
	})
}

func TestStoreResumesAfterTransientWatchFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := &fakeBackend{result: ListResult{Revision: 7, Services: []Descriptor{descriptor("a", "old")}}}
		s := startedStore(t, b, options())
		synctest.Wait()
		b.watcher(0).input <- watchReply{err: ErrWatchClosed}
		synctest.Wait()
		if s.Status().Ready || !errors.Is(s.Status().LastError, ErrWatchClosed) || s.Snapshot().Revision != 7 {
			t.Fatal(s.Status())
		}
		time.Sleep(100 * time.Millisecond)
		synctest.Wait()
		lists, revisions := b.counts()
		if lists != 1 || !reflect.DeepEqual(revisions, []int64{8, 8}) {
			t.Fatalf("lists=%d revisions=%v", lists, revisions)
		}
		select {
		case <-b.watcher(0).closed:
		default:
			t.Fatal("old watcher leaked")
		}
		b.watcher(1).input <- watchReply{batch: Batch{Revision: 8, Changes: []Change{put("a", "new")}}}
		synctest.Wait()
		if !s.Status().Ready || s.Status().LastError != nil || s.Snapshot().Revision != 8 {
			t.Fatal(s.Status())
		}
		if s.Err() != nil {
			t.Fatal("transient error became terminal")
		}
	})
}

func TestStoreCompactionAndFailedReloadKeepSnapshot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := &fakeBackend{result: ListResult{Revision: 10, Services: []Descriptor{descriptor("a", "old")}}}
		s := startedStore(t, b, options())
		synctest.Wait()
		unavailable := errors.New("backend unavailable")
		b.setList(ListResult{}, unavailable)
		b.watcher(0).input <- watchReply{err: ErrCompacted}
		synctest.Wait()
		time.Sleep(100 * time.Millisecond)
		synctest.Wait()
		lists, revisions := b.counts()
		if lists < 2 || len(revisions) != 1 || s.Status().Ready || s.Snapshot().Revision != 10 {
			t.Fatalf("counts=%d/%v status=%+v", lists, revisions, s.Status())
		}
		b.setList(ListResult{Revision: 20, Services: []Descriptor{descriptor("b", "reloaded")}}, nil)
		time.Sleep(100 * time.Millisecond)
		synctest.Wait()
		_, revisions = b.counts()
		if !s.Status().Ready || !reflect.DeepEqual(revisions, []int64{11, 21}) {
			t.Fatalf("%+v %v", s.Status(), revisions)
		}
		if _, ok := s.Get("a"); ok {
			t.Fatal("reload retained deleted node")
		}
	})
}

func TestStoreRejectsWholeInvalidBatchAndRegressingReload(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := &fakeBackend{result: ListResult{Revision: 10, Services: []Descriptor{descriptor("a", "old")}}}
		s := startedStore(t, b, options())
		synctest.Wait()
		b.setList(ListResult{Revision: 9}, nil)
		b.watcher(0).input <- watchReply{batch: Batch{Revision: 11, Changes: []Change{{Type: Delete, ID: "a"}, put("b", "valid"), {Type: Put, ID: "bad", Service: Descriptor{ID: "mismatch"}}}}}
		synctest.Wait()
		if _, ok := s.Get("a"); !ok {
			t.Fatal("partial deletion published")
		}
		if _, ok := s.Get("b"); ok {
			t.Fatal("partial addition published")
		}
		time.Sleep(100 * time.Millisecond)
		synctest.Wait()
		if s.Snapshot().Revision != 10 || s.Status().Ready || !errors.Is(s.Status().LastError, ErrInvalidData) {
			t.Fatal(s.Status())
		}
		b.setList(ListResult{Revision: 12}, nil)
		time.Sleep(100 * time.Millisecond)
		synctest.Wait()
		if !s.Status().Ready || len(s.All()) != 0 || s.Snapshot().Revision != 12 {
			t.Fatal(s.Status())
		}
	})
}

func TestStorePeriodicVerificationDetectsIdleOutage(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := &fakeBackend{listFn: func(ctx context.Context, call int) (ListResult, error) {
			if call == 1 {
				return ListResult{Revision: 1}, nil
			}
			<-ctx.Done()
			return ListResult{}, ctx.Err()
		}}
		o := options()
		o.ResyncInterval = 10 * time.Second
		o.MaxStaleness = 20 * time.Second
		o.RequestTimeout = time.Second
		s := startedStore(t, b, o)
		synctest.Wait()
		time.Sleep(12 * time.Second)
		synctest.Wait()
		if s.Status().Ready || s.Status().LastError == nil || s.Status().Revision != 1 {
			t.Fatal(s.Status())
		}
		lists, _ := b.counts()
		if lists < 2 {
			t.Fatal("idle watch never verified")
		}
	})
}

func TestStoreStopCancelsStartupAndRetry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := &fakeBackend{listFn: func(ctx context.Context, _ int) (ListResult, error) { <-ctx.Done(); return ListResult{}, ctx.Err() }}
		s, _ := NewStore(b, options())
		started := make(chan error, 1)
		go func() { started <- s.Start(t.Context()) }()
		synctest.Wait()
		if err := s.Stop(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := <-started; !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if err := s.Start(t.Context()); err == nil {
			t.Fatal("restarted a closed store")
		}
		b2 := &fakeBackend{result: ListResult{Revision: 1}}
		o := options()
		o.RetryMin = time.Hour
		o.RetryMax = time.Hour
		s2 := startedStore(t, b2, o)
		synctest.Wait()
		b2.watcher(0).input <- watchReply{err: ErrWatchClosed}
		synctest.Wait()
		before := time.Now()
		if err := s2.Stop(t.Context()); err != nil {
			t.Fatal(err)
		}
		if time.Since(before) != 0 {
			t.Fatal("stop waited for retry timer")
		}
	})
}

func TestStoreObserverPanicAndBoundedStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := &fakeBackend{result: ListResult{Revision: 1}}
		o := options()
		release := make(chan struct{})
		o.Observer = func(event StoreEvent) {
			if event.Kind == "loaded" {
				<-release
				panic("observer")
			}
		}
		s := startedStore(t, b, o)
		synctest.Wait()
		time.Sleep(3 * time.Minute)
		if s.Status().Ready {
			t.Fatal("old snapshot reported ready while observer stalled")
		}
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		if err := s.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
		close(release)
		if err := s.Stop(t.Context()); err != nil {
			t.Fatal(err)
		}
		if s.Err() != nil {
			t.Fatal("observer panic became terminal")
		}
	})
}

func TestStoreConcurrentReadersSeeCompleteRevision(t *testing.T) {
	b := &fakeBackend{result: ListResult{Revision: 1, Services: []Descriptor{descriptor("a", "1"), descriptor("b", "1")}}}
	notifications := make(chan StoreEvent, 128)
	o := options()
	o.Observer = func(event StoreEvent) { notifications <- event }
	s := startedStore(t, b, o)
	// Start's initial notification precedes Watch creation; a small polling loop
	// is only used here, outside virtual time, for this real concurrent race test.
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, revisions := b.counts()
		if len(revisions) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("watch not started")
		}
		time.Sleep(time.Millisecond)
	}
	var readers sync.WaitGroup
	for range 8 {
		readers.Go(func() {
			for range 500 {
				snapshot := s.Snapshot()
				if len(snapshot.Services) != 2 || snapshot.Services["a"].Metadata["value"] != snapshot.Services["b"].Metadata["value"] {
					t.Error("partial transaction observed")
					return
				}
				snapshot.Services["a"].Metadata["value"] = "caller mutation"
			}
		})
	}
	for revision := int64(2); revision <= 50; revision++ {
		value := strconv.FormatInt(revision, 10)
		b.watcher(0).input <- watchReply{batch: Batch{Revision: revision, Changes: []Change{put("a", value), put("b", value)}}}
	}
	for s.Status().Revision < 50 {
		select {
		case <-notifications:
		case <-time.After(2 * time.Second):
			t.Fatal("updates stalled")
		}
	}
	readers.Wait()
}

func TestStoreInitialFailureAndEmptySnapshot(t *testing.T) {
	for _, result := range []ListResult{{}, {Revision: 1, Services: []Descriptor{descriptor("a", "x"), descriptor("a", "y")}}} {
		s, _ := NewStore(&fakeBackend{result: result}, options())
		if err := s.Start(t.Context()); !errors.Is(err, ErrInvalidData) {
			t.Fatal(err)
		}
		select {
		case <-s.Done():
		default:
			t.Fatal("failed start not completed")
		}
	}
	s := startedStore(t, &fakeBackend{result: ListResult{Revision: 1}}, options())
	if len(s.All()) != 0 || !s.Status().Ready {
		t.Fatal(s.Status())
	}
}

type watchBackend struct {
	Backend
	watch func(context.Context, int64) (Watcher, error)
}

func (b watchBackend) Watch(ctx context.Context, revision int64) (Watcher, error) {
	return b.watch(ctx, revision)
}

func TestStoreWatchCreationFailureAndProgressRecovery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := &fakeBackend{result: ListResult{Revision: 10}}
		calls := 0
		disconnected := errors.New("watch setup failed")
		backend := watchBackend{Backend: b, watch: func(ctx context.Context, revision int64) (Watcher, error) {
			calls++
			if calls == 1 {
				return nil, disconnected
			}
			return b.Watch(ctx, revision)
		}}
		s := startedStore(t, backend, options())
		synctest.Wait()
		if !errors.Is(s.Status().LastError, disconnected) || s.Status().Ready {
			t.Fatal(s.Status())
		}
		time.Sleep(100 * time.Millisecond)
		synctest.Wait()
		if s.Status().Ready {
			t.Fatal("creating a watcher alone is not synchronization evidence")
		}
		b.watcher(0).input <- watchReply{batch: Batch{Revision: 10}}
		synctest.Wait()
		if !s.Status().Ready || s.Status().LastError != nil {
			t.Fatal(s.Status())
		}
		// A healthy idle directory stays ready via periodic verification.
		time.Sleep(3 * time.Minute)
		synctest.Wait()
		lists, revisions := b.counts()
		if !s.Status().Ready || lists < 3 || revisions[len(revisions)-1] != 11 {
			t.Fatalf("status=%+v lists=%d revisions=%v", s.Status(), lists, revisions)
		}
	})
}

func TestStoreBackendPanicCompletesLifecycle(t *testing.T) {
	for _, startup := range []bool{true, false} {
		synctest.Test(t, func(t *testing.T) {
			var backend Backend = &fakeBackend{result: ListResult{Revision: 1}}
			if startup {
				backend = &fakeBackend{listFn: func(context.Context, int) (ListResult, error) { panic("list failed") }}
			} else {
				backend = watchBackend{Backend: backend, watch: func(context.Context, int64) (Watcher, error) { panic("watch failed") }}
			}
			s, err := NewStore(backend, options())
			if err != nil {
				t.Fatal(err)
			}
			err = s.Start(t.Context())
			if startup != (err != nil) {
				t.Fatalf("startup=%v error=%v", startup, err)
			}
			synctest.Wait()
			select {
			case <-s.Done():
			default:
				t.Fatal("backend panic left Store running")
			}
			if s.Status().Running || s.Status().Ready || s.Status().LastError == nil || s.Err() == nil {
				t.Fatal(s.Status())
			}
			if err := s.Stop(t.Context()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
