package util

import (
	"context"
	"log/slog"

	serverlog "github.com/NumberMan1/log"
	"github.com/NumberMan1/log/field"
)

// wheelLogHandler forwards upstream diagnostics without serializing error/panic
// values or changing global slog state. Groups qualify keys with a dot.
type wheelLogHandler struct {
	logger serverlog.Logger
	prefix string
}

func (h wheelLogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h wheelLogHandler) Handle(_ context.Context, record slog.Record) error {
	fields := make([]field.Field, 0, record.NumAttrs())
	record.Attrs(func(attr slog.Attr) bool {
		fields = appendWheelAttr(fields, h.prefix, attr)
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

func (h wheelLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	var fields []field.Field
	for _, attr := range attrs {
		fields = appendWheelAttr(fields, h.prefix, attr)
	}
	h.logger = h.logger.With(fields...)
	return h
}

func (h wheelLogHandler) WithGroup(name string) slog.Handler {
	if name != "" {
		h.prefix += name + "."
	}
	return h
}

func appendWheelAttr(fields []field.Field, prefix string, attr slog.Attr) []field.Field {
	attr.Value = attr.Value.Resolve()
	if attr.Equal(slog.Attr{}) {
		return fields
	}
	if attr.Value.Kind() == slog.KindGroup {
		if attr.Key != "" {
			prefix += attr.Key + "."
		}
		for _, child := range attr.Value.Group() {
			fields = appendWheelAttr(fields, prefix, child)
		}
		return fields
	}
	return append(fields, field.Any(prefix+attr.Key, attr.Value.Any()))
}
