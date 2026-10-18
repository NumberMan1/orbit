package request

import (
	"context"
	"encoding/json/v2"
	"errors"
	"time"

	"github.com/NumberMan1/orbit/observe"
	"github.com/NumberMan1/orbit/rpc"
)

// metadataV1 is opt-in on BOTH ends. It wraps request payloads only; frame layout
// and response bodies remain unchanged. Never enable on unnegotiated legacy IDs.
type metadataV1 struct {
	Version  int    `json:"version"`
	Deadline int64  `json:"deadline_unix_milli"`
	TraceID  string `json:"trace_id"`
	Payload  []byte `json:"payload"`
}

func pack(ctx context.Context, f rpc.Frame) (rpc.Frame, error) {
	deadline, _ := ctx.Deadline()
	data, err := json.Marshal(metadataV1{1, deadline.UnixMilli(), observe.TraceID(ctx), f.Payload})
	if err != nil {
		return rpc.Frame{}, err
	}
	if len(data) > rpc.MaxPayload {
		return rpc.Frame{}, rpc.ErrTooLarge
	}
	f.Payload = data
	return f, nil
}

func unpack(ctx context.Context, f rpc.Frame) (context.Context, context.CancelFunc, rpc.Frame, error) {
	var m metadataV1
	if err := json.Unmarshal(f.Payload, &m, json.RejectUnknownMembers(true)); err != nil {
		return nil, nil, rpc.Frame{}, err
	}
	if m.Version != 1 || m.Deadline <= 0 {
		return nil, nil, rpc.Frame{}, errors.New("request: invalid metadata version/deadline")
	}
	ctx, err := observe.WithTraceID(ctx, m.TraceID)
	if err != nil {
		return nil, nil, rpc.Frame{}, err
	}
	ctx, cancel := context.WithDeadline(ctx, time.UnixMilli(m.Deadline))
	f.Payload = m.Payload
	return ctx, cancel, f, nil
}
