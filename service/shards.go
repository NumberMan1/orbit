package service

import (
	"context"
	"errors"
	"hash/fnv"
	"sync"

	serverlog "github.com/NumberMan1/log"
)

// Shards routes string entity keys to a fixed number of bounded serial queues.
// The same key always runs on one queue; different keys may share a queue.
// Accepted jobs are FIFO per shard, in Post lock-acquisition order. No per-key
// goroutines or unbounded entity table are created. Do not copy after use.
type Shards struct {
	mu              sync.Mutex
	queues          []*Queue
	started, closed bool
}

func NewShards(count, capacityPerShard int, logger serverlog.Logger) (*Shards, error) {
	if count < 1 || capacityPerShard < 1 {
		return nil, errors.New("service: positive shard count and capacity required")
	}
	s := &Shards{}
	for range count {
		q, err := NewQueue(capacityPerShard, 0, nil, logger)
		if err != nil {
			return nil, err
		}
		s.queues = append(s.queues, q)
	}
	return s, nil
}

func (s *Shards) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started || s.closed {
		return ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, q := range s.queues {
		if err := q.Start(ctx); err != nil {
			s.closed = true
			for _, q := range s.queues {
				_ = q.Stop(context.Background())
			}
			return err
		}
	}
	s.started = true
	return nil
}

// Post is nonblocking; the caller retains responsibility for the lifetime of
// captured data. A full shard returns ErrFull without executing the job.
func (s *Shards) Post(key string, fn func()) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started || s.closed {
		return ErrClosed
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	return s.queues[h.Sum64()%uint64(len(s.queues))].Post(fn)
}

type keyedPoster struct {
	shards *Shards
	key    string
}

func (p keyedPoster) Post(fn func()) error { return p.shards.Post(p.key, fn) }

// For returns a Poster suitable for socket.Queued. The key is application-owned;
// hashing selects execution only, not distributed entity ownership.
func (s *Shards) For(key string) interface{ Post(func()) error } { return keyedPoster{s, key} }

// Stop rejects new posts across all shards before draining accepted jobs.
// Call from outside jobs. A timeout cannot kill a non-cooperative callback.
func (s *Shards) Stop(ctx context.Context) error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	var wg sync.WaitGroup
	var mu sync.Mutex
	var result error
	for _, q := range s.queues {
		wg.Go(func() {
			if err := q.Stop(ctx); err != nil {
				mu.Lock()
				result = errors.Join(result, err)
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	return result
}

// Drain finishes jobs while application dependencies remain alive.
func (s *Shards) Drain(ctx context.Context) error { return s.Stop(ctx) }

func (s *Shards) Status() []QueueStatus {
	all := make([]QueueStatus, len(s.queues))
	for i, q := range s.queues {
		all[i] = q.Status()
	}
	return all
}
