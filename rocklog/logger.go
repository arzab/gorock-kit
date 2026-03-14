package rocklog

import "io"

// Level represents the log severity.
type Level int

const (
	// Explicit values starting at 1 so that the zero value of Config{} maps to
	// LevelInfo (the most common default) rather than LevelDebug.
	LevelDebug Level = iota + 1
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
)

// Format represents the log output format.
type Format int

const (
	FormatText Format = iota // human-readable, for development
	FormatJSON               // structured JSON, for production
)

// Config holds the configuration for a Logger instance.
type Config struct {
	Level      Level
	Format     Format
	TimeFormat string    // e.g. time.RFC3339, "2006-01-02 15:04:05"; empty = backend default
	Output     io.Writer // defaults to os.Stdout
	Caller     bool      // include file:line in every log entry
}

// Logger is the core logging interface. All backends must implement it.
type Logger interface {
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, fields ...Field)
	// Fatal logs at Fatal level and then calls os.Exit(1).
	// Deferred functions are NOT executed.
	Fatal(msg string, fields ...Field)

	// IsEnabled reports whether the given level is active.
	// Use this to guard expensive log-message construction:
	//   if log.IsEnabled(LevelDebug) { log.Debug("stats: " + compute()) }
	IsEnabled(lvl Level) bool

	// With returns a new Logger with the given fields attached to every entry.
	With(fields ...Field) Logger

	// Named returns a new Logger with a "logger" field set to name.
	// Calling Named again on the returned logger overwrites the previous name.
	Named(name string) Logger
}

// New creates a Logger backed by logrus with the given configuration.
func New(cfg Config) Logger {
	return newLogrusLogger(cfg, 0)
}
