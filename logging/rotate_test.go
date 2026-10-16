package logging

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestRotationBoundsOwnershipAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.log")
	w, err := NewRotatingWriter(FileOptions{Path: path, MaxBytes: 8, Backups: 2})
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range []string{"1111", "2222", "3333", "4444", "5555", "6666", "7777"} {
		if _, err := w.Write([]byte(record)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := w.Write([]byte("oversized")); !errors.Is(err, ErrRecordTooLarge) {
		t.Fatal(err)
	}
	w.Close()
	w.Close()
	for name, want := range map[string]string{path: "7777", path + ".1": "55556666", path + ".2": "33334444"} {
		got, err := os.ReadFile(name)
		if err != nil || string(got) != want {
			t.Fatal(name, string(got), err)
		}
	}
	if _, err := w.Write([]byte("x")); !errors.Is(err, os.ErrClosed) {
		t.Fatal(err)
	}
	w, err = NewRotatingWriter(FileOptions{Path: path, MaxBytes: 8, Backups: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.Write([]byte("8888"))
	w.Write([]byte("9999"))
	w.Sync()
	if data, _ := os.ReadFile(path); string(data) != "9999" {
		t.Fatal(string(data))
	}
}
func TestSlogConcurrentRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log")
	w, err := NewRotatingWriter(FileOptions{Path: path, MaxBytes: 512, Backups: 3})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	logger := slog.New(slog.NewJSONHandler(w, nil))
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 20 {
				logger.Info("event", "value", 42)
			}
		})
	}
	wg.Wait()
	w.Sync()
	files, _ := filepath.Glob(path + "*")
	if len(files) > 4 {
		t.Fatal(files)
	}
	for _, p := range files {
		info, err := os.Stat(p)
		if err != nil || info.Size() > 512 {
			t.Fatal(p, info, err)
		}
	}
}
