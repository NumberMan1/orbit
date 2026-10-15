package discovery

import (
	"context"
	"errors"
)

var (
	ErrCompacted   = errors.New("discovery: watch revision compacted")
	ErrInvalidData = errors.New("discovery: invalid snapshot or event batch")
	ErrWatchClosed = errors.New("discovery: watch stream closed")
)

// Backend reads a versioned service directory. The caller owns the backend and
// its client. Implementations must honor context cancellation and never publish
// partial transactions. Returned data must remain stable until the next call.
type Backend interface {
	List(context.Context) (ListResult, error)
	Watch(context.Context, int64) (Watcher, error)
}

type ListResult struct {
	Revision int64
	Services []Descriptor
}

type ChangeType uint8

const (
	Put ChangeType = iota + 1
	Delete
)

type Change struct {
	Type    ChangeType
	ID      string
	Service Descriptor // used only by Put; ID must equal Service.ID
}

// Batch is one complete transaction revision. An empty batch is a progress
// watermark: the watcher has delivered all changes through Revision.
type Batch struct {
	Revision int64
	Changes  []Change
}

// Watcher has one Next caller. Close cancels the stream and releases resources;
// it must be prompt, idempotent and safe concurrently with Next. A per-call
// deadline on Next must be honored even when the stream is idle.
type Watcher interface {
	Next(context.Context) (Batch, error)
	Close()
}
