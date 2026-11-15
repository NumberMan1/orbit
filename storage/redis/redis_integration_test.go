//go:build integration

package redis

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	rdb "github.com/redis/go-redis/v9"
)

func TestRedisIntegration(t *testing.T) {
	address := os.Getenv("ORBIT_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("set ORBIT_TEST_REDIS_ADDR to an isolated test instance")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	c, err := Open(ctx, Options{Addresses: []string{address}, Password: os.Getenv("ORBIT_TEST_REDIS_PASSWORD")})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if pong, err := c.Ping(ctx).Result(); err != nil || pong != "PONG" {
		t.Fatalf("PING: %s %v", pong, err)
	}
}

func TestRedisClusterIntegration(t *testing.T) {
	addresses := os.Getenv("ORBIT_TEST_REDIS_CLUSTER_ADDRS")
	if addresses == "" {
		t.Skip("set ORBIT_TEST_REDIS_CLUSTER_ADDRS to comma-separated cluster addresses")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	c, err := Open(ctx, Options{
		Addresses: strings.Split(addresses, ","), Cluster: true, ClusterRedirects: 3,
		Password: os.Getenv("ORBIT_TEST_REDIS_CLUSTER_PASSWORD"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	key := "orbit:test:" + rand.Text()
	if err := c.Set(ctx, key, "test", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	if value, err := c.GetDel(ctx, key).Result(); err != nil || value != "test" {
		t.Fatalf("GETDEL: %q %v", value, err)
	}
	if err := c.Get(ctx, key).Err(); !errors.Is(err, rdb.Nil) {
		t.Fatalf("GET after delete: %v", err)
	}
}
