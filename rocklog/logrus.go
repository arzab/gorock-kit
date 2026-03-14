package rocklog

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/sirupsen/logrus"
)

type logrusLogger struct {
	entry      *logrus.Entry
	cfg        Config
	callerSkip int // extra frames to skip when reporting caller location
}

// newLogrusLogger creates a logrus-backed Logger.
// callerSkip is the number of extra call frames to skip when resolving file:line.
// Use 0 for direct Logger usage, 1 when called through package-level functions.
func newLogrusLogger(cfg Config, callerSkip int) *logrusLogger {
	l := logrus.New()

	if cfg.Output != nil {
		l.SetOutput(cfg.Output)
	} else {
		l.SetOutput(os.Stdout)
	}

	l.SetLevel(toLogrusLevel(cfg.Level))

	switch cfg.Format {
	case FormatJSON:
		f := &logrus.JSONFormatter{}
		if cfg.TimeFormat != "" {
			f.TimestampFormat = cfg.TimeFormat
		}
		l.SetFormatter(f)
	default: // FormatText
		f := &logrus.TextFormatter{FullTimestamp: true}
		if cfg.TimeFormat != "" {
			f.TimestampFormat = cfg.TimeFormat
		}
		l.SetFormatter(f)
	}

	return &logrusLogger{
		entry:      logrus.NewEntry(l),
		cfg:        cfg,
		callerSkip: callerSkip,
	}
}

// log is the single dispatch point for all levels.
func (l *logrusLogger) log(level logrus.Level, msg string, fields []Field) {
	// Avoid allocating a new Entry when there are no extra fields.
	entry := l.entry
	if len(fields) > 0 {
		entry = l.entry.WithFields(toLogrusFields(fields))
	}
	if l.cfg.Caller {
		// 2 = skip log() itself + the Debug/Info/Warn/Error/Fatal method above it.
		if _, file, line, ok := runtime.Caller(2 + l.callerSkip); ok {
			entry = entry.WithField("caller", fmt.Sprintf("%s:%d", filepath.Base(file), line))
		}
	}
	entry.Log(level, msg)
}

func (l *logrusLogger) IsEnabled(lvl Level) bool {
	return l.entry.Logger.IsLevelEnabled(toLogrusLevel(lvl))
}

func (l *logrusLogger) Debug(msg string, fields ...Field) { l.log(logrus.DebugLevel, msg, fields) }
func (l *logrusLogger) Info(msg string, fields ...Field)  { l.log(logrus.InfoLevel, msg, fields) }
func (l *logrusLogger) Warn(msg string, fields ...Field)  { l.log(logrus.WarnLevel, msg, fields) }
func (l *logrusLogger) Error(msg string, fields ...Field) { l.log(logrus.ErrorLevel, msg, fields) }
func (l *logrusLogger) Fatal(msg string, fields ...Field) { l.log(logrus.FatalLevel, msg, fields) }

func (l *logrusLogger) With(fields ...Field) Logger {
	return &logrusLogger{
		entry: l.entry.WithFields(toLogrusFields(fields)),
		cfg:   l.cfg,
		// callerSkip resets to 0: the returned logger is always used directly,
		// never through an extra package-level wrapper function.
		callerSkip: 0,
	}
}

func (l *logrusLogger) Named(name string) Logger {
	return &logrusLogger{
		entry:      l.entry.WithField("logger", name),
		cfg:        l.cfg,
		callerSkip: 0, // same reasoning as With
	}
}

func toLogrusLevel(lvl Level) logrus.Level {
	switch lvl {
	case LevelDebug:
		return logrus.DebugLevel
	case LevelWarn:
		return logrus.WarnLevel
	case LevelError:
		return logrus.ErrorLevel
	case LevelFatal:
		return logrus.FatalLevel
	default: // LevelInfo or zero value (unset Config{})
		return logrus.InfoLevel
	}
}

func toLogrusFields(fields []Field) logrus.Fields {
	lf := make(logrus.Fields, len(fields))
	for _, f := range fields {
		lf[f.Key] = f.Value
	}
	return lf
}
