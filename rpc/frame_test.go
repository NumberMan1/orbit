package rpc

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"testing"
)

func TestWireAndBounds(t *testing.T) {
	f := Frame{ID: 7, Sequence: 0x01020304, Payload: []byte("ok")}
	b, err := Marshal(f, MaxPayload)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(b) != "000200070102030400006f6b" {
		t.Fatalf("wire %x", b)
	}
	got, err := Read(bytes.NewReader(b), 2)
	if err != nil || got.ID != f.ID || got.Sequence != f.Sequence || !bytes.Equal(got.Payload, f.Payload) {
		t.Fatalf("roundtrip %+v %v", got, err)
	}
	for n := 0; n < len(b); n++ {
		if _, err := Read(bytes.NewReader(b[:n]), MaxPayload); err == nil {
			t.Fatalf("accepted truncated frame %d", n)
		}
	}
	if _, err := Read(bytes.NewReader(b), 1); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
	b[9] = 2
	if _, err := Read(bytes.NewReader(b), 2); !errors.Is(err, ErrFlags) {
		t.Fatal(err)
	}
	if _, err := Marshal(Frame{Payload: make([]byte, MaxPayload+1)}, MaxPayload); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
	for _, limit := range []int{-1, 0, MaxPayload + 1} {
		if _, err := Marshal(f, limit); err == nil {
			t.Fatalf("limit %d accepted", limit)
		}
	}
}

type zeroWriter struct{}

func (zeroWriter) Write([]byte) (int, error) { return 0, nil }

type shortWriter struct{ bytes.Buffer }

func (w *shortWriter) Write(b []byte) (int, error) { return w.Buffer.Write(b[:1]) }
func TestShortWrites(t *testing.T) {
	if err := Write(zeroWriter{}, Frame{}, 1); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal(err)
	}
	w := new(shortWriter)
	if err := Write(w, Frame{ID: 3}, 1); err != nil {
		t.Fatal(err)
	}
	if w.Len() != HeaderSize {
		t.Fatal(w.Len())
	}
}

func FuzzRead(f *testing.F) {
	b, _ := Marshal(Frame{ID: 1, Payload: []byte("hello")}, MaxPayload)
	f.Add(b)
	f.Add([]byte{})
	f.Add([]byte{0xff, 0xff})
	f.Fuzz(func(t *testing.T, b []byte) {
		packet, err := Read(bytes.NewReader(b), 1024)
		if err == nil {
			encoded, err := Marshal(packet, 1024)
			if err != nil || len(encoded) > len(b) || !bytes.Equal(encoded, b[:len(encoded)]) {
				t.Fatalf("invalid roundtrip %v", err)
			}
		}
	})
}
