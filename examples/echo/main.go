// Command echo demonstrates the framework without any domain protocols.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	serverlog "github.com/NumberMan1/log"
	"github.com/NumberMan1/log/field"
	"github.com/NumberMan1/orbit"
	"github.com/NumberMan1/orbit/config"
	"github.com/NumberMan1/orbit/rpc"
	"github.com/NumberMan1/orbit/service"
	"github.com/NumberMan1/orbit/socket"
	httpserver "github.com/NumberMan1/orbit/socket/http"
	"github.com/NumberMan1/orbit/socket/tcp"
	"github.com/NumberMan1/orbit/socket/websocket"
)

func main() { os.Exit(mainCode()) }

func mainCode() int {
	path := flag.String("config", "examples/echo/config.yaml", "configuration path")
	flag.Parse()
	raw, err := serverlog.NewConfiguredLogger(serverlog.Config{Level: "info", Stdout: true, StdoutTyp: "json"})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer raw.Close() // Flush only after components and their callbacks have stopped.
	logger := serverlog.NewServerLogger(raw)
	if err := run(*path, logger); err != nil {
		logger.Error("server exited", field.Error(err))
		return 1
	}
	return 0
}

func run(path string, logger serverlog.Logger) error {
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
	logger.Info("starting", field.String("name", c.Name), field.String("tcp", c.TCP), field.String("http", c.HTTP), field.String("websocket", c.WebSocket))
	return app.Run(ctx)
}
