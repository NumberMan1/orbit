package discovery

import (
	"testing"
)

func TestDescriptorAndSnapshotIsolation(t *testing.T) {
	for _, d := range []Descriptor{{}, {ID: "a/b", Name: "worker", Address: "127.0.0.1:1"}} {
		if d.Validate() == nil {
			t.Fatal("invalid descriptor accepted")
		}
	}
	state := map[string]Descriptor{"one": {ID: "one", Name: "worker", Address: "127.0.0.1:1", Metadata: map[string]string{"zone": "a"}}}
	copy := clone(state)
	copy["one"].Metadata["zone"] = "b"
	delete(copy, "one")
	if state["one"].Metadata["zone"] != "a" {
		t.Fatal("snapshot aliases internal state")
	}
	r := Registry{prefix: "/test/"}
	if _, err := r.decode("/test/wrong", []byte(`{"id":"one","name":"worker","address":"localhost:1"}`)); err == nil {
		t.Fatal("mismatched key accepted")
	}
}

func TestDescriptorJSONV2Validation(t *testing.T) {
	r := Registry{prefix: "/test/"}
	for _, data := range []string{
		`{"id":"one","id":"one","name":"worker","address":"localhost:1"}`,
		`{"ID":"one","name":"worker","address":"localhost:1"}`,
		`{"id":"one","name":"worker","address":"localhost:1","metadata":{"zone":"a","zone":"b"}}`,
		"{\"id\":\"one\",\"name\":\"\xff\",\"address\":\"localhost:1\"}",
	} {
		if _, err := r.decode("/test/one", []byte(data)); err == nil {
			t.Fatalf("accepted invalid descriptor: %q", data)
		}
	}
	// Unknown extension fields remain allowed for schema evolution.
	if d, err := r.decode("/test/one", []byte(`{"id":"one","name":"worker","address":"localhost:1","future":true}`)); err != nil || d.ID != "one" {
		t.Fatalf("valid descriptor rejected: %+v %v", d, err)
	}
}
