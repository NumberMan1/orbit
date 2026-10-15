// Command discovery exposes a local read-only view of an etcd service directory.
package main

import (
	"context"
	"encoding/json/v2"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/NumberMan1/orbit/discovery"
	"github.com/NumberMan1/orbit/service"
	httpserver "github.com/NumberMan1/orbit/socket/http"
	clientv3 "go.etcd.io/etcd/client/v3"
)

func main() {
	endpoints := flag.String("endpoints", "http://127.0.0.1:2379", "comma-separated etcd endpoints")
	prefix := flag.String("prefix", "/orbit/example/services", "service directory")
	listen := flag.String("listen", "127.0.0.1:9010", "local HTTP address")
	flag.Parse()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(*endpoints, *prefix, *listen, logger); err != nil {
		logger.Error("discovery exited", "error", err)
		os.Exit(1)
	}
}

func run(endpoints, prefix, listen string, logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	client, err := clientv3.New(clientv3.Config{Endpoints: strings.Split(endpoints, ","), DialTimeout: 5 * time.Second})
	if err != nil {
		return err
	}
	defer client.Close() // App stops the Store before this borrowed client is closed.
	backend, err := discovery.NewEtcdBackend(client, prefix)
	if err != nil {
		return err
	}
	store, err := discovery.NewStore(backend, discovery.StoreOptions{
		Logger: logger,
		Observer: func(event discovery.StoreEvent) {
			logger.Info("discovery sync", "kind", event.Kind, "revision", event.Status.Revision,
				"count", event.Status.Count, "ready", event.Status.Ready, "error", event.Status.LastError)
		},
	})
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if !store.Status().Ready {
			http.Error(w, "directory unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /services", func(w http.ResponseWriter, r *http.Request) {
		if !store.Status().Ready {
			http.Error(w, "directory unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.MarshalWrite(w, store.Snapshot()); err != nil {
			logger.Warn("write snapshot", "error", err)
		}
	})
	server, err := httpserver.NewServer(listen, mux, nil)
	if err != nil {
		return err
	}
	app, err := service.NewApp(5*time.Second, store, server)
	if err != nil {
		return err
	}
	return app.Run(ctx)
}
