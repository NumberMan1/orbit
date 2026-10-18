package config

import (
	"bytes"
	"errors"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"gopkg.in/yaml.v3"
)

type liveState struct {
	data    []byte
	version uint64
	updated time.Time
}

// Live publishes validated immutable configuration snapshots. The explicit
// policy must reject changes to restart-only fields. Reloads serialize; reads
// do not wait for disk I/O or policy evaluation. Validate/policy must be pure,
// prompt and must not recursively reload. No global watcher is installed.
type Live[T interface{ Validate() error }] struct {
	mu     sync.Mutex
	state  atomic.Pointer[liveState]
	policy func(previous, next T) error
}

// View owns Value, including its maps/slices/pointers; mutation cannot change the
// published snapshot. Types must round-trip through YAML without runtime handles.
type View[T any] struct {
	Value     T
	Version   uint64
	UpdatedAt time.Time
}

func NewLive[T interface{ Validate() error }](initial T, policy func(T, T) error) (*Live[T], error) {
	if policy == nil {
		return nil, errors.New("config: explicit reload policy required")
	}
	data, err := canonical(initial)
	if err != nil {
		return nil, err
	}
	l := &Live[T]{policy: policy}
	l.state.Store(&liveState{data, 1, time.Now()})
	return l, nil
}

func canonical[T interface{ Validate() error }](value T) ([]byte, error) {
	if err := value.Validate(); err != nil {
		return nil, err
	}
	data, err := yaml.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(data) > MaxFileSize {
		return nil, errors.New("config: file too large")
	}
	_, err = Decode[T](bytes.NewReader(data))
	return data, err
}

func (l *Live[T]) Snapshot() (View[T], error) {
	s := l.state.Load()
	value, err := Decode[T](bytes.NewReader(s.data))
	return View[T]{value, s.version, s.updated}, err
}

// Reload validates the entire candidate and policy before atomically publishing.
// Errors leave both version and value unchanged. Equivalent YAML is a no-op.
// Readers must be bounded/cooperative; use ReloadFile for ordinary local files.
func (l *Live[T]) Reload(r io.Reader) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	next, err := Decode[T](r)
	if err != nil {
		return err
	}
	data, err := canonical(next)
	if err != nil {
		return err
	}
	old := l.state.Load()
	previous, err := Decode[T](bytes.NewReader(old.data))
	if err != nil {
		return err
	}
	// Policy receives independent values; its accidental mutations are not published.
	if err := l.policy(previous, next); err != nil {
		return err
	}
	if bytes.Equal(old.data, data) {
		return nil
	}
	if old.version == ^uint64(0) {
		return errors.New("config: version exhausted")
	}
	l.state.Store(&liveState{data, old.version + 1, time.Now()})
	return nil
}

func (l *Live[T]) ReloadFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return l.Reload(f)
}
