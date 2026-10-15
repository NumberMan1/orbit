// Package http provides HTTP lifecycle and bounded JSON request helpers.
package http

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	stdhttp "net/http"
	"sync"
	"time"
)

type Server struct {
	mu       sync.Mutex
	server   *stdhttp.Server
	listener net.Listener
	done     chan struct{}
	used     bool
	err      error
	tls      *tls.Config
}

// NewServer uses a private mux/handler; no handlers (including pprof) are global.
func NewServer(address string, handler stdhttp.Handler, tlsConfig *tls.Config) (*Server, error) {
	if address == "" || handler == nil {
		return nil, errors.New("http: address and handler required")
	}
	if tlsConfig != nil {
		tlsConfig = tlsConfig.Clone()
	}
	return &Server{server: &stdhttp.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}, done: make(chan struct{}), tls: tlsConfig}, nil
}

// The parent context governs initialization. Stop performs graceful draining;
// unlike socket sessions, requests are not canceled just because App.Run ends.
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.used {
		return errors.New("http: server already started or stopped")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", s.server.Addr)
	if err != nil {
		return err
	}
	if s.tls != nil {
		ln = tls.NewListener(ln, s.tls)
	}
	s.used, s.listener = true, ln
	go func() {
		err := s.server.Serve(ln)
		s.mu.Lock()
		if !errors.Is(err, stdhttp.ErrServerClosed) {
			s.err = err
		}
		s.mu.Unlock()
		close(s.done)
	}()
	return nil
}

func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.used {
		s.used = true
		close(s.done)
	}
	s.mu.Unlock()
	err := s.server.Shutdown(ctx)
	if err != nil {
		err = errors.Join(err, s.server.Close())
	}
	select {
	case <-s.done:
		return errors.Join(err, s.Err())
	case <-ctx.Done():
		return errors.Join(err, ctx.Err())
	}
}
func (s *Server) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}
func (s *Server) Done() <-chan struct{} { return s.done }
func (s *Server) Err() error            { s.mu.Lock(); defer s.mu.Unlock(); return s.err }
