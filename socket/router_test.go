package socket

import (
	"context"
	"reflect"
	"testing"

	"github.com/NumberMan1/orbit/rpc"
)

func TestRouterMiddlewareOrderAndErrors(t *testing.T) {
	var r Router
	var calls []string
	middleware := func(name string) Middleware {
		return func(next Handler) Handler {
			return func(ctx context.Context, s *Session, f rpc.Frame) error {
				calls = append(calls, name)
				return next(ctx, s, f)
			}
		}
	}
	h := Chain(func(context.Context, *Session, rpc.Frame) error { calls = append(calls, "handler"); return nil }, middleware("first"), middleware("second"))
	if err := r.Register(1, h); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(1, h); err == nil {
		t.Fatal("duplicate accepted")
	}
	if err := r.Handle(t.Context(), nil, rpc.Frame{ID: 1}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"first", "second", "handler"}) {
		t.Fatal(calls)
	}
	if err := r.Handle(t.Context(), nil, rpc.Frame{ID: 2}); err == nil {
		t.Fatal("unknown ID accepted")
	}
}
