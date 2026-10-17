package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/NumberMan1/orbit/discovery"
	"github.com/NumberMan1/orbit/request"
	"github.com/NumberMan1/orbit/rpc"
	"github.com/NumberMan1/orbit/socket"
	"github.com/NumberMan1/orbit/socket/tcp"
)

type backendPeer struct {
	connector *socket.Connector
	mu        sync.Mutex
	session   *socket.Session
	client    *request.Client
}

func newBackendPeer(address string, metrics *socket.Metrics) (*backendPeer, error) {
	p := &backendPeer{}
	options := socket.Options{Metrics: metrics, OnOpen: func(ctx context.Context, s *socket.Session) error {
		client, err := request.New(s, request.Options{ResponseIDs: []uint16{2}, MaxPending: 64})
		if err != nil {
			return err
		}
		p.mu.Lock()
		p.session, p.client = s, client
		p.mu.Unlock()
		return nil
	}, OnClose: func(ctx context.Context, s *socket.Session, err error) {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.session == s {
			p.client.Close()
			p.session, p.client = nil, nil
		}
	}}
	connector, err := tcp.NewConnector(address, options, nil, func(ctx context.Context, s *socket.Session, f rpc.Frame) error {
		client, ok := p.clientFor(s)
		if !ok {
			return socket.ErrDisconnected
		}
		return client.Handler(nil)(ctx, s, f)
	}, socket.ConnectorOptions{})
	if err != nil {
		return nil, err
	}
	p.connector = connector
	return p, nil
}
func (p *backendPeer) Start(ctx context.Context) error { return p.connector.Start(ctx) }
func (p *backendPeer) Stop(ctx context.Context) error  { return p.connector.Stop(ctx) }
func (p *backendPeer) Session() (*socket.Session, bool) {
	s, ok := p.connector.Session()
	if !ok {
		return nil, false
	}
	_, ok = p.clientFor(s)
	return s, ok
}
func (p *backendPeer) clientFor(s *socket.Session) (*request.Client, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.client, p.client != nil && p.session == s && s.Context().Err() == nil
}

// The static backend is confined to this example; it never changes revision.
type staticDirectory struct{ servers []*tcp.Server }

func (d staticDirectory) List(ctx context.Context) (discovery.ListResult, error) {
	if err := ctx.Err(); err != nil {
		return discovery.ListResult{}, err
	}
	result := discovery.ListResult{Revision: 1}
	for i, s := range d.servers {
		result.Services = append(result.Services, discovery.Descriptor{ID: fmt.Sprintf("backend-%d", i), Name: "echo", Address: s.Addr().String()})
	}
	return result, nil
}
func (d staticDirectory) Watch(ctx context.Context, _ int64) (discovery.Watcher, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &idleWatch{done: make(chan struct{}), parent: ctx}, nil
}

type idleWatch struct {
	done   chan struct{}
	once   sync.Once
	parent context.Context
}

func (w *idleWatch) Next(ctx context.Context) (discovery.Batch, error) {
	select {
	case <-ctx.Done():
		return discovery.Batch{}, ctx.Err()
	case <-w.parent.Done():
		return discovery.Batch{}, w.parent.Err()
	case <-w.done:
		return discovery.Batch{}, discovery.ErrWatchClosed
	}
}
func (w *idleWatch) Close() { w.once.Do(func() { close(w.done) }) }
