// Package redis opens standalone or cluster Redis clients with explicit ownership.
package redis

import (
	"context"
	"crypto/tls"
	"errors"
	"time"

	rdb "github.com/go-redis/redis/v8"
)

type Options struct {
	Addresses []string
	Username  string
	Password  string
	DB        int
	Cluster   bool
	TLS       *tls.Config
}

// Open validates options and pings before returning. The caller must Close.
func Open(ctx context.Context, o Options) (rdb.UniversalClient, error) {
	if len(o.Addresses) == 0 || o.DB < 0 || (o.Cluster && o.DB != 0) || (!o.Cluster && len(o.Addresses) != 1) {
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
	if o.Cluster {
		client = rdb.NewClusterClient(&rdb.ClusterOptions{Addrs: append([]string(nil), o.Addresses...), Username: o.Username, Password: o.Password, TLSConfig: tlsConfig, DialTimeout: 5 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second})
	} else {
		client = rdb.NewClient(&rdb.Options{Addr: o.Addresses[0], Username: o.Username, Password: o.Password, DB: o.DB, TLSConfig: tlsConfig, DialTimeout: 5 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second})
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}
