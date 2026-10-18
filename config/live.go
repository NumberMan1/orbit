package config

import (
	"bytes"
	"errors"
	"io"
	"os"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"gopkg.in/yaml.v3"
)

type liveState[T any] struct {
	data     []byte
	value    T
	fastCopy bool
	version  uint64
	updated  time.Time
}

// Live publishes validated immutable configuration snapshots. The explicit
// policy must reject changes to restart-only fields. Reloads serialize; reads
// do not wait for disk I/O or policy evaluation. Validate/policy must be pure,
// prompt and must not recursively reload. No global watcher is installed.
type Live[T interface{ Validate() error }] struct {
	mu     sync.Mutex
	state  atomic.Pointer[liveState[T]]
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
	value, err := Decode[T](bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	l.state.Store(&liveState[T]{data: data, value: value, fastCopy: copyable(reflect.TypeOf(value)), version: 1, updated: time.Now()})
	return l, nil
}

func canonical[T interface{ Validate() error }](value T) ([]byte, error) {
	if err := validate(value); err != nil {
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
	if s == nil {
		return View[T]{}, errors.New("config: Live must be constructed with NewLive")
	}
	value, err := snapshotValue(s)
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
	if old == nil {
		return errors.New("config: Live must be constructed with NewLive")
	}
	previous, err := snapshotValue(old)
	if err != nil {
		return err
	}
	published, err := Decode[T](bytes.NewReader(data))
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
	l.state.Store(&liveState[T]{data: data, value: published, fastCopy: copyable(reflect.TypeOf(published)), version: old.version + 1, updated: time.Now()})
	return nil
}

// Hidden state from custom YAML types cannot be copied safely with reflection.
// Keep the YAML round-trip path for those types; ordinary data uses a deep copy.
func snapshotValue[T interface{ Validate() error }](s *liveState[T]) (T, error) {
	if !s.fastCopy {
		return Decode[T](bytes.NewReader(s.data))
	}
	return copyValue(s.value), nil
}
func copyable(t reflect.Type) bool {
	seen := map[reflect.Type]bool{}
	var visit func(reflect.Type) bool
	visit = func(t reflect.Type) bool {
		if t == nil {
			return false
		}
		if seen[t] {
			return true
		}
		seen[t] = true
		if t == reflect.TypeFor[time.Time]() {
			return true
		}
		switch t.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array:
			return visit(t.Elem())
		case reflect.Map:
			return visit(t.Key()) && visit(t.Elem())
		case reflect.Struct:
			for i := range t.NumField() {
				f := t.Field(i)
				if !f.IsExported() || !visit(f.Type) {
					return false
				}
			}
		// Dynamic interface values can hold custom types with hidden state.
		case reflect.Interface:
			return false
		}
		return true
	}
	return visit(t)
}

// Copy decoded data; time.Time's unexported state is immutable.
func copyValue[T any](value T) T { return copyReflect(reflect.ValueOf(value)).Interface().(T) }
func copyReflect(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return v
		}
		r := reflect.New(v.Type().Elem())
		r.Elem().Set(copyReflect(v.Elem()))
		return r
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		r := reflect.New(v.Type()).Elem()
		r.Set(copyReflect(v.Elem()))
		return r
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		r := reflect.MakeMapWithSize(v.Type(), v.Len())
		it := v.MapRange()
		for it.Next() {
			r.SetMapIndex(copyReflect(it.Key()), copyReflect(it.Value()))
		}
		return r
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		r := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := range v.Len() {
			r.Index(i).Set(copyReflect(v.Index(i)))
		}
		return r
	case reflect.Array:
		r := reflect.New(v.Type()).Elem()
		for i := range v.Len() {
			r.Index(i).Set(copyReflect(v.Index(i)))
		}
		return r
	case reflect.Struct:
		r := reflect.New(v.Type()).Elem()
		r.Set(v)
		for i := range v.NumField() {
			if r.Field(i).CanSet() && v.Type().Field(i).IsExported() {
				r.Field(i).Set(copyReflect(v.Field(i)))
			}
		}
		return r
	default:
		return v
	}
}

func (l *Live[T]) ReloadFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return l.Reload(f)
}
