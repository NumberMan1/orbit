// Package logging supplies optional instance-owned sinks for standard log/slog.
package logging

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

type FileOptions struct {
	Path     string
	MaxBytes int64
	Backups  int
}

var ErrRecordTooLarge = errors.New("logging: record exceeds file size limit")

// RotatingWriter serializes writes and rotates before exceeding MaxBytes.
// Path, Path.1 ... Path.N are exclusively owned by this writer; do not combine
// with external rotation or other processes writing the same files. It never
// splits an oversized record. Close is required; use directly as a slog sink.
type RotatingWriter struct {
	mu      sync.Mutex
	options FileOptions
	file    *os.File
	size    int64
	closed  bool
}

func NewRotatingWriter(options FileOptions) (*RotatingWriter, error) {
	if options.Path == "" || options.MaxBytes <= 0 || options.Backups < 0 {
		return nil, errors.New("logging: invalid file options")
	}
	if err := os.MkdirAll(filepath.Dir(options.Path), 0700); err != nil {
		return nil, err
	}
	w := &RotatingWriter{options: options}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}
func (w *RotatingWriter) open() error {
	f, err := os.OpenFile(w.options.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	stat, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	w.file = f
	w.size = stat.Size()
	return nil
}
func (w *RotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, os.ErrClosed
	}
	if int64(len(p)) > w.options.MaxBytes {
		return 0, ErrRecordTooLarge
	}
	if len(p) == 0 {
		return 0, nil
	}
	if w.file == nil {
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	if w.size > w.options.MaxBytes-int64(len(p)) {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	return n, err
}
func (w *RotatingWriter) rotate() error {
	err := w.file.Close()
	w.file = nil
	if err != nil {
		return err
	}
	path := w.options.Path
	if w.options.Backups == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	} else {
		if err := os.Remove(path + "." + strconv.Itoa(w.options.Backups)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		for i := w.options.Backups - 1; i >= 1; i-- {
			if err := os.Rename(path+"."+strconv.Itoa(i), path+"."+strconv.Itoa(i+1)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		if err := os.Rename(path, path+".1"); err != nil {
			return err
		}
	}
	return w.open()
}
func (w *RotatingWriter) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || w.file == nil {
		return os.ErrClosed
	}
	return w.file.Sync()
}
func (w *RotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	if w.file != nil {
		return w.file.Close()
	}
	return nil
}
