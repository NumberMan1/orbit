package codec

import (
	"sync"
	"testing"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

type first struct{ Value string }
type second struct{ Value int }

func TestRegistryAtomicAndRoundTrip(t *testing.T) {
	var r Registry
	if err := r.Register(1, (*first)(nil), JSON{}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(2, (*first)(nil), JSON{}); err == nil {
		t.Fatal("duplicate type accepted")
	}
	if err := r.Register(2, (*second)(nil), JSON{}); err != nil {
		t.Fatalf("failed registration corrupted ID index: %v", err)
	}
	if _, err := r.Encode((*first)(nil), 0); err == nil {
		t.Fatal("nil accepted")
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			f, err := r.Encode(&first{Value: "hello"}, 42)
			if err != nil {
				t.Error(err)
				return
			}
			v, err := r.Decode(f)
			if err != nil {
				t.Error(err)
				return
			}
			if f.Sequence != 42 || v.(*first).Value != "hello" {
				t.Error("incorrect roundtrip")
			}
		})
	}
	wg.Wait()
}
func TestProtobuf(t *testing.T) {
	var r Registry
	if err := r.Register(3, &wrapperspb.StringValue{}, Protobuf{}); err != nil {
		t.Fatal(err)
	}
	f, err := r.Encode(wrapperspb.String("test"), 4)
	if err != nil {
		t.Fatal(err)
	}
	m, err := r.Decode(f)
	if err != nil || m.(*wrapperspb.StringValue).Value != "test" {
		t.Fatalf("%v %v", m, err)
	}
	if _, err := (Protobuf{}).Marshal("bad"); err == nil {
		t.Fatal("invalid type accepted")
	}
}
