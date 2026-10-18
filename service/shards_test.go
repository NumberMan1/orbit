package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"testing/synctest"
	"time"
)

func TestShardsOrderingParallelismBackpressureAndDrain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, err := NewShards(2, 2, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		gate := make(chan struct{})
		entered := make(chan struct{})
		other := make(chan struct{})
		var order []int
		if err = s.Post("a", func() { close(entered); <-gate; order = append(order, 0) }); err != nil {
			t.Fatal(err)
		}
		<-entered
		for i := 1; i <= 2; i++ {
			if err = s.Post("a", func() { order = append(order, i) }); err != nil {
				t.Fatal(err)
			}
		}
		if err = s.Post("a", func() {}); !errors.Is(err, ErrFull) {
			t.Fatal(err)
		}
		// Adjacent ASCII keys map to different parity with FNV-1a and two shards.
		if err = s.Post("b", func() { close(other); panic("isolated") }); err != nil {
			t.Fatal(err)
		}
		<-other
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		done := make(chan error, 1)
		go func() { done <- s.Stop(ctx) }()
		synctest.Wait()
		if err = s.Post("b", func() {}); !errors.Is(err, ErrClosed) {
			t.Fatal(err)
		}
		close(gate)
		if err = <-done; err != nil {
			t.Fatal(err)
		}
		cancel()
		if !reflect.DeepEqual(order, []int{0, 1, 2}) {
			t.Fatal(order)
		}
		var panics uint64
		for _, q := range s.Status() {
			panics += q.Panics
		}
		if panics != 1 {
			t.Fatal(panics)
		}
	})
}
