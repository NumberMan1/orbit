package request

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/NumberMan1/orbit/observe"
	"github.com/NumberMan1/orbit/rpc"
)

func TestMetadataStrictVersionAndFields(t *testing.T) {
	valid := `{"version":1,"deadline_unix_milli":2000000000000,"trace_id":"0123456789abcdef0123456789abcdef","payload":"eA=="}`
	for _, bad := range []string{
		strings.Replace(valid, `"version":1`, `"version":2`, 1),
		strings.Replace(valid, `"version":1`, `"version":1,"version":1`, 1),
		strings.Replace(valid, `"payload":"eA=="`, `"payload":"eA==","unknown":true`, 1),
		strings.Replace(valid, `0123456789abcdef0123456789abcdef`, `invalid`, 1),
		strings.Replace(valid, `2000000000000`, `0`, 1),
	} {
		_, cancel, _, err := unpack(t.Context(), rpc.Frame{ID: 1, Payload: []byte(bad)})
		if cancel != nil {
			cancel()
		}
		if err == nil {
			t.Fatal("invalid metadata accepted", bad)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	remote, stop, f, err := unpack(ctx, rpc.Frame{ID: 1, Sequence: 7, Payload: []byte(valid)})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	deadline, _ := ctx.Deadline()
	received, _ := remote.Deadline()
	if !deadline.Equal(received) || f.ID != 1 || f.Sequence != 7 || string(f.Payload) != "x" || observe.TraceID(remote) == "" {
		t.Fatal("metadata weakened server budget or corrupted frame")
	}
}

func FuzzMetadata(f *testing.F) {
	f.Add([]byte(`{"version":1,"deadline_unix_milli":2000000000000,"trace_id":"0123456789abcdef0123456789abcdef","payload":"eA=="}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"version":1,"version":1}`))
	f.Fuzz(func(t *testing.T, payload []byte) {
		if len(payload) > rpc.MaxPayload {
			return
		}
		_, cancel, frame, err := unpack(t.Context(), rpc.Frame{ID: 1, Sequence: 7, Payload: payload})
		if cancel != nil {
			defer cancel()
		}
		if err == nil && (frame.ID != 1 || frame.Sequence != 7 || len(frame.Payload) > rpc.MaxPayload) {
			t.Fatal("invalid decoded frame")
		}
	})
}
