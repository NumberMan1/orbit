// Package redis opens standalone or cluster Redis clients with explicit ownership.
package redis

import (
	"context"
	"crypto/tls"
	"errors"
	"time"

	rdb "github.com/redis/go-redis/v9"
)

type Options struct {
	Addresses []string
	Username  string
	Password  string
	DB        int
	Cluster   bool
	TLS       *tls.Config
	// Retries explicitly opts into replay after ambiguous network failures.
	// Zero disables retries; positive values are unsafe for non-idempotent writes
	// unless the application provides its own deduplication protocol.
	Retries int
	// ClusterRedirects also retries network failures in the cluster client.
	// Zero disables automatic redirects/replays; opt in deliberately for clusters.
	ClusterRedirects int
}

// Open validates options and pings before returning. The caller must Close.
func Open(ctx context.Context, o Options) (rdb.UniversalClient, error) {
	if len(o.Addresses) == 0 || o.DB < 0 || o.Retries < 0 || o.ClusterRedirects < 0 || (o.Cluster && o.DB != 0) || (!o.Cluster && (len(o.Addresses) != 1 || o.ClusterRedirects != 0)) {
		return nil, errors.New("redis: invalid addresses, database or cluster options")
	}
	for _, a := range o.Addresses {
		if a == "" {
			return nil, errors.New("redis: empty address")
		}
	}
	var tlsConfig *tls.Config
	if o.TLS != nil {
		tlsConfig = o.TLS.Clone()
	}
	var client rdb.UniversalClient
	retries, redirects := o.Retries, o.ClusterRedirects
	if retries == 0 {
		retries = -1
	}
	if redirects == 0 {
		redirects = -1
	}
	if o.Cluster {
		client = rdb.NewClusterClient(&rdb.ClusterOptions{
			Addrs: append([]string(nil), o.Addresses...), Username: o.Username, Password: o.Password, TLSConfig: tlsConfig,
			Protocol: 2, ContextTimeoutEnabled: true, MaxRetries: retries, MaxRedirects: redirects,
			DialTimeout: 5 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second,
		})
	} else {
		client = rdb.NewClient(&rdb.Options{
			Addr: o.Addresses[0], Username: o.Username, Password: o.Password, DB: o.DB, TLSConfig: tlsConfig,
			Protocol: 2, ContextTimeoutEnabled: true, MaxRetries: retries,
			DialTimeout: 5 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second,
		})
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}
