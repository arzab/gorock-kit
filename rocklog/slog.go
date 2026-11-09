package rocklog

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// levelFatal is a custom slog level above Error.
const levelFatal = slog.Level(12)

// exit is called after a Fatal entry is written. Overridable in tests.
var exit = os.Exit

// slogLogger is the default Logger backend, built on log/slog.
//
// Fields are kept in the logger (not pre-bound into the handler) so that
// With/Named replace an existing key instead of emitting it twice, matching
// the documented semantics.
type slogLogger struct {
	handler    slog.Handler
	cfg        Config
	fields     []Field // unique keys, in first-seen order
	name       string  // "logger" field set by Named
	callerSkip int     // extra frames to skip when reporting caller location
}

// newSlogLogger creates a slog-backed Logger.
// callerSkip is the number of extra call frames to skip when resolving file:line.
// Use 0 for direct Logger usage, 1 when called through package-level functions.
func newSlogLogger(cfg Config, callerSkip int) *slogLogger {
	out := cfg.Output
	if out == nil {
		out = os.Stdout
	}
	opts := &slog.HandlerOptions{
		Level:       toSlogLevel(cfg.Level),
		ReplaceAttr: replaceAttr(cfg.TimeFormat),
	}

	var h slog.Handler
	if cfg.Format == FormatJSON {
		h = slog.NewJSONHandler(out, opts)
	} else {
		h = slog.NewTextHandler(out, opts)
	}
	return &slogLogger{handler: h, cfg: cfg, callerSkip: callerSkip}
}

// replaceAttr keeps the level names of the previous logrus backend
// (debug, info, warning, error, fatal) and applies a custom time format.
func replaceAttr(timeFormat string) func(groups []string, a slog.Attr) slog.Attr {
	return func(groups []string, a slog.Attr) slog.Attr {
		if len(groups) > 0 {
			return a
		}
		switch a.Key {
		case slog.LevelKey:
			if lvl, ok := a.Value.Any().(slog.Level); ok {
				a.Value = slog.StringValue(levelName(lvl))
			}
		case slog.TimeKey:
			if timeFormat != "" {
				if t, ok := a.Value.Any().(time.Time); ok {
					a.Value = slog.StringValue(t.Format(timeFormat))
				}
			}
		}
		return a
	}
}

func levelName(l slog.Level) string {
	switch {
	case l >= levelFatal:
		return "fatal"
	case l >= slog.LevelError:
		return "error"
	case l >= slog.LevelWarn:
		return "warning"
	case l >= slog.LevelInfo:
		return "info"
	default:
		return "debug"
	}
}

// log is the single dispatch point for all levels.
func (l *slogLogger) log(level slog.Level, msg string, fields []Field) {
	ctx := context.Background()
	if !l.handler.Enabled(ctx, level) {
		if level >= levelFatal {
			exit(1)
		}
		return
	}

	r := slog.NewRecord(time.Now(), level, msg, 0)
	r.AddAttrs(l.attrs(fields)...)
	if l.cfg.Caller {
		// 2 = skip log() itself + the Debug/Info/Warn/Error/Fatal method above it.
		if _, file, line, ok := runtime.Caller(2 + l.callerSkip); ok {
			r.AddAttrs(slog.String("caller", fmt.Sprintf("%s:%d", filepath.Base(file), line)))
		}
	}
	_ = l.handler.Handle(ctx, r)

	if level >= levelFatal {
		exit(1)
	}
}

// attrs merges logger fields, the logger name and call fields.
// A later value for the same key replaces an earlier one.
func (l *slogLogger) attrs(call []Field) []slog.Attr {
	all := make([]Field, 0, len(l.fields)+len(call)+1)
	all = append(all, l.fields...)
	if l.name != "" {
		all = append(all, Field{Key: "logger", Value: l.name})
	}
	all = append(all, call...)

	merged := mergeFields(nil, all)
	out := make([]slog.Attr, len(merged))
	for i, f := range merged {
		out[i] = slog.Any(f.Key, f.Value)
	}
	return out
}

// mergeFields appends add to base; a key already present is replaced in place.
func mergeFields(base, add []Field) []Field {
	out := append([]Field(nil), base...)
	for _, f := range add {
		replaced := false
		for i := range out {
			if out[i].Key == f.Key {
				out[i] = f
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, f)
		}
	}
	return out
}

func (l *slogLogger) IsEnabled(lvl Level) bool {
	return l.handler.Enabled(context.Background(), toSlogLevel(lvl))
}

func (l *slogLogger) Debug(msg string, fields ...Field) { l.log(slog.LevelDebug, msg, fields) }
func (l *slogLogger) Info(msg string, fields ...Field)  { l.log(slog.LevelInfo, msg, fields) }
func (l *slogLogger) Warn(msg string, fields ...Field)  { l.log(slog.LevelWarn, msg, fields) }
func (l *slogLogger) Error(msg string, fields ...Field) { l.log(slog.LevelError, msg, fields) }
func (l *slogLogger) Fatal(msg string, fields ...Field) { l.log(levelFatal, msg, fields) }

func (l *slogLogger) With(fields ...Field) Logger {
	return &slogLogger{
		handler: l.handler,
		cfg:     l.cfg,
		fields:  mergeFields(l.fields, fields),
		name:    l.name,
		// callerSkip resets to 0: the returned logger is always used directly,
		// never through an extra package-level wrapper function.
		callerSkip: 0,
	}
}

func (l *slogLogger) Named(name string) Logger {
	return &slogLogger{
		handler:    l.handler,
		cfg:        l.cfg,
		fields:     l.fields,
		name:       name,
		callerSkip: 0, // same reasoning as With
	}
}

func toSlogLevel(lvl Level) slog.Level {
	switch lvl {
	case LevelDebug:
		return slog.LevelDebug
	case LevelWarn:
		return slog.LevelWarn
	case LevelError:
		return slog.LevelError
	case LevelFatal:
		return levelFatal
	default: // LevelInfo or zero value (unset Config{})
		return slog.LevelInfo
	}
}
