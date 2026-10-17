package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckAndHandwrittenProtection(t *testing.T) {
	dir := t.TempDir()
	in, out := filepath.Join(dir, "messages.json"), filepath.Join(dir, "bindings.go")
	manifest := []byte(`{"package":"test","messages":[{"id":1,"type":"Ping","codec":"json"}]}`)
	if err := os.WriteFile(in, manifest, 0600); err != nil {
		t.Fatal(err)
	}
	if err := run(in, out, false); err != nil {
		t.Fatal(err)
	}
	if err := run(in, out, true); err != nil {
		t.Fatal(err)
	}
	manual := []byte("package test\n// handwritten\n")
	if err := os.WriteFile(out, manual, 0600); err != nil {
		t.Fatal(err)
	}
	if err := run(in, out, true); err == nil {
		t.Fatal("stale output accepted")
	}
	if err := run(in, out, false); err == nil {
		t.Fatal("handwritten file overwritten")
	}
	got, _ := os.ReadFile(out)
	if string(got) != string(manual) {
		t.Fatal(string(got))
	}
}
