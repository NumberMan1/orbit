// Command client sends a single framework packet to the echo example.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/NumberMan1/orbit/rpc"
	"github.com/NumberMan1/orbit/socket"
	"github.com/NumberMan1/orbit/socket/tcp"
)

func main() {
	address := flag.String("addr", "127.0.0.1:9000", "echo TCP address")
	flag.Parse()
	if err := run(*address); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(address string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := tcp.Dial(ctx, address, socket.Options{}, nil)
	if err != nil {
		return err
	}
	defer s.Close()
	result := make(chan rpc.Frame, 1)
	done := make(chan error, 1)
	go func() {
		done <- s.Run(func(_ context.Context, _ *socket.Session, f rpc.Frame) error { result <- f; return nil })
	}()
	defer func() { s.Close(); <-s.Done() }()
	if err := s.Send(rpc.Frame{ID: 1, Sequence: 42, Payload: []byte("hello orbit")}); err != nil {
		return err
	}
	select {
	case f := <-result:
		fmt.Printf("id=%d sequence=%d payload=%s\n", f.ID, f.Sequence, f.Payload)
		return nil
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
