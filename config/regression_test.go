package config

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type pointerConfig struct {
	Name string `yaml:"name"`
}

func (c *pointerConfig) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("name required")
	}
	return nil
}
func TestNullPointerConfigRejected(t *testing.T) {
	for _, text := range []string{"null", "~", ""} {
		if _, err := Decode[*pointerConfig](strings.NewReader(text)); err == nil {
			t.Fatalf("accepted %q", text)
		}
	}
	if _, err := NewLive[*pointerConfig](nil, func(*pointerConfig, *pointerConfig) error { return nil }); err == nil {
		t.Fatal("accepted nil initial value")
	}
}

var validationCalls atomic.Int64

type nestedConfig struct {
	Values map[string][]*pointerConfig `yaml:"values"`
	When   time.Time                   `yaml:"when"`
}

func (n nestedConfig) Validate() error { validationCalls.Add(1); return nil }
func TestLiveReadOwnsNestedValuesWithoutRevalidation(t *testing.T) {
	initial := nestedConfig{Values: map[string][]*pointerConfig{"a": {{Name: "original"}}}, When: time.Now().UTC()}
	l, err := NewLive(initial, func(nestedConfig, nestedConfig) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	before := validationCalls.Load()
	v, err := l.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	v.Value.Values["a"][0].Name = "mutated"
	v, err = l.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if v.Value.Values["a"][0].Name != "original" || !v.Value.When.Equal(initial.When) {
		t.Fatal("snapshot alias or lost time", v)
	}
	if validationCalls.Load() != before {
		t.Fatal("read revalidated configuration")
	}
}

type hiddenConfig struct {
	Values []string `yaml:"values"`
	hidden []string
}

func (c *hiddenConfig) UnmarshalYAML(n *yaml.Node) error {
	type plain hiddenConfig
	if err := n.Decode((*plain)(c)); err != nil {
		return err
	}
	c.hidden = append([]string(nil), c.Values...)
	return nil
}
func (hiddenConfig) Validate() error { return nil }
func TestLiveCustomYAMLHiddenStateRemainsOwned(t *testing.T) {
	l, err := NewLive(hiddenConfig{Values: []string{"original"}}, func(hiddenConfig, hiddenConfig) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	a, err := l.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	a.Value.hidden[0] = "changed"
	b, err := l.Snapshot()
	if err != nil || b.Value.hidden[0] != "original" {
		t.Fatal("aliased hidden state", b, err)
	}
}

func BenchmarkLiveSnapshot(b *testing.B) {
	for _, size := range []int{3, 100} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			c := reloadConfig{Listen: "local", Limit: 1, Labels: map[string]string{}}
			for i := range size {
				c.Labels[fmt.Sprint(i)] = "value"
			}
			l, err := NewLive(c, func(reloadConfig, reloadConfig) error { return nil })
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := l.Snapshot(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
