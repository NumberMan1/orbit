package redis

import "testing"

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{}, {Addresses: []string{""}}, {Addresses: []string{"a", "b"}}, {Addresses: []string{"a"}, Cluster: true, DB: 1}} {
		if c, err := Open(t.Context(), o); err == nil {
			c.Close()
			t.Fatal("invalid options accepted")
		}
	}
}
