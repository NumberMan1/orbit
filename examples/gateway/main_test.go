package main

import (
	"context"
	"testing"
	"time"
)

func TestGatewayEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := run(ctx, "127.0.0.1:0", "127.0.0.1:0", true); err != nil {
		t.Fatal(err)
	}
}
