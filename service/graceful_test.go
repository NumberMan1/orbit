package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"testing/synctest"
	"time"
)

type gracefulComponent struct {
	name            string
	log             *[]string
	started         chan struct{}
	runtime         context.Context
	waitForDeadline bool
}

func (c *gracefulComponent) Start(ctx context.Context) error {
	c.runtime = ctx
	*c.log = append(*c.log, "start "+c.name)
	if c.started != nil {
		close(c.started)
	}
	return nil
}
func (c *gracefulComponent) Quiesce() { *c.log = append(*c.log, "quiesce "+c.name) }
func (c *gracefulComponent) Drain(ctx context.Context) error {
	*c.log = append(*c.log, "drain "+c.name)
	if c.runtime.Err() != nil {
		return errors.New("runtime canceled before drain")
	}
	if c.waitForDeadline {
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}
func (c *gracefulComponent) Stop(context.Context) error {
	*c.log = append(*c.log, "stop "+c.name)
	if c.runtime.Err() == nil {
		return errors.New("runtime not canceled before Stop")
	}
	return nil
}

func TestGracefulAppPhases(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var log []string
		ready := make(chan struct{})
		a, _ := NewApp(time.Second, &gracefulComponent{name: "dependency", log: &log}, &gracefulComponent{name: "listener", log: &log, started: ready})
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- a.RunGraceful(ctx) }()
		<-ready
		// Startup has returned; synchronize on the first test component's completion
		// without making cancellation race with the Start cancellation contract.
		synctest.Wait()
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		want := []string{"start dependency", "start listener", "quiesce listener", "quiesce dependency", "drain listener", "drain dependency", "stop listener", "stop dependency"}
		if !reflect.DeepEqual(log, want) {
			t.Fatal(log)
		}
	})
}

func TestGracefulAppStillStopsAfterDrainDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var log []string
		ready := make(chan struct{})
		a, _ := NewApp(20*time.Millisecond, &gracefulComponent{name: "listener", log: &log, started: ready, waitForDeadline: true})
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- a.RunGraceful(ctx) }()
		<-ready
		synctest.Wait()
		cancel()
		if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
		if log[len(log)-1] != "stop listener" {
			t.Fatal(log)
		}
	})
}
