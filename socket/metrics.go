package socket

import "sync/atomic"

// Metrics collects bounded-cardinality transport counters. Its zero value works;
// share it across sessions/listeners, never copy it after use. No exporter or
// global registry is installed; applications can expose Snapshot through HTTP.
type Metrics struct {
	active                                   atomic.Int64
	received, sent, receivedBytes, sentBytes atomic.Uint64
	rejected, handlerErrors, handlerNanos    atomic.Uint64
}

// MetricsSnapshot contains approximate concurrent totals, not a transaction.
// Bytes count payload only; Sent means written, not acknowledged by the peer.
// HandlerNanos sums dispatch and Queued callback execution time, excluding queue
// waiting. HandlerErrors counts failing/panicking dispatches and queued callbacks.
type MetricsSnapshot struct {
	Active                                   int64
	Received, Sent, ReceivedBytes, SentBytes uint64
	Rejected, HandlerErrors, HandlerNanos    uint64
}

// Snapshot reads counters without blocking transport work.
func (m *Metrics) Snapshot() MetricsSnapshot {
	return MetricsSnapshot{m.active.Load(), m.received.Load(), m.sent.Load(), m.receivedBytes.Load(), m.sentBytes.Load(), m.rejected.Load(), m.handlerErrors.Load(), m.handlerNanos.Load()}
}
