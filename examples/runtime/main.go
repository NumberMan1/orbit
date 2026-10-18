// Command runtime demonstrates isolated config snapshots and entity-keyed jobs.
package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/NumberMan1/orbit/config"
	"github.com/NumberMan1/orbit/service"
)

type settings struct {
	Listen string `yaml:"listen"`
	Limit  int    `yaml:"limit"`
}

func (s settings) Validate() error {
	if s.Listen == "" || s.Limit < 1 {
		return errors.New("invalid settings")
	}
	return nil
}
func main() {
	if err := run(context.Background()); err != nil {
		panic(err)
	}
}
func run(ctx context.Context) error {
	live, err := config.NewLive(settings{"127.0.0.1:9100", 10}, func(previous, next settings) error {
		if previous.Listen != next.Listen {
			return errors.New("listen requires restart")
		}
		return nil
	})
	if err != nil {
		return err
	}
	workers, err := service.NewShards(4, 16, nil)
	if err != nil {
		return err
	}
	if err = workers.Start(ctx); err != nil {
		return err
	}
	defer workers.Stop(context.Background())
	results := make(chan string, 3)
	for i := range 3 {
		view, err := live.Snapshot()
		if err != nil {
			return err
		}
		if err = workers.Post("room-1", func() { results <- fmt.Sprintf("job=%d version=%d limit=%d", i, view.Version, view.Value.Limit) }); err != nil {
			return err
		}
		if i == 0 {
			if err = live.Reload(strings.NewReader("listen: 127.0.0.1:9100\nlimit: 20")); err != nil {
				return err
			}
		}
	}
	if err = live.Reload(strings.NewReader("listen: 0.0.0.0:9200\nlimit: 30")); err == nil {
		return errors.New("restart-only change accepted")
	}
	if err = workers.Drain(ctx); err != nil {
		return err
	}
	close(results)
	for line := range results {
		fmt.Println(line)
	}
	return nil
}
