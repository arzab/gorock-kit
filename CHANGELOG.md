# Changelog

All modules are versioned together. Tags are per module (`rockengine/v0.1.0`, `rockfiber/v0.1.0`, …), as Go requires for a multi-module repository.

## Unreleased

_Nothing yet._

## v0.1.0 — 2026-11-09

First tagged release. Everything before it was untagged (pseudo-versions).

### ⚠️ Changes that need action

| Module | Change | What to do |
|---|---|---|
| rockengine | When an app fails permanently (restart limit exceeded, re-init failed), the **engine now shuts down** and `Run` returns an error naming the app. Previously the process kept running without it. | Nothing, if a crashed component should restart the process. To keep the old behaviour: `SetStopOnFatal(false)` / `WithStopOnFatal(false)`. |
| rockconfig | Env references are `${VAR}` / `${VAR:-default}` only; `$VAR` without braces is no longer expanded. An unset `${VAR}` is an error. | Replace `$VAR` with `${VAR}`; add `:-default` where a variable is optional. |
| rockconfig | Unknown keys are an error. | Fix typos, or pass `rockconfig.AllowUnknownKeys()` for shared files. |
| rockconfig | A required field must be **present**; zero values (`0`, `false`) are now valid, and a required string must not be empty. | Fields that were silently treated as missing may now be accepted — and required bools are enforced. Add `omitempty` to fields with in-code defaults. |
| rockconfig | Numeric durations are rejected (`timeout: 30` used to mean 30ns). | Write `timeout: 30s`. |
| rockfiber | pprof is **off by default** and, when enabled, behind the admin password. | Set `Pprof: true` if you use `/admin/debug/pprof`. |
| rockfiber | `CorsConfig: nil` no longer allows all origins; no CORS headers are sent. | Browser front-ends on another origin: set `CorsConfig` explicitly. |
| rockfiber | `?debug=` no longer reveals masked 500 messages. | Read the real message in `OnError` (it now receives it unmasked). |
| rockbus | New topics must be subscribed **before Init** (was: before Exec). `Subscribe` returns an `error`. | Subscribe via `NewApp(..., rockbus.On(...))` or before `engine.Run()`; handle the returned error. |
| rocklog | Backend is `log/slog` instead of logrus. Level names and field semantics are unchanged; text output prints `msg` before fields and timestamps use slog's format. | Only log parsers that depend on the exact text layout are affected. |

### Added

- **rockengine:** `Resource` + `RegisterResource` — DB/cache clients are initialised before all apps and closed after them. `RestartPolicy.ResetAfter`. `WithStopOnFatal` / `SetStopOnFatal`.
- **rockfiber:** `New(cfg, endpoints...)` (as documented; `New(cfg)` still works). `Config.Pprof`. `TraceIdFromContext(ctx)` — the trace ID is also stored in `c.UserContext()`.
- **rocklog:** `WithContext`, `ContextWithLogger`, `FromContext`; `Any` field constructor.
- **rockconfig:** `AllowUnknownKeys()`, `ErrMissingFields`, `ErrUnknownKeys`; `${VAR:-default}`.
- **rockredis:** `Client(svc)` — the underlying `*redis.Client` for commands `Service` does not cover.
- `examples/fullapp` — a runnable service using every module; the root README example is taken from it.

### Fixed

- **rockengine:** a normal shutdown could be reported as a fatal failure when `Exec` returned e.g. `http.ErrServerClosed`. `Stop` was often never called on shutdown, so resources were not released. A panic in `Exec` bypassed the restart policy. Panics in re-init, `Stop` and fatal handlers crashed the process. The shutdown timeout did not cover `Stop`, so one hanging app hung the process. Apps were not stopped one at a time despite the documented reverse order.
- **rocktelebot:** shutdown hung forever when ctx cancellation and `Stop` happened together (always, under rockengine).
- **rockfiber:** pprof was reachable without the admin password. After a restart the new server was never shut down.
- **rockbus:** events published during `Stop` could be lost without reaching `OnError`. Events published by other apps right after startup failed with `ErrNoWorker`. `QueueSize` was a required config field despite its documented default.
- **rockcron:** invalid schedules or locations were reported at `Exec` (triggering restarts) instead of `Init`; `Stop` did not cancel running jobs' context.
- **rockbun / rockredis:** a failed `Init` leaked the connection pool. `rockbun` did not escape credentials in the DSN (`@ : / #` in passwords broke it). `rockredis.SetNX` used a deprecated go-redis call.
- **rockconfig:** `os.ExpandEnv` on the raw file corrupted values containing `$` and let env values inject YAML keys. Defaults and env bindings inside a missing section were ignored. Integer overflows were silently truncated.
- **Docs:** README examples that did not compile (`rockengine.New`, `rockbus.Config.OnError`, `svc.Close`, `rocklog.Any`). All examples are now compiled as `example_test.go` in CI.

### Tooling

- `go.work` is committed; `scripts/check.sh` (build / vet / test with `-race` / tidy-check / lint) over all modules; GitHub Actions matrix per module with golangci-lint.
