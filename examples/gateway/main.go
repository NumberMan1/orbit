// Command gateway runs two backends, discovered connectors and a bounded gateway.
package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/NumberMan1/orbit"
	"github.com/NumberMan1/orbit/codec"
	"github.com/NumberMan1/orbit/discovery"
	"github.com/NumberMan1/orbit/examples/gateway/protocol"
	"github.com/NumberMan1/orbit/gateway"
	"github.com/NumberMan1/orbit/health"
	"github.com/NumberMan1/orbit/observe"
	"github.com/NumberMan1/orbit/request"
	"github.com/NumberMan1/orbit/rpc"
	"github.com/NumberMan1/orbit/service"
	"github.com/NumberMan1/orbit/socket"
	httpserver "github.com/NumberMan1/orbit/socket/http"
	"github.com/NumberMan1/orbit/socket/tcp"
)

func main() {
	selfTest := flag.Bool("self-test", false, "run end-to-end requests then drain and exit")
	listen := flag.String("listen", "127.0.0.1:9100", "gateway TCP address")
	admin := flag.String("admin", "127.0.0.1:9103", "private health/metrics HTTP address")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, *listen, *admin, *selfTest); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(parent context.Context, listen, admin string, selfTest bool) error {
	var registry codec.Registry
	if err := protocol.RegisterMessages(&registry); err != nil {
		return err
	}
	metrics := &socket.Metrics{}
	timings := &observe.Metrics{}
	admission, err := socket.NewAdmission(128, 64)
	if err != nil {
		return err
	}
	options := socket.Options{Admission: admission, Metrics: metrics, Observer: timings, ReadTimeout: time.Minute, InboundRate: socket.RateLimit{PerSecond: 100, Burst: 200}}
	onError := func(_ context.Context, s *socket.Session, f rpc.Frame, _ error) error {
		reply, err := registry.Encode(&protocol.EchoReply{Error: "unavailable"}, f.Sequence)
		if err != nil {
			return err
		}
		return s.Send(reply)
	}
	var backends []*tcp.Server
	for _, name := range []string{"backend-a", "backend-b"} {
		router := &socket.Router{}
		if err := protocol.RegisterHandlers(router, &registry, protocol.Handlers{Echo: func(ctx context.Context, e socket.MessageEvent) error {
			return e.Reply(&protocol.EchoReply{Text: e.Message.(*protocol.Echo).Text, Backend: name})
		}}); err != nil {
			return err
		}
		calls, err := request.NewServer(request.ServerOptions{RequestIDs: []uint16{1}, CancelID: 9, Metadata: true, MaxConcurrent: 64, OnError: onError, Observer: timings}, router.Handle)
		if err != nil {
			return err
		}
		handler, err := request.RespondHeartbeat(3, 4, calls.Handler(nil))
		if err != nil {
			return err
		}
		s, err := tcp.NewServer("127.0.0.1:0", options, handler, nil)
		if err != nil {
			return err
		}
		backends = append(backends, s)
	}
	// This demo directory is static; replace it with Registry.NewStore for etcd.
	store, err := discovery.NewStore(staticDirectory{backends}, discovery.StoreOptions{})
	if err != nil {
		return err
	}
	pool, err := discovery.NewPool(store, func(d discovery.Descriptor) (orbit.Component, error) {
		return newBackendPeer(d.Address, metrics, timings)
	}, discovery.PoolOptions{Interval: 10 * time.Millisecond})
	if err != nil {
		return err
	}
	balancer, err := discovery.NewBalancer(pool, discovery.BalancerOptions{FailureThreshold: 3, Cooldown: time.Second})
	if err != nil {
		return err
	}
	forward, err := gateway.ForwardWithOptions(gateway.ForwardOptions{MaxConcurrent: 64, OnError: onError}, func(ctx context.Context, front *socket.Session, f rpc.Frame) (gateway.Caller, uint16, error) {
		if f.ID != 1 {
			return nil, 0, errors.New("example: unsupported request")
		}
		selected, err := balancer.Acquire("echo", "")
		if err != nil {
			return nil, 0, err
		}
		component, ok := pool.Get(selected.Descriptor.ID)
		if !ok {
			selected.Complete(discovery.ErrNoPeer)
			return nil, 0, discovery.ErrNoPeer
		}
		peer, ok := component.(*backendPeer)
		if !ok {
			selected.Complete(discovery.ErrNoPeer)
			return nil, 0, discovery.ErrNoPeer
		}
		client, ok := peer.clientFor(selected.Session)
		if !ok {
			selected.Complete(discovery.ErrNoPeer)
			return nil, 0, discovery.ErrNoPeer
		}
		return gateway.CallerFunc(func(ctx context.Context, f rpc.Frame, id uint16) (reply rpc.Frame, err error) {
			defer func() { selected.Complete(err) }()
			return client.Call(ctx, f, id)
		}), 2, nil
	})
	if err != nil {
		return err
	}
	frontCalls, err := request.NewServer(request.ServerOptions{RequestIDs: []uint16{1}, CancelID: 9, Metadata: true, MaxConcurrent: 64, OnError: onError, Observer: timings}, forward)
	if err != nil {
		return err
	}
	front, err := tcp.NewServer(listen, options, frontCalls.Handler(nil), nil)
	if err != nil {
		return err
	}
	checks, err := health.New(time.Second)
	if err != nil {
		return err
	}
	if err := checks.Add("directory", func(context.Context) error {
		if !store.Status().Ready {
			return discovery.ErrNoPeer
		}
		return nil
	}); err != nil {
		return err
	}
	if err := checks.Add("backends", func(context.Context) error {
		for _, d := range store.All() {
			component, ok := pool.Get(d.ID)
			if !ok {
				return discovery.ErrNoPeer
			}
			peer, ok := component.(*backendPeer)
			if !ok {
				return discovery.ErrNoPeer
			}
			if _, ok := peer.Session(); !ok {
				return discovery.ErrNoPeer
			}
		}
		return nil
	}); err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /livez", checks.Liveness)
	mux.HandleFunc("GET /readyz", checks.Readiness)
	mux.Handle("GET /metrics/prometheus", timings)
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.MarshalWrite(w, struct {
			Socket    socket.MetricsSnapshot
			Admission socket.AdmissionStatus
		}{metrics.Snapshot(), admission.Status()})
	})
	ops, err := httpserver.NewServer(admin, mux, nil)
	if err != nil {
		return err
	}
	app, err := service.NewApp(5*time.Second, backends[0], backends[1], store, pool, checks, ops, front)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- app.RunGraceful(ctx) }()
	readyCtx, readyCancel := context.WithTimeout(ctx, 5*time.Second)
	defer readyCancel()
	for front.Addr() == nil || ops.Addr() == nil || !checks.Probe(readyCtx).Ready {
		select {
		case err := <-done:
			return err
		case <-readyCtx.Done():
			cancel()
			return errors.Join(readyCtx.Err(), <-done)
		case <-time.After(10 * time.Millisecond):
		}
	}
	fmt.Printf("gateway=%s admin=%s\n", front.Addr(), ops.Addr())
	if selfTest {
		err := exercise(ctx, front.Addr().String(), ops.Addr().String(), &registry)
		cancel()
		return errors.Join(err, <-done)
	}
	return <-done
}

func exercise(ctx context.Context, address, admin string, registry *codec.Registry) error {
	s, err := tcp.Dial(ctx, address, socket.Options{}, nil)
	if err != nil {
		return err
	}
	c, err := request.New(s, request.Options{ResponseIDs: []uint16{2}, Metadata: true, CancelID: 9})
	if err != nil {
		s.Close()
		return err
	}
	done := make(chan struct{})
	go func() { defer close(done); _ = s.Run(c.Handler(nil)) }()
	defer func() { c.Close(); s.Close(); <-done }()
	seen := map[string]bool{}
	for i := range 4 {
		text := fmt.Sprintf("hello-%d", i)
		f, err := registry.Encode(&protocol.Echo{Text: text}, 0)
		if err != nil {
			return err
		}
		reply, err := c.Call(ctx, f, 2)
		if err != nil {
			return err
		}
		decoded, err := registry.Decode(reply)
		if err != nil {
			return err
		}
		message := decoded.(*protocol.EchoReply)
		if message.Text != text || message.Error != "" {
			return errors.New("example: response payload mismatch")
		}
		seen[message.Backend] = true
		fmt.Printf("%s -> %s sequence=%d\n", text, message.Backend, reply.Sequence)
	}
	if len(seen) != 2 {
		return errors.New("example: both backends were not selected")
	}
	client := &http.Client{Timeout: time.Second}
	defer client.CloseIdleConnections()
	for _, path := range []string{"/livez", "/readyz", "/metrics", "/metrics/prometheus"} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+admin+path, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("example: %s returned %d", path, resp.StatusCode)
		}
	}
	return nil
}
