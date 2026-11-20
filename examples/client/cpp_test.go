//go:build cppinterop

package main

import (
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/NumberMan1/orbit/rpc"
	"github.com/NumberMan1/orbit/socket"
	"github.com/NumberMan1/orbit/socket/tcp"
)

func TestCPPEcho(t *testing.T) {
	executable := os.Getenv("ORBIT_CPP_ECHO_CLIENT")
	if executable == "" {
		t.Fatal("ORBIT_CPP_ECHO_CLIENT must point to the built C++ echo client")
	}
	t.Run("echo", func(t *testing.T) { testCPPEcho(t, executable) })
	t.Run("incomplete payload times out", func(t *testing.T) { testCPPReadTimeout(t, executable) })
}

func testCPPEcho(t *testing.T, executable string) {
	server, err := tcp.NewServer("127.0.0.1:0", socket.Options{}, func(_ context.Context, s *socket.Session, f rpc.Frame) error {
		return s.Send(f)
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	output, err := runCPPEcho(t, executable, server.Addr())
	if err != nil {
		t.Fatalf("C++ echo client: %v\n%s", err, output)
	}
	if got := strings.TrimSpace(string(output)); got != "id=1 sequence=42 payload=hello orbit" {
		t.Fatalf("unexpected C++ echo output: %q", got)
	}
}

func testCPPReadTimeout(t *testing.T, executable string) {
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	deadline := time.Now().Add(10 * time.Second)
	if err := listener.SetDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	peer := make(chan error, 1)
	go func() { peer <- sendIncompleteReply(listener, deadline) }()
	output, err := runCPPEcho(t, executable, listener.Addr())
	if err == nil || !strings.Contains(string(output), "TCP read timed out") {
		t.Errorf("expected a C++ read timeout, got %v\n%s", err, output)
	}
	if err := <-peer; err != nil {
		t.Fatal(err)
	}
}

func runCPPEcho(t *testing.T, executable string, address net.Addr) ([]byte, error) {
	t.Helper()
	host, port, err := net.SplitHostPort(address.String())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, executable, host, port).CombinedOutput()
}

func sendIncompleteReply(listener *net.TCPListener, deadline time.Time) error {
	conn, err := listener.AcceptTCP()
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}
	frame, err := rpc.Read(conn, rpc.MaxPayload)
	if err != nil {
		return err
	}
	wire, err := rpc.Marshal(frame, rpc.MaxPayload)
	if err != nil {
		return err
	}
	if _, err := conn.Write(wire[:rpc.HeaderSize]); err != nil {
		return err
	}
	// Keep the stream open with an incomplete payload until the client times out.
	_, err = io.Copy(io.Discard, conn)
	return err
}
