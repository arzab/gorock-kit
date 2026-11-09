# Changelog

All modules are versioned together. Tags are per module (`rockengine/v0.1.0`, `rockfiber/v0.1.0`, …), as Go requires for a multi-module repository.

## Unreleased

### Added

- **rockhandlers** — transport-independent request handling: `Ctx[P, R]` (a `context.Context` with typed `Params`/`Result`, `Next`, `Route`), `Chain` → `Route` (generics erased; `NewParams`/`Serve`/`With`/`ParamsType`/`ResultType` for transports), `Prepend` for group functions, `Middleware` over `Context`, `Validator`, typed `Local[T]` (`Get`/`MustGet`), `Timeout`, `Run` for tests; error kinds incl. `Canceled`/`Timeout`, `Fields`, `PanicError`. Will replace rockdelivery once rockhttp moves over.
- **rockdelivery** — the delivery contract: `Handler[In, Out]` (`func(ctx, *In) (*Out, error)`), `Bind` → `Action` (`NewInput`, `Call` with validation and panic recovery), error kinds `Invalid`/`Unauthorized`/`Forbidden`/`NotFound`/`Conflict`/`Unavailable`/Internal with `KindOf`/`MessageOf`, `Validator`, transport-independent `Middleware`, `Timeout`; `Pipeline` + `Codec` (Decode / Encode / EncodeError) that runs every request the same way for any transport, with `Failure` (Internal messages hidden), `OnFailure`, `Observe` and panic containment; `Route` with typed metadata (`NewMetaKey`, `WithMeta`, `Meta`). No transport.
- **rockhttp** (experimental, may move to gorock-catalog) — HTTP transport for rockdelivery on `net/http`, no dependencies: `GET`/`POST`/`PUT`/`PATCH`/`DELETE` endpoints with `Status`/`Use`/`Name`; binding from `path`/`query`/`header`/`form` tags, JSON body and multipart files; `File` (download, ranges) and `Redirect` results; `SetHeader`/`SetCookie`/`Request` for the rest; error kinds → HTTP status with a JSON `ErrorBody`; `App` implements `rockengine.App` (graceful shutdown, prefix, body limit, net/http middlewares).
- **rockservices** — the services layer: `Catalog` with `Add(c, ctor, cfg)`, `Get[T](name)`, `Init`, `Stop` (reverse order, timeout), a default catalog, `New()` and `SetDefault` for tests. A service implements `Name() string`, `Init(ctx) error`, `Stop() error`.

### Removed

- **rockengine:** `Resource`, `RegisterResource`, `MustRegisterResource`. The engine runs apps only; services are started before it and stopped after it by `rockservices` in `cmd/main`.

### ⚠️ Changes that need action

The kit is now only its core: **`rockengine`** and **`rockconfig`**. The other modules were moved, as-is, to the sibling repository `gorock-catalog` (a separate repository, not published yet) (module paths `github.com/arzab/gorock-catalog/<module>`), where they will be reshaped into a catalog of components you copy into your project, with an HTTP app on `net/http` instead of Fiber.

| Removed module | Where |
|---|---|
| `rockfiber`, `rockbus`, `rockcron`, `rocktelebot`, `rockbun`, `rockredis`, `rocklog` | gorock-catalog; their last kit versions, `v0.2.0`, stay available here |

`examples/fullapp` was removed with them.

## v0.2.0 — 2026-11-09

Security release: dependency updates for published advisories.

### ⚠️ Changes that need action

| Module | Change | What to do |
|---|---|---|
| all | **Minimum Go is 1.26** (was 1.25). Fixed versions of `golang.org/x/net` (≥ v0.62) and `golang.org/x/crypto` (≥ v0.56) require it, and Go 1.25 no longer receives security fixes. | Build with Go 1.26 or 1.27, preferably the latest patch release — several advisories are in the standard library (`crypto/tls`, `crypto/x509`, `net/http`) and are fixed only by the Go toolchain. |

### Security

- **rockfiber:** `github.com/andybalholm/brotli` v1.1.0 → v1.2.x (GO-2026-6888, excessive memory allocation on crafted streams). This one is reachable: it affects servers with `Compress: true`.
- **rockfiber:** `gofiber/fiber/v2` → v2.52.16, `valyala/fasthttp`, `klauspost/compress`, `golang.org/x/net`, `golang.org/x/text`, `golang.org/x/sys` to fixed versions.
- **rockbun:** `golang.org/x/crypto`, `go.opentelemetry.io/otel`, `uptrace/bun` to fixed versions.
- Other modules: dependencies updated to their latest releases.

`govulncheck` reports no vulnerabilities in any module with Go 1.26.10. Apart from brotli, the advisories were in dependency code that the kit does not call (e.g. the SSH packages of `x/crypto`), but they triggered alerts for every user.

### Fixed

- **rockconfig, rockfiber:** deprecated `reflect.Ptr` replaced by `reflect.Pointer`.

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
