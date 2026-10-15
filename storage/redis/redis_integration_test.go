//go:build integration

package redis

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestRedisIntegration(t *testing.T) {
	address := os.Getenv("ORBIT_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("set ORBIT_TEST_REDIS_ADDR to an isolated test instance")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	c, err := Open(ctx, Options{Addresses: []string{address}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if pong, err := c.Ping(ctx).Result(); err != nil || pong != "PONG" {
		t.Fatalf("PING: %s %v", pong, err)
	}
}
