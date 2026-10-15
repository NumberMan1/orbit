package config

import (
	"strings"
	"testing"
	"time"
)

const valid = "name: demo\ntcp: '127.0.0.1:0'\nmax_payload: 65535\nsend_queue: 16\nread_timeout: 0s\nwrite_timeout: 2s\nshutdown_timeout: 5s\n"

func TestStrictConfig(t *testing.T) {
	c, err := Decode[Config](strings.NewReader(valid))
	if err != nil || c.WriteTimeout != 2*time.Second {
		t.Fatalf("%+v %v", c, err)
	}
	for _, data := range []string{valid + "typo: true\n", valid + "---\nname: other\n", strings.Replace(valid, "65535", "65536", 1), strings.Replace(valid, "127.0.0.1:0", "bad", 1), valid + "name: duplicate\n", strings.Repeat("x", MaxFileSize+1)} {
		if _, err := Decode[Config](strings.NewReader(data)); err == nil {
			t.Fatal("invalid config accepted")
		}
	}
}
