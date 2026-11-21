//go:build cppinterop

package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/NumberMan1/orbit/request"
	"github.com/NumberMan1/orbit/rpc"
	"github.com/NumberMan1/orbit/socket"
	"github.com/NumberMan1/orbit/socket/tcp"
)

func TestCPPRequestServer(t *testing.T) {
	executable := os.Getenv("ORBIT_CPP_REQUEST_SERVER")
	if executable == "" {
		t.Fatal("ORBIT_CPP_REQUEST_SERVER must point to the built C++ request server fixture")
	}
	t.Run("normal and out of order", func(t *testing.T) {
		fixture := startCPPRequestServer(t, executable)
		_, c := dialCPPRequestServer(t, fixture.address)
		normal := []byte{0, 1, 0xff, 0}
		reply := callCPP(t, c, 1, 2, normal)
		if reply.Sequence != 1 {
			t.Fatalf("first reply sequence = %d, want 1", reply.Sequence)
		}

		// Both calls expect the same response ID, so only the sequence
		// distinguishes the fast reply from the delayed one.
		slow := startCPPCall(t, c, 3, 2, []byte("slow"))
		fixture.waitEvent(t, "waiting")
		fast := callCPP(t, c, 1, 2, []byte("fast"))
		select {
		case result := <-slow:
			t.Fatalf("slow call returned before release: %+v", result)
		default:
		}
		release := callCPP(t, c, 5, 6, nil)
		delayed := checkCPPCall(t, <-slow, 2, []byte("slow"))
		if delayed.Sequence != 2 || fast.Sequence != 3 || release.Sequence != 4 {
			t.Fatalf("sequences slow=%d fast=%d release=%d", delayed.Sequence, fast.Sequence, release.Sequence)
		}
	})

	t.Run("disconnect cancels work and frees connection", func(t *testing.T) {
		fixture := startCPPRequestServer(t, executable)
		s, c := dialCPPRequestServer(t, fixture.address)
		pending := startCPPCall(t, c, 7, 8, nil)
		fixture.waitEvent(t, "waiting")
		s.Close()
		checkCPPDisconnect(t, <-pending)
		fixture.waitEvent(t, "canceled")
		if got := c.Status().Pending; got != 0 {
			t.Fatalf("Go pending calls after disconnect = %d", got)
		}
		_, next := dialCPPRequestServer(t, fixture.address)
		callCPP(t, next, 1, 2, []byte("new connection"))
	})

	t.Run("per-connection limit closes overloaded connection", func(t *testing.T) {
		fixture := startCPPRequestServer(t, executable)
		_, c := dialCPPRequestServer(t, fixture.address)
		first := startCPPCall(t, c, 7, 8, nil)
		fixture.waitEvent(t, "waiting")
		second := startCPPCall(t, c, 7, 8, nil)
		fixture.waitEvent(t, "waiting")
		reply, err := c.Call(t.Context(), rpc.Frame{ID: 1}, 2)
		checkCPPDisconnect(t, cppCallResult{reply, err})
		checkCPPDisconnect(t, <-first)
		checkCPPDisconnect(t, <-second)
		fixture.waitEvent(t, "canceled")
		fixture.waitEvent(t, "canceled")
	})

	t.Run("global overload lets another client wait for capacity", func(t *testing.T) {
		fixture := startCPPRequestServer(t, executable, "fairness")
		s, first := dialCPPRequestServer(t, fixture.address)
		busy := startCPPCall(t, first, 7, 8, nil)
		fixture.waitEvent(t, "waiting")
		_, second := dialCPPRequestServer(t, fixture.address)
		waiting := startCPPCall(t, second, 1, 2, []byte("other connection"))
		select {
		case result := <-waiting:
			t.Fatalf("second connection failed to wait: %+v", result)
		case <-time.After(200 * time.Millisecond):
		}
		s.Close()
		checkCPPDisconnect(t, <-busy)
		checkCPPCall(t, <-waiting, 2, []byte("other connection"))
		fixture.waitEvent(t, "canceled")
	})

	t.Run("bounded replies to a stalled reader", func(t *testing.T) {
		for _, mode := range []string{"pressure", "byte-pressure"} {
			t.Run(mode, func(t *testing.T) { testCPPReplyQueue(t, executable, mode) })
		}
	})

	t.Run("split and joined frames", func(t *testing.T) {
		fixture := startCPPRequestServer(t, executable)
		conn := dialCPPRaw(t, fixture.address)
		requests := []rpc.Frame{
			{ID: 1, Sequence: 101, Payload: []byte{1, 0xff}},
			{ID: 1, Sequence: 102, Payload: []byte{2, 0}},
		}
		first, err := rpc.Marshal(requests[0], rpc.MaxPayload)
		if err != nil {
			t.Fatal(err)
		}
		second, err := rpc.Marshal(requests[1], rpc.MaxPayload)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Write(first[:3]); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Write(append(first[3:], second...)); err != nil {
			t.Fatal(err)
		}
		want := map[uint32][]byte{requests[0].Sequence: requests[0].Payload, requests[1].Sequence: requests[1].Payload}
		for range requests {
			got, err := rpc.Read(conn, rpc.MaxPayload)
			payload, ok := want[got.Sequence]
			if err != nil || got.ID != 2 || !ok || !bytes.Equal(got.Payload, payload) {
				t.Fatalf("reply %+v, error %v", got, err)
			}
			delete(want, got.Sequence)
		}
		first[9] = 1
		if _, err := conn.Write(first); err != nil {
			t.Fatal(err)
		}
		if _, err := rpc.Read(conn, rpc.MaxPayload); err == nil || errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("server did not close after receiving invalid flags: %v", err)
		}
	})
}

func testCPPReplyQueue(t *testing.T, executable, mode string) {
	fixture := startCPPRequestServer(t, executable, mode)
	conn := dialCPPRaw(t, fixture.address)
	if err := conn.SetReadBuffer(1024); err != nil {
		t.Fatal(err)
	}
	if err := rpc.Write(conn, rpc.Frame{ID: 7, Sequence: 1}, rpc.MaxPayload); err != nil {
		t.Fatal(err)
	}
	fixture.waitEvent(t, "waiting")
	// Leave replies unread. Each request produces a maximum-size frame, and
	// we wait for its handler before sending another to avoid work overload.
	for seq := uint32(2); seq <= 256; seq++ {
		if err := rpc.Write(conn, rpc.Frame{ID: 9, Sequence: seq}, rpc.MaxPayload); err != nil {
			fixture.waitEvent(t, "canceled")
			return
		}
		switch event := fixture.nextEvent(t); event {
		case "canceled":
			return
		case "reply ready":
		default:
			t.Fatalf("unexpected fixture event %q", event)
		}
	}
	t.Fatal("server did not disconnect when its send queue filled")
}

type cppRequestFixture struct {
	address string
	events  <-chan string
}

func startCPPRequestServer(t *testing.T, executable string, args ...string) cppRequestFixture {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, executable, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	events := make(chan string, 16)
	done := make(chan error, 1)
	go func() {
		scan := bufio.NewScanner(stdout)
		for scan.Scan() {
			select {
			case events <- scan.Text():
			case <-ctx.Done():
			}
		}
		close(events)
		done <- errors.Join(scan.Err(), cmd.Wait())
	}()
	t.Cleanup(func() {
		_ = stdin.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("C++ request server: %v\n%s", err, stderr.String())
			}
		case <-time.After(5 * time.Second):
			cancel()
			<-done
			t.Errorf("C++ request server did not stop\n%s", stderr.String())
		}
	})
	fixture := cppRequestFixture{events: events}
	port := fixture.nextEvent(t)
	if parsed, err := strconv.ParseUint(port, 10, 16); err != nil || parsed == 0 {
		t.Fatalf("invalid C++ server port %q", port)
	}
	fixture.address = net.JoinHostPort("127.0.0.1", port)
	return fixture
}

func (f cppRequestFixture) nextEvent(t *testing.T) string {
	t.Helper()
	select {
	case event, open := <-f.events:
		if !open {
			t.Fatal("C++ server output closed")
		}
		return event
	case <-time.After(8 * time.Second):
		t.Fatal("timed out waiting for C++ server")
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	return ""
}

func (f cppRequestFixture) waitEvent(t *testing.T, want string) {
	t.Helper()
	if got := f.nextEvent(t); got != want {
		t.Fatalf("fixture event = %q, want %q", got, want)
	}
}

func dialCPPRequestServer(t *testing.T, address string) (*socket.Session, *request.Client) {
	t.Helper()
	s, err := tcp.Dial(t.Context(), address, socket.Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	c, err := request.New(s, request.Options{ResponseIDs: []uint16{2, 6, 8}})
	if err != nil {
		s.Close()
		t.Fatal(err)
	}
	go func() { _ = s.Run(c.Handler(nil)) }()
	t.Cleanup(func() {
		s.Close()
		c.Close()
		select {
		case <-s.Done():
		case <-time.After(5 * time.Second):
			t.Error("Go session did not stop")
		}
	})
	return s, c
}

func dialCPPRaw(t *testing.T, address string) *net.TCPConn {
	t.Helper()
	conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(t.Context(), "tcp4", address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	return conn.(*net.TCPConn)
}

type cppCallResult struct {
	reply rpc.Frame
	err   error
}

func startCPPCall(t *testing.T, c *request.Client, id, responseID uint16, payload []byte) <-chan cppCallResult {
	t.Helper()
	done := make(chan cppCallResult, 1)
	go func() {
		reply, err := c.Call(t.Context(), rpc.Frame{ID: id, Payload: payload}, responseID)
		done <- cppCallResult{reply, err}
	}()
	return done
}

func callCPP(t *testing.T, c *request.Client, id, responseID uint16, payload []byte) rpc.Frame {
	t.Helper()
	reply, err := c.Call(t.Context(), rpc.Frame{ID: id, Payload: payload}, responseID)
	return checkCPPCall(t, cppCallResult{reply, err}, responseID, payload)
}

func checkCPPCall(t *testing.T, result cppCallResult, id uint16, payload []byte) rpc.Frame {
	t.Helper()
	if result.err != nil || result.reply.ID != id || !bytes.Equal(result.reply.Payload, payload) {
		t.Fatalf("C++ reply %+v, error %v, want ID %d payload %q", result.reply, result.err, id, payload)
	}
	return result.reply
}

func checkCPPDisconnect(t *testing.T, result cppCallResult) {
	t.Helper()
	if !errors.Is(result.err, request.ErrClosed) {
		t.Fatalf("reply %+v, error %v, want request.ErrClosed", result.reply, result.err)
	}
}
