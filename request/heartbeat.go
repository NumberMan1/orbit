package request

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/NumberMan1/orbit/rpc"
	"github.com/NumberMan1/orbit/socket"
)

var ErrUnresponsive = errors.New("request: heartbeat response timeout threshold reached")

type HeartbeatOptions struct {
	PingID, PongID    uint16
	Interval, Timeout time.Duration
	MissLimit         int
	// Explicit opt-in: close the borrowed session to trigger connector recovery.
	CloseOnFailure bool
}
type HeartbeatStatus struct {
	Healthy   bool
	Replies   uint64
	Misses    int
	RTT       time.Duration
	LastReply time.Time
}

// Heartbeat borrows the session's single Client and its unique sequence space.
// Run once under the connection lifetime; create a new instance after reconnect.
// It does not change application health checks or reserve global message IDs.
type Heartbeat struct {
	client  *Client
	options HeartbeatOptions
	used    atomic.Bool
	mu      sync.Mutex
	status  HeartbeatStatus
}

func NewHeartbeat(client *Client, options HeartbeatOptions) (*Heartbeat, error) {
	if client == nil || options.PingID == 0 || options.PongID == 0 || options.PingID == options.PongID || options.Interval <= 0 || options.Timeout <= 0 || options.MissLimit < 1 {
		return nil, errors.New("request: invalid heartbeat options")
	}
	if !client.responses[options.PongID] || client.responses[options.PingID] || options.PingID == client.options.CancelID || (client.options.Metadata && !client.raw[options.PingID]) {
		return nil, errors.New("request: heartbeat requires raw reserved ping/pong IDs on the client")
	}
	return &Heartbeat{client: client, options: options}, nil
}
func (h *Heartbeat) Status() HeartbeatStatus { h.mu.Lock(); defer h.mu.Unlock(); return h.status }
func (h *Heartbeat) Run(ctx context.Context) error {
	if !h.used.CompareAndSwap(false, true) {
		return errors.New("request: heartbeat already run")
	}
	defer func() { h.mu.Lock(); h.status.Healthy = false; h.mu.Unlock() }()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		probe, cancel := context.WithTimeout(ctx, h.options.Timeout)
		start := time.Now()
		frame, err := h.client.Call(probe, rpc.Frame{ID: h.options.PingID}, h.options.PongID)
		cancel()
		if errors.Is(err, socket.ErrDraining) {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil && len(frame.Payload) != 0 {
			return errors.New("request: invalid heartbeat response")
		}
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		h.mu.Lock()
		if err == nil {
			h.status.Healthy = true
			h.status.Misses = 0
			h.status.Replies++
			h.status.RTT = time.Since(start)
			h.status.LastReply = time.Now()
		} else {
			h.status.Healthy = false
			h.status.Misses++
		}
		failed := h.status.Misses >= h.options.MissLimit
		h.mu.Unlock()
		if failed {
			if h.options.CloseOnFailure {
				h.client.session.Close()
			}
			return ErrUnresponsive
		}
		timer := time.NewTimer(h.options.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-h.client.session.Context().Done():
			timer.Stop()
			return ErrClosed
		case <-timer.C:
		}
	}
}

// RespondHeartbeat handles reserved, raw ping frames before application decoding.
// Pong preserves the request sequence; all other IDs are delegated to next.
func RespondHeartbeat(pingID, pongID uint16, next socket.Handler) (socket.Handler, error) {
	if pingID == 0 || pongID == 0 || pingID == pongID {
		return nil, errors.New("request: invalid heartbeat IDs")
	}
	return func(ctx context.Context, s *socket.Session, f rpc.Frame) error {
		s.AllowDuringDrain(pingID)
		if f.ID != pingID {
			if next != nil {
				return next(ctx, s, f)
			}
			return nil
		}
		if f.Sequence == 0 || len(f.Payload) != 0 {
			return errors.New("request: malformed heartbeat")
		}
		return s.Send(rpc.Frame{ID: pongID, Sequence: f.Sequence})
	}, nil
}
