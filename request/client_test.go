package request

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

func testClient(t *testing.T, max int) (*Client, net.Conn) {
	t.Helper()
	a, b := net.Pipe()
	s, err := socket.NewSession(t.Context(), tcp.Transport{Conn: a}, socket.Options{})
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(s, Options{MaxPending: max, ResponseIDs: []uint16{2, 4}, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); _ = s.Run(c.Handler(nil)) }()
	t.Cleanup(func() { c.Close(); s.Close(); b.Close(); <-done })
	_ = b.SetDeadline(time.Now().Add(3 * time.Second))
	return c, b
}

func TestCallsMatchOutOfOrderAndPreservePayload(t *testing.T) {
	c, peer := testClient(t, 4)
	results := make(chan result, 2)
	for _, value := range []string{"one", "two"} {
		go func() {
			f, err := c.Call(t.Context(), rpc.Frame{ID: 1, Payload: []byte(value)}, 2)
			results <- result{f, err}
		}()
	}
	a, err := rpc.Read(peer, rpc.MaxPayload)
	if err != nil {
		t.Fatal(err)
	}
	b, err := rpc.Read(peer, rpc.MaxPayload)
	if err != nil {
		t.Fatal(err)
	}
	if a.Sequence == b.Sequence || a.Sequence == 0 || b.Sequence == 0 {
		t.Fatal(a, b)
	}
	for _, f := range []rpc.Frame{b, a} {
		f.ID = 2
		if err := rpc.Write(peer, f, rpc.MaxPayload); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	for range 2 {
		r := <-results
		if r.err != nil {
			t.Fatal(r.err)
		}
		seen[string(r.frame.Payload)] = true
	}
	if !seen["one"] || !seen["two"] || c.Status().Pending != 0 {
		t.Fatal(seen, c.Status())
	}
}

func TestTimeoutCapacityLateResponseAndDisconnect(t *testing.T) {
	c, peer := testClient(t, 1)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := c.Call(ctx, rpc.Frame{ID: 1}, 2); done <- err }()
	first, err := rpc.Read(peer, rpc.MaxPayload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Call(t.Context(), rpc.Frame{ID: 1}, 2); !errors.Is(err, ErrFull) {
		t.Fatal(err)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	first.ID = 2
	if err := rpc.Write(peer, first, rpc.MaxPayload); err != nil {
		t.Fatal(err)
	}
	go func() { _, err := c.Call(t.Context(), rpc.Frame{ID: 1}, 2); done <- err }()
	second, err := rpc.Read(peer, rpc.MaxPayload)
	if err != nil || second.Sequence == first.Sequence {
		t.Fatal(second, err)
	}
	peer.Close()
	if err := <-done; !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if c.Status().Pending != 0 {
		t.Fatal(c.Status())
	}
}

func TestResponseTypeMismatchAndSequenceExhaustion(t *testing.T) {
	c, peer := testClient(t, 1)
	done := make(chan error, 1)
	go func() { _, err := c.Call(t.Context(), rpc.Frame{ID: 1}, 2); done <- err }()
	f, err := rpc.Read(peer, rpc.MaxPayload)
	if err != nil {
		t.Fatal(err)
	}
	f.ID = 4
	if err := rpc.Write(peer, f, rpc.MaxPayload); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, ErrResponseID) {
		t.Fatal(err)
	}
	c.mu.Lock()
	c.next = 1<<32 - 1
	c.mu.Unlock()
	if _, err := c.Call(t.Context(), rpc.Frame{ID: 1}, 2); !errors.Is(err, ErrSequenceExhausted) {
		t.Fatal(err)
	}
}

func TestCallTimeoutAndCloseReleasePending(t *testing.T) {
	c, peer := testClient(t, 1)
	c.options.Timeout = 20 * time.Millisecond
	done := make(chan error, 1)
	go func() { _, err := c.Call(t.Context(), rpc.Frame{ID: 1}, 2); done <- err }()
	if _, err := rpc.Read(peer, rpc.MaxPayload); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if status := c.Status(); status.Pending != 0 || status.Expired != 1 {
		t.Fatal(status)
	}
	c.options.Timeout = time.Second
	go func() { _, err := c.Call(t.Context(), rpc.Frame{ID: 1}, 2); done <- err }()
	if _, err := rpc.Read(peer, rpc.MaxPayload); err != nil {
		t.Fatal(err)
	}
	c.Close()
	if err := <-done; !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if c.session.Context().Err() != nil {
		t.Fatal("client closed borrowed session")
	}
}
