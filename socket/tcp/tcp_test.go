package tcp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/NumberMan1/orbit/rpc"
	"github.com/NumberMan1/orbit/socket"
)

func echo(_ context.Context, s *socket.Session, f rpc.Frame) error { return s.Send(f) }

func TestTCPAndTLSRoundTripShutdown(t *testing.T) {
	for _, secure := range []bool{false, true} {
		t.Run(map[bool]string{false: "tcp", true: "tls"}[secure], func(t *testing.T) {
			var serverTLS, clientTLS *tls.Config
			if secure {
				fixture := httptest.NewTLSServer(nil)
				defer fixture.Close()
				roots := x509.NewCertPool()
				roots.AddCert(fixture.Certificate())
				serverTLS = &tls.Config{Certificates: fixture.TLS.Certificates, MinVersion: tls.VersionTLS13}
				clientTLS = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13}
			}
			s, err := NewServer("127.0.0.1:0", socket.Options{}, echo, serverTLS)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			defer s.Stop(context.Background())
			if err := s.Start(t.Context()); err == nil {
				t.Fatal("duplicate start accepted")
			}
			client, err := Dial(t.Context(), s.Addr().String(), socket.Options{}, clientTLS)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			received := make(chan rpc.Frame, 1)
			go client.Run(func(_ context.Context, _ *socket.Session, f rpc.Frame) error { received <- f; return nil })
			if err := client.Send(rpc.Frame{ID: 11, Sequence: 42, Payload: []byte("echo")}); err != nil {
				t.Fatal(err)
			}
			select {
			case f := <-received:
				if f.Sequence != 42 || string(f.Payload) != "echo" {
					t.Fatal(f)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("echo timeout")
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			var wg sync.WaitGroup
			for range 4 {
				wg.Go(func() {
					if err := s.Stop(ctx); err != nil {
						t.Error(err)
					}
				})
			}
			wg.Wait()
			select {
			case <-client.Done():
			case <-ctx.Done():
				t.Fatal("client did not exit")
			}
		})
	}
}

func TestReconnectCanceledDuringBackoff(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := ln.Addr().String()
	_ = ln.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	err = Reconnect(ctx, address, socket.Options{}, nil, time.Hour, time.Hour, func(*socket.Session) error { t.Error("unexpected connection"); return nil })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

func TestReconnectUsesFreshSession(t *testing.T) {
	s, _ := NewServer("127.0.0.1:0", socket.Options{}, echo, nil)
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer s.Stop(context.Background())
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	var first uint64
	count := 0
	err := Reconnect(ctx, s.Addr().String(), socket.Options{}, nil, time.Millisecond, time.Millisecond, func(session *socket.Session) error {
		count++
		if count == 1 {
			first = session.ID()
		} else {
			if first == session.ID() {
				t.Error("session reused")
			}
			cancel()
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) || count != 2 {
		t.Fatalf("count=%d err=%v", count, err)
	}
}
