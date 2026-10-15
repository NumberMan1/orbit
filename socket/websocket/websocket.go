// Package websocket carries exactly one RO frame per binary WebSocket message.
package websocket

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/NumberMan1/orbit/rpc"
	"github.com/NumberMan1/orbit/socket"
	httpserver "github.com/NumberMan1/orbit/socket/http"
	ws "github.com/gorilla/websocket"
)

type Transport struct{ *ws.Conn }

func (t Transport) ReadFrame(limit int) (rpc.Frame, error) {
	t.SetReadLimit(int64(rpc.HeaderSize + limit))
	kind, b, err := t.ReadMessage()
	if err != nil {
		return rpc.Frame{}, err
	}
	if kind != ws.BinaryMessage {
		return rpc.Frame{}, errors.New("websocket: binary message required")
	}
	r := bytes.NewReader(b)
	f, err := rpc.Read(r, limit)
	if err != nil {
		return rpc.Frame{}, err
	}
	if r.Len() != 0 {
		return rpc.Frame{}, errors.New("websocket: trailing frame bytes")
	}
	return f, nil
}
func (t Transport) WriteFrame(f rpc.Frame, limit int) error {
	b, err := rpc.Marshal(f, limit)
	if err != nil {
		return err
	}
	return t.WriteMessage(ws.BinaryMessage, b)
}

type Server struct {
	http     *httpserver.Server
	upgrader ws.Upgrader
	options  socket.Options
	handler  socket.Handler
	group    socket.Group
	mu       sync.Mutex
	closed   bool
	wg       sync.WaitGroup
	stopOnce sync.Once
	done     chan struct{}
}

// A nil checkOrigin uses Gorilla's same-origin check; it does not allow all origins.
func NewServer(address string, options socket.Options, handler socket.Handler, tlsConfig *tls.Config, checkOrigin func(*http.Request) bool) (*Server, error) {
	o, err := options.Normalize()
	if err != nil {
		return nil, err
	}
	if handler == nil {
		return nil, errors.New("websocket: handler required")
	}
	s := &Server{options: o, handler: handler, upgrader: ws.Upgrader{HandshakeTimeout: 5 * time.Second, CheckOrigin: checkOrigin}, done: make(chan struct{})}
	s.http, err = httpserver.NewServer(address, s, tlsConfig)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		http.Error(w, "server stopping", http.StatusServiceUnavailable)
		return
	}
	s.wg.Add(1)
	s.mu.Unlock()
	defer s.wg.Done()
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	sess, err := socket.NewSession(r.Context(), Transport{conn}, s.options)
	if err != nil {
		_ = conn.Close()
		return
	}
	if !s.group.Add(sess) {
		return
	}
	defer s.group.Remove(sess)
	_ = sess.Run(s.handler)
}
func (s *Server) Start(ctx context.Context) error { return s.http.Start(ctx) }
func (s *Server) Addr() net.Addr                  { return s.http.Addr() }
func (s *Server) Done() <-chan struct{}           { return s.http.Done() }
func (s *Server) Err() error                      { return s.http.Err() }
func (s *Server) Stop(ctx context.Context) error {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		s.group.Close()
		go func() { s.wg.Wait(); close(s.done) }()
	})
	err := s.http.Stop(ctx)
	select {
	case <-s.done:
		return err
	case <-ctx.Done():
		return errors.Join(err, ctx.Err())
	}
}

func Dial(ctx context.Context, url string, options socket.Options, tlsConfig *tls.Config) (*socket.Session, error) {
	if _, err := options.Normalize(); err != nil {
		return nil, err
	}
	if tlsConfig != nil {
		tlsConfig = tlsConfig.Clone()
	}
	dialer := ws.Dialer{HandshakeTimeout: 10 * time.Second, TLSClientConfig: tlsConfig, Proxy: http.ProxyFromEnvironment}
	conn, res, err := dialer.DialContext(ctx, url, nil)
	if err != nil {
		if res != nil {
			_ = res.Body.Close()
		}
		return nil, err
	}
	s, err := socket.NewSession(ctx, Transport{conn}, options)
	if err != nil {
		_ = conn.Close()
	}
	return s, err
}
