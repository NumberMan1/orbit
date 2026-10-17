package service

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	serverlog "github.com/NumberMan1/log"
)

func TestQueueDrainPanicAndBackpressure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logger := serverlog.NewServerLogger(nil)
		q, err := NewQueue(1, 0, nil, logger)
		if err != nil {
			t.Fatal(err)
		}
		if err := q.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		blocked := make(chan struct{})
		if err := q.Post(func() { <-blocked; panic("test") }); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		n := 0
		if err := q.Post(func() { n++ }); err != nil {
			t.Fatal(err)
		}
		if err := q.Post(func() {}); !errors.Is(err, ErrFull) {
			t.Fatal(err)
		}
		close(blocked)
		if err := q.Stop(t.Context()); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("drain = %d", n)
		}
		if status := q.Status(); status.Accepting || status.Queued != 0 || status.Executed != 2 || status.Panics != 1 || status.Rejected != 1 {
			t.Fatal(status)
		}
		if err := q.Post(func() {}); !errors.Is(err, ErrClosed) {
			t.Fatal(err)
		}
		if err := q.Stop(t.Context()); err != nil {
			t.Fatal(err)
		}
	})
}

func TestQueueConcurrentStopPost(t *testing.T) {
	q, _ := NewQueue(32, 0, nil, nil)
	_ = q.Start(t.Context())
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			for range 100 {
				_ = q.Post(func() {})
			}
		})
	}
	wg.Go(func() { _ = q.Stop(t.Context()) })
	wg.Wait()
	if err := q.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
}

type component struct {
	name string
	log  *[]string
	fail bool
}

func (c component) Start(context.Context) error {
	*c.log = append(*c.log, "start "+c.name)
	if c.fail {
		return errors.New("failure")
	}
	return nil
}
func (c component) Stop(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	*c.log = append(*c.log, "stop "+c.name)
	return nil
}
func TestAppRollback(t *testing.T) {
	var log []string
	a, _ := NewApp(time.Second, component{"a", &log, false}, component{"b", &log, false}, component{"c", &log, true})
	if err := a.Run(t.Context()); err == nil {
		t.Fatal("startup failure lost")
	}
	if want := []string{"start a", "start b", "start c", "stop b", "stop a"}; !reflect.DeepEqual(log, want) {
		t.Fatalf("%v", log)
	}
	if err := a.Run(t.Context()); err == nil {
		t.Fatal("reuse accepted")
	}
}

type monitored struct {
	component
	done chan struct{}
}

func (m monitored) Done() <-chan struct{} { return m.done }
func (m monitored) Err() error            { return errors.New("listener failed") }
func TestAppMonitorsFailure(t *testing.T) {
	var log []string
	done := make(chan struct{})
	close(done)
	a, _ := NewApp(time.Second, monitored{component{"a", &log, false}, done})
	if err := a.Run(t.Context()); err == nil {
		t.Fatal("runtime failure lost")
	}
	if !reflect.DeepEqual(log, []string{"start a", "stop a"}) {
		t.Fatal(log)
	}
}
