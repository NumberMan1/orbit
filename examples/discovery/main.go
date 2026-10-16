// Command discovery exposes a local read-only view of an etcd service directory.
package main

import (
	"context"
	"encoding/json/v2"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	serverlog "github.com/NumberMan1/log"
	"github.com/NumberMan1/log/field"
	"github.com/NumberMan1/orbit/discovery"
	"github.com/NumberMan1/orbit/service"
	httpserver "github.com/NumberMan1/orbit/socket/http"
	clientv3 "go.etcd.io/etcd/client/v3"
)

func main() { os.Exit(mainCode()) }

func mainCode() int {
	endpoints := flag.String("endpoints", "http://127.0.0.1:2379", "comma-separated etcd endpoints")
	prefix := flag.String("prefix", "/orbit/example/services", "service directory")
	listen := flag.String("listen", "127.0.0.1:9010", "local HTTP address")
	flag.Parse()
	raw, err := serverlog.NewConfiguredLogger(serverlog.Config{Level: "info", Stdout: true, StdoutTyp: "json"})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer raw.Close() // Flush only after components and their callbacks have stopped.
	logger := serverlog.NewServerLogger(raw)
	if err := run(*endpoints, *prefix, *listen, logger); err != nil {
		logger.Error("discovery exited", field.Error(err))
		return 1
	}
	return 0
}

func run(endpoints, prefix, listen string, logger serverlog.Logger) error {
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
			logger.Info("discovery sync", field.String("kind", event.Kind), field.Int64("revision", event.Status.Revision),
				field.Int("count", event.Status.Count), field.Bool("ready", event.Status.Ready), field.Error(event.Status.LastError))
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
			logger.Warn("write snapshot", field.Error(err))
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
