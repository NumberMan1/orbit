package redis

import "testing"

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{}, {Addresses: []string{""}}, {Addresses: []string{"a", "b"}}, {Addresses: []string{"a"}, Cluster: true, DB: 1}, {Addresses: []string{"a"}, Retries: -1}, {Addresses: []string{"a"}, ClusterRedirects: -1}, {Addresses: []string{"a"}, ClusterRedirects: 1}} {
		if c, err := Open(t.Context(), o); err == nil {
			c.Close()
			t.Fatal("invalid options accepted")
		}
	}
}
