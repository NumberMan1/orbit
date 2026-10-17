// Package tcp implements TCP/TLS listeners and cancellable reconnecting clients.
package tcp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/NumberMan1/orbit/rpc"
	"github.com/NumberMan1/orbit/socket"
)

type Transport struct{ net.Conn }

func (t Transport) ReadFrame(limit int) (rpc.Frame, error)  { return rpc.Read(t.Conn, limit) }
func (t Transport) WriteFrame(f rpc.Frame, limit int) error { return rpc.Write(t.Conn, f, limit) }

type Server struct {
	mu        sync.Mutex
	address   string
	options   socket.Options
	handler   socket.Handler
	tls       *tls.Config
	listener  net.Listener
	cancel    context.CancelFunc
	done      chan struct{}
	used      bool
	quiescing bool
	group     socket.Group
	err       error
}

func NewServer(address string, options socket.Options, handler socket.Handler, tlsConfig *tls.Config) (*Server, error) {
	o, err := options.Normalize()
	if err != nil {
		return nil, err
	}
	if address == "" || handler == nil {
		return nil, errors.New("tcp: address and handler required")
	}
	if tlsConfig != nil {
		tlsConfig = tlsConfig.Clone()
	}
	return &Server{address: address, options: o, handler: handler, tls: tlsConfig, done: make(chan struct{})}, nil
}

func (s *Server) Start(parent context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.used || s.quiescing {
		return errors.New("tcp: server already started or stopped")
	}
	if err := parent.Err(); err != nil {
		return err
	}
	ln, err := (&net.ListenConfig{}).Listen(parent, "tcp", s.address)
	if err != nil {
		return err
	}
	if s.tls != nil {
		ln = tls.NewListener(ln, s.tls)
	}
	ctx, cancel := context.WithCancel(parent)
	s.used, s.listener, s.cancel = true, ln, cancel
	go s.serve(ctx, ln)
	return nil
}

func (s *Server) serve(ctx context.Context, ln net.Listener) {
	var wg sync.WaitGroup
	stop := context.AfterFunc(ctx, func() { _ = ln.Close(); s.group.Close() })
	defer func() {
		_ = ln.Close()
		s.mu.Lock()
		draining := s.quiescing
		s.mu.Unlock()
		if !draining {
			s.group.Close()
		}
		wg.Wait()
		stop()
		close(s.done)
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			s.mu.Lock()
			if ctx.Err() == nil && !s.quiescing {
				s.err = err
			}
			s.mu.Unlock()
			return
		}
		release, err := s.options.Admission.Acquire(conn.RemoteAddr().String())
		if err != nil {
			_ = conn.Close()
			continue
		}
		if err := socket.ConfigureConn(conn, s.options.Network); err != nil {
			release()
			_ = conn.Close()
			continue
		}
		sess, err := socket.NewSession(ctx, Transport{conn}, s.options)
		if err != nil {
			release()
			_ = conn.Close()
			continue
		}
		if !s.group.Add(sess) {
			release()
			continue
		}
		wg.Go(func() { defer release(); defer s.group.Remove(sess); _ = sess.Run(s.handler) })
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
func (s *Server) Done() <-chan struct{}                     { return s.done }
func (s *Server) Session(id uint64) (*socket.Session, bool) { return s.group.Get(id) }
func (s *Server) SessionCount() int                         { return s.group.Len() }

// Quiesce stops accepting connections without canceling existing sessions.
func (s *Server) Quiesce() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.quiescing = true
	s.group.Seal()
	if s.listener != nil {
		_ = s.listener.Close()
	}
}

// Drain quiesces, finishes accepted work and writes, then stops the listener.
// On deadline it force-closes sessions; it does not guarantee peer delivery.
func (s *Server) Drain(ctx context.Context) error {
	s.Quiesce()
	err := s.group.Drain(ctx)
	return errors.Join(err, s.Stop(ctx))
}

func (s *Server) Err() error { s.mu.Lock(); defer s.mu.Unlock(); return s.err }
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.used {
		s.used = true
		close(s.done)
	}
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
	select {
	case <-s.done:
		return s.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Dial creates a session; the caller must Run it or Close it.
func Dial(ctx context.Context, address string, options socket.Options, tlsConfig *tls.Config) (*socket.Session, error) {
	if _, err := options.Normalize(); err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	var conn net.Conn
	var err error
	if tlsConfig == nil {
		conn, err = dialer.DialContext(ctx, "tcp", address)
	} else {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: tlsConfig.Clone()}).DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return nil, err
	}
	if err := socket.ConfigureConn(conn, options.Network); err != nil {
		_ = conn.Close()
		return nil, err
	}
	s, err := socket.NewSession(ctx, Transport{conn}, options)
	if err != nil {
		_ = conn.Close()
	}
	return s, err
}

// NewConnector adds managed lifetime and readiness to TCP/TLS reconnection.
func NewConnector(address string, options socket.Options, tlsConfig *tls.Config, handler socket.Handler, retry socket.ConnectorOptions) (*socket.Connector, error) {
	o, err := options.Normalize()
	if err != nil {
		return nil, err
	}
	if address == "" {
		return nil, errors.New("tcp: address required")
	}
	if tlsConfig != nil {
		tlsConfig = tlsConfig.Clone()
	}
	return socket.NewConnector(func(ctx context.Context) (*socket.Session, error) { return Dial(ctx, address, o, tlsConfig) }, handler, retry)
}

// Reconnect invokes serve with a NEW session per connection. serve normally calls
// Session.Run and must return on ctx cancellation. Failed sends are never replayed.
func Reconnect(ctx context.Context, address string, options socket.Options, tlsConfig *tls.Config, minDelay, maxDelay time.Duration, serve func(*socket.Session) error) error {
	if minDelay <= 0 || maxDelay < minDelay || serve == nil {
		return errors.New("tcp: invalid reconnect options")
	}
	if _, err := options.Normalize(); err != nil {
		return err
	}
	delay := minDelay
	for ctx.Err() == nil {
		s, err := Dial(ctx, address, options, tlsConfig)
		if err == nil {
			func() { defer s.Close(); err = serve(s) }()
			delay = minDelay
		}
		if ctx.Err() != nil {
			break
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if delay < maxDelay {
			if delay > maxDelay/2 {
				delay = maxDelay
			} else {
				delay *= 2
			}
		}
	}
	return fmt.Errorf("tcp: reconnect stopped: %w", ctx.Err())
}
