package config

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

type reloadConfig struct {
	Listen string            `yaml:"listen"`
	Limit  int               `yaml:"limit"`
	Labels map[string]string `yaml:"labels"`
}

func (c reloadConfig) Validate() error {
	if c.Limit < 1 {
		return errors.New("invalid limit")
	}
	return nil
}
func TestLiveAtomicPolicyAndOwnedSnapshots(t *testing.T) {
	initial := reloadConfig{"local", 1, map[string]string{"key": "original"}}
	live, err := NewLive(initial, func(old, next reloadConfig) error {
		if old.Listen != next.Listen {
			return errors.New("listen requires restart")
		}
		next.Labels["key"] = "policy mutation"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	initial.Labels["key"] = "caller mutation"
	v, err := live.Snapshot()
	if err != nil || v.Value.Labels["key"] != "original" {
		t.Fatal(v, err)
	}
	v.Value.Labels["key"] = "snapshot mutation"
	for _, invalid := range []string{"listen: changed\nlimit: 2", "listen: local\nlimit: 0", "listen: local\nlimit: 2\nunknown: true"} {
		if err := live.Reload(strings.NewReader(invalid)); err == nil {
			t.Fatal("accepted invalid config")
		}
		v, _ = live.Snapshot()
		if v.Version != 1 || v.Value.Labels["key"] != "original" {
			t.Fatal(v)
		}
	}
	data := "listen: local\nlimit: 2\nlabels: {key: updated}"
	for range 2 {
		if err := live.Reload(strings.NewReader(data)); err != nil {
			t.Fatal(err)
		}
	}
	v, _ = live.Snapshot()
	if v.Version != 2 || v.Value.Limit != 2 || v.Value.Labels["key"] != "updated" {
		t.Fatal(v)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 50 {
				v, err := live.Snapshot()
				if err != nil || v.Value.Limit != int(v.Version) {
					t.Errorf("torn config: %+v %v", v, err)
				}
			}
		})
	}
	for i := 3; i < 10; i++ {
		if err := live.Reload(strings.NewReader(fmt.Sprintf("listen: local\nlimit: %d\nlabels: {key: updated}", i))); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
}
