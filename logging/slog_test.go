package logging

import (
	"errors"
	"log/slog"
	"testing"

	serverlog "github.com/NumberMan1/log"
)

type wheelLogRecord struct {
	level serverlog.Level
	data  serverlog.M
}

type wheelLogPrinter struct{ records []wheelLogRecord }

func (p *wheelLogPrinter) Log(level serverlog.Level, _ string, data serverlog.M) {
	p.records = append(p.records, wheelLogRecord{level, data})
}
func (*wheelLogPrinter) Close() {}

func TestSlogAdapterScopesAndLevels(t *testing.T) {
	raw := serverlog.NewLogger()
	defer raw.Close()
	printer := &wheelLogPrinter{}
	raw.AddPrinter(serverlog.LevelSwitchLte(serverlog.TraceLevel), printer)
	logger := NewSlogLogger(serverlog.NewServerLogger(raw))
	original := errors.New("original")
	scoped := logger.With("owner", "queue").WithGroup("wheel").With("id", 7)
	scoped.Error("failure", "error", original, slog.Group("nested", "value", 3), slog.Attr{})
	logger.Debug("root")
	scoped.WithGroup("").Info("info")
	scoped.Warn("warn")
	if len(printer.records) != 4 {
		t.Fatal(printer.records)
	}
	for i, level := range []serverlog.Level{serverlog.ErrorLevel, serverlog.DebugLevel, serverlog.InfoLevel, serverlog.WarnLevel} {
		if printer.records[i].level != level {
			t.Fatal("incorrect level mapping", printer.records)
		}
	}
	data := printer.records[0].data
	if data["owner"] != "queue" || data["wheel.id"] != int64(7) || data["wheel.error"] != original || data["wheel.nested.value"] != int64(3) {
		t.Fatal("scope or value lost", data)
	}
	if _, ok := data["wheel."]; ok {
		t.Fatal("empty attribute retained", data)
	}
	if len(printer.records[1].data) != 0 {
		t.Fatal("child attributes leaked into parent", printer.records[1])
	}
}
