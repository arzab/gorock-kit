# rocklog

Structured logging for Go with a clean interface, logrus backend, and global or instance-based usage.

## Overview

`rocklog` wraps any logging backend behind a `Logger` interface. The default backend is [logrus](https://github.com/sirupsen/logrus). Logs are always structured with typed fields — no format strings required. The log level is always present in the output automatically.

## Quick Start

```go
// Global logger — ready to use with no setup.
rocklog.Info("server started", rocklog.Int("port", 8080))
rocklog.Error("request failed", rocklog.Err(err), rocklog.Str("path", "/api/v1"))

// Configure the global logger.
rocklog.Init(rocklog.Config{
    Level:      rocklog.LevelInfo,
    Format:     rocklog.FormatJSON,
    TimeFormat: time.RFC3339,
})
```

## Config

```go
type Config struct {
    Level      Level     // LevelDebug/Info/Warn/Error/Fatal; zero value = LevelInfo
    Format     Format    // FormatText (default) or FormatJSON
    TimeFormat string    // e.g. time.RFC3339, "2006-01-02 15:04:05"; empty = backend default
    Output     io.Writer // defaults to os.Stdout
    Caller     bool      // include file:line in every log entry
}
```

### Levels

| Constant     | When to use                            |
|--------------|----------------------------------------|
| `LevelDebug` | Detailed internal state, dev only      |
| `LevelInfo`  | Normal operational events (default)    |
| `LevelWarn`  | Unexpected but recoverable situations  |
| `LevelError` | Errors that need attention             |
| `LevelFatal` | Unrecoverable error — calls `os.Exit(1)` |

### Formats

| Constant     | Output                        | Typical use          |
|--------------|-------------------------------|----------------------|
| `FormatText` | Human-readable colored output | Local development    |
| `FormatJSON` | Structured JSON per line      | Production, GCP, etc |

## Instance Logger

Use `New` when you need an independent logger (e.g. per component, per test):

```go
log := rocklog.New(rocklog.Config{
    Level:  rocklog.LevelDebug,
    Format: rocklog.FormatJSON,
})
log.Info("hello")
```

## Global Logger

The package ships a default logger (Info level, text format, stdout). All package-level functions delegate to it:

```go
rocklog.Info("hello")
rocklog.Debug("query", rocklog.Str("sql", query))
```

Replace the default logger at startup:

```go
rocklog.Init(rocklog.Config{
    Level:      rocklog.LevelInfo,
    Format:     rocklog.FormatJSON,
    TimeFormat: time.RFC3339,
    Caller:     true,
})
```

Plug in any custom `Logger` implementation:

```go
rocklog.SetDefault(myZapAdapter)
```

> **Note:** if you use `SetDefault(rocklog.New(cfg))` with `Caller: true`, the reported call site will be inside `rocklog` rather than your code. Use `Init(cfg)` instead when you want the default logrus backend with caller reporting.

## Structured Fields

Every log method accepts zero or more `Field` values. Use the provided helpers:

| Helper                         | Description                          |
|--------------------------------|--------------------------------------|
| `F(key, val)`                  | Any value                            |
| `Err(err)`                     | Error under key `"error"`            |
| `Str(key, val)`                | String                               |
| `Int(key, val)`                | int                                  |
| `Int64(key, val)`              | int64                                |
| `Float64(key, val)`            | float64                              |
| `Bool(key, val)`               | bool                                 |
| `Dur(key, val)`                | `time.Duration` as `"1m30s"` string  |
| `Time(key, val)`               | `time.Time`                          |
| `Stringer(key, val)`           | Any `fmt.Stringer`                   |

```go
log.Info("payment processed",
    rocklog.Str("currency", "USD"),
    rocklog.Int("amount_cents", 9900),
    rocklog.Dur("latency", time.Since(start)),
    rocklog.F("metadata", map[string]any{"source": "stripe"}),
)
```

### Logging structs and maps

Pass any struct or map directly via `F`:

```go
log.Info("user created", rocklog.F("user", userStruct))
log.Warn("rate limited", rocklog.F("headers", headersMap))
```

In JSON format, nested objects are serialised recursively — useful for GCP Cloud Logging, Datadog, and similar systems that index structured fields.

## Contextual Loggers

### With — attach permanent fields

`With` returns a new logger with the given fields attached to every subsequent entry:

```go
log := rocklog.New(cfg).With(
    rocklog.Str("service", "payments"),
    rocklog.Str("env", "prod"),
)
log.Info("charge created", rocklog.Int("amount", 100))
// → level=info service=payments env=prod amount=100 msg="charge created"
```

Chain `With` calls to add more fields:

```go
reqLog := log.With(rocklog.Str("request_id", rid))
reqLog.Info("handler started")
reqLog.Error("handler failed", rocklog.Err(err))
```

### Named — tag logs by component

```go
db := rocklog.Named("database")
db.Info("query executed", rocklog.Dur("took", d))
// → level=info logger=database took=2ms msg="query executed"
```

> Calling `Named` again on a named logger overwrites the previous name.

## Level Checking

For expensive log-message construction, check whether the level is active first:

```go
if rocklog.IsEnabled(rocklog.LevelDebug) {
    rocklog.Debug("state dump", rocklog.F("state", computeExpensiveState()))
}
```

## Custom Backend

Implement the `Logger` interface to use any logging library:

```go
type Logger interface {
    Debug(msg string, fields ...Field)
    Info(msg string, fields ...Field)
    Warn(msg string, fields ...Field)
    Error(msg string, fields ...Field)
    Fatal(msg string, fields ...Field)

    IsEnabled(lvl Level) bool
    With(fields ...Field) Logger
    Named(name string) Logger
}
```

Then register it as the default:

```go
rocklog.SetDefault(&myZapLogger{})
```

## Caller Reporting

Enable `Caller: true` in `Config` to include `file:line` in every entry:

```go
rocklog.Init(rocklog.Config{Caller: true})
rocklog.Info("hello")
// → caller=main.go:42 level=info msg=hello
```

Loggers created with `New` and loggers returned by `With` / `Named` always report the correct call site. The global logger (`Init`) also reports correctly.

## Fatal Behaviour

`Fatal` logs the message and then calls `os.Exit(1)`. Deferred functions are **not** executed. Use it only for truly unrecoverable startup failures.

## Limitations

- **`Err(nil)`** is logged as `"error": null`, not omitted. Guard it yourself: `if err != nil { fields = append(fields, rocklog.Err(err)) }`.
- **`Stringer` with a nil-pointer receiver** may panic if the underlying `String()` method does not handle nil. Avoid passing nil-pointer values wrapped in a non-nil interface.
- **`SetDefault` with a custom Logger and `Caller: true`** will report `rocklog/default.go` as the call site. Use `Init` or implement caller-skip inside your adapter.
- **Embedded logrus** — concurrent calls to `Init` / `SetDefault` while logging is happening are safe (protected by `sync.RWMutex`), but should be avoided in production.
