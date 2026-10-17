package util

import (
	"log/slog"
	"time"

	serverlog "github.com/NumberMan1/log"
	"github.com/NumberMan1/numbox/pkg/timewheel"
)

var (
	// ErrTimerExists reports a duplicate pending key; it is the upstream sentinel.
	ErrTimerExists = timewheel.ErrTimerExists
	// ErrClockBackwards reports an Advance time earlier than the last observation.
	ErrClockBackwards = timewheel.ErrClockBackwards
	// ErrInvalidKey reports a non-comparable or non-reflexive key (for example NaN).
	ErrInvalidKey = timewheel.ErrInvalidKey
)

// WheelOptions configures the numbox wheel and Orbit's borrowed logger.
type WheelOptions struct {
	// Tick is the positive scheduling resolution; deadlines round up to a tick.
	Tick time.Duration
	// Slots is the number of buckets and must be at least two.
	Slots int
	// Logger is borrowed; nil captures the current configured default logger.
	Logger serverlog.Logger
}

// TimeWheel is numbox's caller-driven wheel. All methods must run on one owner
// (for example service.Queue); callbacks run serially and must not reenter Advance.
// Construct it with NewTimeWheel; the zero value is not ready for use.
type TimeWheel[K comparable] = timewheel.TimeWheel[K]

// NewTimeWheel creates a numbox wheel without background goroutines or timers.
// It adapts diagnostics to Logger without taking ownership of its core.
func NewTimeWheel[K comparable](start time.Time, options WheelOptions) (*TimeWheel[K], error) {
	logger := options.Logger
	if logger == nil {
		logger = serverlog.DefaultLogger()
	}
	return timewheel.NewTimeWheel[K](start, timewheel.WheelOptions{
		Tick: options.Tick, Slots: options.Slots,
		Logger: slog.New(wheelLogHandler{logger: logger}),
	})
}
