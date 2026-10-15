// Package rpc implements the business-independent RO packet envelope.
package rpc

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const HeaderSize = 10
const MaxPayload = 65535

var (
	ErrTooLarge = errors.New("rpc: payload exceeds limit")
	ErrFlags    = errors.New("rpc: unsupported flags; use TLS for encryption")
)

// Frame retains the original big-endian length/id/sequence/flags wire layout.
// The length field contains only the payload length. Flags must be zero.
type Frame struct {
	ID       uint16
	Sequence uint32
	Payload  []byte
}

func ValidateLimit(limit int) error {
	if limit < 1 || limit > MaxPayload {
		return fmt.Errorf("rpc: limit must be between 1 and %d", MaxPayload)
	}
	return nil
}

func Read(r io.Reader, limit int) (Frame, error) {
	if err := ValidateLimit(limit); err != nil {
		return Frame{}, err
	}
	var header [HeaderSize]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return Frame{}, err
	}
	n := int(binary.BigEndian.Uint16(header[:2]))
	if n > limit {
		return Frame{}, ErrTooLarge
	}
	if binary.BigEndian.Uint16(header[8:]) != 0 {
		return Frame{}, ErrFlags
	}
	f := Frame{ID: binary.BigEndian.Uint16(header[2:4]), Sequence: binary.BigEndian.Uint32(header[4:8]), Payload: make([]byte, n)}
	_, err := io.ReadFull(r, f.Payload)
	return f, err
}

func Marshal(f Frame, limit int) ([]byte, error) {
	if err := ValidateLimit(limit); err != nil {
		return nil, err
	}
	if len(f.Payload) > limit {
		return nil, ErrTooLarge
	}
	b := make([]byte, HeaderSize+len(f.Payload))
	binary.BigEndian.PutUint16(b[:2], uint16(len(f.Payload)))
	binary.BigEndian.PutUint16(b[2:4], f.ID)
	binary.BigEndian.PutUint32(b[4:8], f.Sequence)
	copy(b[HeaderSize:], f.Payload)
	return b, nil
}

func Write(w io.Writer, f Frame, limit int) error {
	b, err := Marshal(f, limit)
	if err != nil {
		return err
	}
	for len(b) > 0 {
		n, err := w.Write(b)
		if n < 0 || n > len(b) {
			return io.ErrShortWrite
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}
