// Command timers runs a caller-driven timing wheel on a serial application queue.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	serverlog "github.com/NumberMan1/log"
	"github.com/NumberMan1/log/field"
	"github.com/NumberMan1/numbox/pkg/timewheel"
	"github.com/NumberMan1/orbit/logging"
	"github.com/NumberMan1/orbit/service"
)

func main() { os.Exit(mainCode()) }

func mainCode() int {
	raw, err := serverlog.NewConfiguredLogger(serverlog.Config{Level: "info", Stdout: true, StdoutTyp: "json"})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer raw.Close() // Flush only after components and their callbacks have stopped.
	logger := serverlog.NewServerLogger(raw)
	if err := run(logger); err != nil {
		logger.Error("timers exited", field.Error(err))
		return 1
	}
	return 0
}
func run(logger serverlog.Logger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	wheel, err := timewheel.NewTimeWheel[string](time.Now(), timewheel.WheelOptions{Tick: 10 * time.Millisecond, Slots: 256, Logger: logging.NewSlogLogger(logger)})
	if err != nil {
		return err
	}
	count := 0
	if err := wheel.Schedule("pulse", time.Now().Add(30*time.Millisecond), 100*time.Millisecond, func(now time.Time) {
		count++
		logger.Info("pulse", field.Int("count", count), field.Any("at", now))
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
			logger.Error("advance", field.Error(err))
			cancel()
		}
	}, logger)
	if err != nil {
		return err
	}
	app, err := service.NewApp(time.Second, queue)
	if err != nil {
		return err
	}
	return app.Run(ctx)
}
