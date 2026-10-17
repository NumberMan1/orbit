// Package gateway provides policy-free request forwarding over bounded callers.
package gateway

import (
	"context"
	"errors"

	"github.com/NumberMan1/orbit/rpc"
	"github.com/NumberMan1/orbit/socket"
)

// Caller is implemented by request.Client. Implementations must honor ctx.
type Caller interface {
	Call(context.Context, rpc.Frame, uint16) (rpc.Frame, error)
}

// Route chooses an authenticated/authorized destination and expected response ID.
// It runs per request; identity, affinity and authorization remain application policy.
type Route func(context.Context, *socket.Session, rpc.Frame) (Caller, uint16, error)

// Forward builds a bounded handler that remaps backend correlation to the original
// client sequence. There is no automatic retry. Errors use the normal session-close
// policy; applications can wrap the handler to encode protocol-specific errors.
// The backend response reader must run independently of this frontend handler.
func Forward(maxConcurrent int, route Route) (socket.Handler, error) {
	if route == nil {
		return nil, errors.New("gateway: route required")
	}
	return socket.LimitConcurrent(maxConcurrent, func(ctx context.Context, s *socket.Session, frame rpc.Frame) error {
		caller, responseID, err := route(ctx, s, frame)
		if err != nil {
			return err
		}
		if caller == nil {
			return errors.New("gateway: route returned nil caller")
		}
		sequence := frame.Sequence
		response, err := caller.Call(ctx, frame, responseID)
		if err != nil {
			return err
		}
		response.Sequence = sequence
		return s.Send(response)
	})
}
