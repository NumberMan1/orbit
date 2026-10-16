// Package codec provides explicit, concurrency-safe message registration.
package codec

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/NumberMan1/orbit/rpc"
	"google.golang.org/protobuf/proto"
)

type Codec interface {
	Marshal(any) ([]byte, error)
	Unmarshal([]byte, any) error
}

type JSON struct{}

func (JSON) Marshal(v any) ([]byte, error)   { return json.Marshal(v) }
func (JSON) Unmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

type Protobuf struct{}

func (Protobuf) Marshal(v any) ([]byte, error) {
	m, ok := v.(proto.Message)
	if !ok || nilValue(v) {
		return nil, errors.New("codec: expected non-nil protobuf message")
	}
	return proto.Marshal(m)
}
func (Protobuf) Unmarshal(b []byte, v any) error {
	m, ok := v.(proto.Message)
	if !ok || nilValue(v) {
		return errors.New("codec: expected non-nil protobuf message")
	}
	return proto.Unmarshal(b, m)
}

type entry struct {
	id    uint16
	typ   reflect.Type
	codec Codec
	name  string
}
type MessageInfo struct {
	ID   uint16
	Name string
	Type reflect.Type
}
type Registry struct {
	mu     sync.RWMutex
	byID   map[uint16]entry
	byType map[reflect.Type]entry
	byName map[string]entry
}

// Register accepts a pointer to a struct as a type exemplar; it is not retained.
// Registration is atomic: a rejected duplicate leaves all indexes unchanged.
func (r *Registry) Register(id uint16, exemplar any, c Codec) error {
	return r.RegisterNamed(id, "", exemplar, c)
}

// RegisterNamed uses an explicit protocol name, or a package-qualified Go type
// name when name is empty. All three indexes are updated atomically.
func (r *Registry) RegisterNamed(id uint16, name string, exemplar any, c Codec) error {
	t := reflect.TypeOf(exemplar)
	if id == 0 || t == nil || t.Kind() != reflect.Pointer || t.Elem().Kind() != reflect.Struct || nilValue(c) {
		return errors.New("codec: nonzero ID, struct pointer and codec required")
	}
	if name == "" {
		name = t.Elem().PkgPath() + "." + t.Elem().Name()
		if t.Elem().Name() == "" {
			name = t.Elem().String()
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byID[id]; ok {
		return fmt.Errorf("codec: duplicate ID %d", id)
	}
	if _, ok := r.byType[t]; ok {
		return fmt.Errorf("codec: duplicate type %v", t)
	}
	if _, ok := r.byName[name]; ok {
		return fmt.Errorf("codec: duplicate name %s", name)
	}
	if r.byID == nil {
		r.byID = make(map[uint16]entry)
		r.byType = make(map[reflect.Type]entry)
		r.byName = make(map[string]entry)
	}
	e := entry{id, t, c, name}
	r.byID[id], r.byType[t] = e, e
	r.byName[name] = e
	return nil
}

func (r *Registry) ByID(id uint16) (MessageInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.byID[id]
	return MessageInfo{e.id, e.name, e.typ}, ok
}
func (r *Registry) ByName(name string) (MessageInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.byName[name]
	return MessageInfo{e.id, e.name, e.typ}, ok
}
func (r *Registry) ByMessage(message any) (MessageInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.byType[reflect.TypeOf(message)]
	return MessageInfo{e.id, e.name, e.typ}, ok
}

func (r *Registry) Encode(v any, sequence uint32) (rpc.Frame, error) {
	if nilValue(v) {
		return rpc.Frame{}, errors.New("codec: nil message")
	}
	r.mu.RLock()
	e, ok := r.byType[reflect.TypeOf(v)]
	r.mu.RUnlock()
	if !ok {
		return rpc.Frame{}, fmt.Errorf("codec: unregistered type %T", v)
	}
	b, err := e.codec.Marshal(v)
	if err != nil {
		return rpc.Frame{}, err
	}
	return rpc.Frame{ID: e.id, Sequence: sequence, Payload: b}, nil
}

func (r *Registry) Decode(f rpc.Frame) (any, error) {
	r.mu.RLock()
	e, ok := r.byID[f.ID]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("codec: unregistered ID %d", f.ID)
	}
	v := reflect.New(e.typ.Elem()).Interface()
	if err := e.codec.Unmarshal(f.Payload, v); err != nil {
		return nil, err
	}
	return v, nil
}

func nilValue(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return r.IsNil()
	}
	return false
}
