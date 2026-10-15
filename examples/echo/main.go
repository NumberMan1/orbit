// Command echo demonstrates the framework without any domain protocols.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/NumberMan1/orbit"
	"github.com/NumberMan1/orbit/config"
	"github.com/NumberMan1/orbit/rpc"
	"github.com/NumberMan1/orbit/service"
	"github.com/NumberMan1/orbit/socket"
	httpserver "github.com/NumberMan1/orbit/socket/http"
	"github.com/NumberMan1/orbit/socket/tcp"
	"github.com/NumberMan1/orbit/socket/websocket"
)

func main() {
	path := flag.String("config", "examples/echo/config.yaml", "configuration path")
	flag.Parse()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(*path, logger); err != nil {
		logger.Error("server exited", "error", err)
		os.Exit(1)
	}
}

func run(path string, logger *slog.Logger) error {
	c, err := config.Load[config.Config](path)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	options := socket.Options{MaxPayload: c.MaxPayload, SendQueue: c.SendQueue, ReadTimeout: c.ReadTimeout, WriteTimeout: c.WriteTimeout, Logger: logger}
	handler := func(_ context.Context, s *socket.Session, f rpc.Frame) error { return s.Send(f) }
	var components []orbit.Component
	if c.TCP != "" {
		server, err := tcp.NewServer(c.TCP, options, handler, nil)
		if err != nil {
			return err
		}
		components = append(components, server)
	}
	if c.HTTP != "" {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintln(w, `{"status":"ok"}`)
		})
		server, err := httpserver.NewServer(c.HTTP, mux, nil)
		if err != nil {
			return err
		}
		components = append(components, server)
	}
	if c.WebSocket != "" {
		server, err := websocket.NewServer(c.WebSocket, options, handler, nil, nil)
		if err != nil {
			return err
		}
		components = append(components, server)
	}
	app, err := service.NewApp(c.ShutdownTimeout, components...)
	if err != nil {
		return err
	}
	logger.Info("starting", "name", c.Name, "tcp", c.TCP, "http", c.HTTP, "websocket", c.WebSocket)
	return app.Run(ctx)
}
