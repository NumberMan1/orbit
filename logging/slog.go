// Package logging adapts structured loggers for Orbit integrations.
package logging

import (
	"context"
	"log/slog"

	serverlog "github.com/NumberMan1/log"
	"github.com/NumberMan1/log/field"
)

// NewSlogLogger forwards slog records to a borrowed NumberMan1/log Logger.
// A nil logger captures the current serverlog.DefaultLogger() at construction.
// It preserves error and panic values, qualifies grouped keys with dots, and
// neither changes the global slog logger nor closes the borrowed log core.
// Concurrent use requires the supplied Logger to support concurrent calls.
func NewSlogLogger(logger serverlog.Logger) *slog.Logger {
	if logger == nil {
		logger = serverlog.DefaultLogger()
	}
	return slog.New(slogHandler{logger: logger})
}

// slogHandler forwards upstream diagnostics without serializing error/panic
// values or changing global slog state. Groups qualify keys with a dot.
type slogHandler struct {
	logger serverlog.Logger
	prefix string
}

func (h slogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h slogHandler) Handle(_ context.Context, record slog.Record) error {
	fields := make([]field.Field, 0, record.NumAttrs())
	record.Attrs(func(attr slog.Attr) bool {
		fields = appendAttr(fields, h.prefix, attr)
		return true
	})
	switch {
	case record.Level >= slog.LevelError:
		h.logger.Error(record.Message, fields...)
	case record.Level >= slog.LevelWarn:
		h.logger.Warn(record.Message, fields...)
	case record.Level >= slog.LevelInfo:
		h.logger.Info(record.Message, fields...)
	default:
		h.logger.Debug(record.Message, fields...)
	}
	return nil
}

func (h slogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	var fields []field.Field
	for _, attr := range attrs {
		fields = appendAttr(fields, h.prefix, attr)
	}
	h.logger = h.logger.With(fields...)
	return h
}

func (h slogHandler) WithGroup(name string) slog.Handler {
	if name != "" {
		h.prefix += name + "."
	}
	return h
}

func appendAttr(fields []field.Field, prefix string, attr slog.Attr) []field.Field {
	attr.Value = attr.Value.Resolve()
	if attr.Equal(slog.Attr{}) {
		return fields
	}
	if attr.Value.Kind() == slog.KindGroup {
		if attr.Key != "" {
			prefix += attr.Key + "."
		}
		for _, child := range attr.Value.Group() {
			fields = appendAttr(fields, prefix, child)
		}
		return fields
	}
	return append(fields, field.Any(prefix+attr.Key, attr.Value.Any()))
}
