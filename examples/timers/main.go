// Command timers runs a caller-driven timing wheel on a serial application queue.
package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/NumberMan1/orbit/service"
	"github.com/NumberMan1/orbit/util"
)

func main() {
	if err := run(); err != nil {
		slog.Error("timers exited", "error", err)
		os.Exit(1)
	}
}
func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	wheel, err := util.NewTimeWheel[string](time.Now(), util.WheelOptions{Tick: 10 * time.Millisecond, Slots: 256})
	if err != nil {
		return err
	}
	count := 0
	if err := wheel.Schedule("pulse", time.Now().Add(30*time.Millisecond), 100*time.Millisecond, func(now time.Time) {
		count++
		slog.Info("pulse", "count", count, "at", now)
		if count == 3 {
			wheel.Cancel("pulse")
			cancel()
		}
	}); err != nil {
		return err
	}
	queue, err := service.NewQueue(128, 10*time.Millisecond, func(time.Time) {
		// Use actual current time to recover from dropped ticker notifications.
		if _, err := wheel.Advance(time.Now(), 64); err != nil {
			slog.Error("advance", "error", err)
			cancel()
		}
	}, nil)
	if err != nil {
		return err
	}
	app, err := service.NewApp(time.Second, queue)
	if err != nil {
		return err
	}
	return app.Run(ctx)
}
