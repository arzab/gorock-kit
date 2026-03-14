package rocklog

import (
	"os"
	"sync"
)

var (
	defaultMu     sync.RWMutex
	defaultLogger Logger = newLogrusLogger(Config{
		Level:  LevelInfo,
		Format: FormatText,
		Output: os.Stdout,
	}, 1) // callerSkip=1 to account for the extra package-level function frame
)

// Init replaces the default logger with a new logrus-backed Logger.
// Safe to call concurrently, but should typically be called once at startup.
func Init(cfg Config) {
	l := newLogrusLogger(cfg, 1)
	defaultMu.Lock()
	defaultLogger = l
	defaultMu.Unlock()
}

// SetDefault replaces the default logger with any Logger implementation.
// Note: if the provided Logger is backed by logrus and has Caller enabled,
// use Init instead — New() sets callerSkip=0 which will report this package
// as the call site rather than user code.
func SetDefault(l Logger) {
	defaultMu.Lock()
	defaultLogger = l
	defaultMu.Unlock()
}

// Default returns the current default logger.
func Default() Logger {
	defaultMu.RLock()
	defer defaultMu.RUnlock()
	return defaultLogger
}

func Debug(msg string, fields ...Field) { Default().Debug(msg, fields...) }
func Info(msg string, fields ...Field)  { Default().Info(msg, fields...) }
func Warn(msg string, fields ...Field)  { Default().Warn(msg, fields...) }
func Error(msg string, fields ...Field) { Default().Error(msg, fields...) }
func Fatal(msg string, fields ...Field) { Default().Fatal(msg, fields...) }

// IsEnabled reports whether the given level is active on the default logger.
func IsEnabled(lvl Level) bool { return Default().IsEnabled(lvl) }

// With returns a child of the default logger with the given fields attached.
func With(fields ...Field) Logger { return Default().With(fields...) }

// Named returns a child of the default logger with the "logger" field set to name.
func Named(name string) Logger { return Default().Named(name) }
