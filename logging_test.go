package orbit_test

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	serverlog "github.com/NumberMan1/log"
	"github.com/NumberMan1/log/field"
	"github.com/NumberMan1/orbit/service"
	"github.com/NumberMan1/orbit/socket"
	"github.com/NumberMan1/orbit/socket/tcp"
	"github.com/NumberMan1/orbit/util"
)

type logRecord struct {
	message string
	data    serverlog.M
}
type capturePrinter struct {
	mu      sync.Mutex
	records []logRecord
	closes  int
}

func (p *capturePrinter) Log(_ serverlog.Level, message string, data serverlog.M) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.records = append(p.records, logRecord{message, data})
}
func (p *capturePrinter) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closes++
}

func TestComponentsReportPanicsWithoutClosingBorrowedLogger(t *testing.T) {
	raw := serverlog.NewLogger()
	defer raw.Close()
	printer := &capturePrinter{}
	raw.AddPrinter(serverlog.LevelSwitchLte(serverlog.TraceLevel), printer)
	logger := serverlog.NewServerLogger(raw).With(field.String("instance", "test"))
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	queue, err := service.NewQueue(1, 0, nil, logger)
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer queue.Stop(ctx)
	if err := queue.Post(func() { panic("queue failure") }); err != nil {
		t.Fatal(err)
	}
	if err := queue.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	wheel, err := util.NewTimeWheel[int](start, util.WheelOptions{Tick: time.Millisecond, Slots: 2, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	if err := wheel.Schedule(1, start.Add(time.Millisecond), 0, func(time.Time) { panic("wheel failure") }); err != nil {
		t.Fatal(err)
	}
	if _, err := wheel.Advance(start.Add(time.Millisecond), 1); err != nil {
		t.Fatal(err)
	}
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	session, err := socket.NewSession(ctx, tcp.Transport{Conn: a}, socket.Options{
		Logger: logger,
		OnOpen: func(context.Context, *socket.Session) error { panic("session failure") },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Run(nil); err == nil {
		t.Fatal("session panic was not reported")
	}
	printer.mu.Lock()
	closed := printer.closes
	printer.mu.Unlock()
	if closed != 0 {
		t.Fatal("component closed the borrowed logger")
	}
	original := errors.New("original error")
	logger.Error("after component shutdown", field.Error(original))
	raw.Close()
	printer.mu.Lock()
	defer printer.mu.Unlock()
	if printer.closes != 1 || len(printer.records) != 4 {
		t.Fatal("missing logs or incorrect ownership", printer.closes, printer.records)
	}
	for i, message := range []string{"event callback panic", "timer callback panic", "session panic", "after component shutdown"} {
		r := printer.records[i]
		if r.message != message || r.data["instance"] != "test" {
			t.Fatal(r)
		}
		if i < 3 && (r.data["panic"] == nil || r.data["stack"] == "" || r.data["stack"] == nil) {
			t.Fatal("panic diagnostics lost", r)
		}
	}
	if printer.records[2].data["id"] != session.ID() || printer.records[3].data["error"] != original {
		t.Fatal("typed fields lost", printer.records)
	}
}

func TestConfiguredFileLoggerDrainsFrameworkDiagnostics(t *testing.T) {
	raw, err := serverlog.NewConfiguredLogger(serverlog.Config{Level: "info"})
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	dir := t.TempDir()
	printer, err := serverlog.NewFilePrinter(serverlog.FilePrinterConfig{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	raw.AddPrinter(serverlog.LevelSwitchLte(serverlog.InfoLevel), printer)
	logger := serverlog.NewServerLogger(raw).With(field.String("component", "worker"))
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	queue, err := service.NewQueue(1, 0, nil, logger)
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer queue.Stop(ctx)
	if err := queue.Post(func() { panic("write to file") }); err != nil {
		t.Fatal(err)
	}
	if err := queue.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	logger.Error("shutdown", field.Error(errors.New("test error")))
	raw.Close()
	if err := printer.Err(); err != nil {
		t.Fatal(err)
	}
	var records []map[string]any
	err = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			var record map[string]any
			if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
				return err
			}
			records = append(records, record)
		}
		return scanner.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0]["msg"] != "event callback panic" || records[1]["msg"] != "shutdown" || records[1]["error"] != "test error" {
		t.Fatal("async logs not drained", records)
	}
	for _, record := range records {
		if record["component"] != "worker" || record["level"] != "error" || record["date"] == nil || record["caller"] == nil {
			t.Fatal("metadata lost", record)
		}
	}
}
