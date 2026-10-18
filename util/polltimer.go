package util

import numtime "github.com/NumberMan1/numbox/pkg/time"

// PollTimer is numbox's owner-driven timer; its zero value is inactive.
// It has no goroutine and is not concurrent-safe.
type PollTimer = numtime.PollTimer
