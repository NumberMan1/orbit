package gateway

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/NumberMan1/orbit/rpc"
	"github.com/NumberMan1/orbit/socket"
	"github.com/NumberMan1/orbit/socket/tcp"
)

func TestMappedFailureKeepsConnectionAndOriginalSequence(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	_ = b.SetDeadline(time.Now().Add(3 * time.Second))
	s, _ := socket.NewSession(t.Context(), tcp.Transport{Conn: a}, socket.Options{})
	h, err := ForwardWithOptions(ForwardOptions{MaxConcurrent: 1, OnError: func(_ context.Context, s *socket.Session, f rpc.Frame, _ error) error {
		return s.Send(rpc.Frame{ID: 2, Sequence: f.Sequence, Payload: []byte("unavailable")})
	}}, func(_ context.Context, _ *socket.Session, f rpc.Frame) (Caller, uint16, error) {
		if string(f.Payload) == "fail" {
			return nil, 0, errors.New("private internal detail")
		}
		return CallerFunc(func(_ context.Context, f rpc.Frame, _ uint16) (rpc.Frame, error) {
			return rpc.Frame{ID: 2, Sequence: 999, Payload: f.Payload}, nil
		}), 2, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); _ = s.Run(h) }()
	defer func() { s.Close(); <-done }()
	for i, payload := range []string{"fail", "success"} {
		seq := uint32(i + 40)
		if err := rpc.Write(b, rpc.Frame{ID: 1, Sequence: seq, Payload: []byte(payload)}, rpc.MaxPayload); err != nil {
			t.Fatal(err)
		}
		f, err := rpc.Read(b, rpc.MaxPayload)
		if err != nil || f.Sequence != seq {
			t.Fatal(f, err)
		}
		want := payload
		if i == 0 {
			want = "unavailable"
		}
		if string(f.Payload) != want {
			t.Fatal(f)
		}
	}
}
