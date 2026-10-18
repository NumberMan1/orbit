package socket

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"time"

	"github.com/NumberMan1/orbit/codec"
	"github.com/NumberMan1/orbit/observe"
	"github.com/NumberMan1/orbit/rpc"
)

// MessageEvent replaces reflection-based receive events, preserving correlation.
type MessageEvent struct {
	Session    *Session
	Message    any
	Sequence   uint32
	ReceivedAt time.Time
	registry   *codec.Registry
}
type receiveTimeKey struct{}

func (e MessageEvent) Reply(message any) error {
	if e.Session == nil || e.registry == nil {
		return errors.New("socket: invalid message event")
	}
	return SendMessage(e.Session, e.registry, message, e.Sequence)
}
func SendMessage(s *Session, registry *codec.Registry, message any, sequence uint32) error {
	if s == nil || registry == nil {
		return errors.New("socket: session and codec registry required")
	}
	f, err := registry.Encode(message, sequence)
	if err != nil {
		return err
	}
	return s.Send(f)
}
func DecodeHandler(registry *codec.Registry, handler func(context.Context, MessageEvent) error) (Handler, error) {
	if registry == nil || handler == nil {
		return nil, errors.New("socket: registry and message handler required")
	}
	return func(ctx context.Context, s *Session, f rpc.Frame) error {
		now, ok := ctx.Value(receiveTimeKey{}).(time.Time)
		if !ok {
			now = time.Now()
		}
		message, err := registry.Decode(f)
		if err != nil {
			return err
		}
		return handler(ctx, MessageEvent{s, message, f.Sequence, now, registry})
	}, nil
}

type Poster interface{ Post(func()) error }

// Queued runs a handler on an application queue. Enqueue errors reach Session.Run;
// asynchronous handler errors close that session unless Execute owns completion.
// It copies the frame payload. Post must reject without accepting on error.
func Queued(queue Poster, handler Handler) (Handler, error) {
	if queue == nil || handler == nil {
		return nil, errors.New("socket: queue and handler required")
	}
	return func(ctx context.Context, s *Session, f rpc.Frame) error {
		queuedAt := time.Now()
		ctx = context.WithValue(ctx, receiveTimeKey{}, queuedAt)
		f.Payload = bytes.Clone(f.Payload)
		if !s.retainWork(true) {
			return ErrClosed
		}
		run, reject := s.tracked(ctx, func() error {
			observe.Emit(s.options.Observer, observe.Event{Kind: observe.QueueWait, Duration: time.Since(queuedAt), MessageID: f.ID, TraceID: observe.TraceID(ctx)})
			if err := ctx.Err(); err != nil {
				return err
			}
			return s.handle(handler, ctx, f)
		})
		err := queue.Post(func() {
			defer s.releaseWork()
			run()
		})
		if err != nil {
			reject()
			s.releaseWork()
		}
		return err
	}, nil
}

type FrameEvent struct {
	Session    *Session
	Frame      rpc.Frame
	ReceivedAt time.Time
}

// Inbox is a bounded bridge for synchronous consumers. A full inbox returns an
// error to the network loop instead of blocking it indefinitely. Close discards
// unread events; no producer ever sends to a closed channel.
type Inbox struct {
	mu     sync.Mutex
	events chan FrameEvent
	done   chan struct{}
	closed bool
}

func NewInbox(capacity int) (*Inbox, error) {
	if capacity < 1 {
		return nil, errors.New("socket: positive inbox capacity required")
	}
	return &Inbox{events: make(chan FrameEvent, capacity), done: make(chan struct{})}, nil
}
func (i *Inbox) Handle(ctx context.Context, s *Session, f rpc.Frame) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return ErrClosed
	}
	f.Payload = bytes.Clone(f.Payload)
	select {
	case i.events <- FrameEvent{s, f, time.Now()}:
		return nil
	default:
		return ErrQueueFull
	}
}
func (i *Inbox) Receive(ctx context.Context) (FrameEvent, error) {
	if err := ctx.Err(); err != nil {
		return FrameEvent{}, err
	}
	select {
	case <-ctx.Done():
		return FrameEvent{}, ctx.Err()
	case <-i.done:
		return FrameEvent{}, ErrClosed
	case event := <-i.events:
		i.mu.Lock()
		closed := i.closed
		i.mu.Unlock()
		if closed {
			return FrameEvent{}, ErrClosed
		}
		return event, nil
	}
}
func (i *Inbox) Close() {
	i.mu.Lock()
	defer i.mu.Unlock()
	if !i.closed {
		i.closed = true
		close(i.done)
	}
	for {
		select {
		case <-i.events:
		default:
			return
		}
	}
}
