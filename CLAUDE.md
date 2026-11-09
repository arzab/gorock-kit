# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository layout

`gorock-kit` is a **multi-module** Go repo (Go 1.25): each top-level `rock*` directory is its own module (`github.com/arzab/gorock-kit/<name>`) with its own `go.mod`/`go.sum`. There is no root `go.mod`; the committed `go.work` wires the modules together.

## Commands

Because there's no root module, `./...` from the repo root does not match anything. `scripts/check.sh` loops over modules (the `Makefile` just wraps it; `make` is not installed on this Windows machine):

```sh
bash scripts/check.sh all                        # build + vet + test, every module
bash scripts/check.sh test rockfiber rockbus     # one step, selected modules
bash scripts/check.sh tidy-check                 # fails if any go.mod/go.sum is not tidy
cd rocktelebot && go test -run TestRestart -v .  # single test
cd rockfiber && go mod tidy                      # deps are managed per module
```

`check.sh test` uses `-race` only if a race build actually works (needs cgo + a C compiler; locally it does not, CI on Linux does). Force with `RACE=1`/`RACE=0`. CI: `.github/workflows/ci.yml` (matrix per module, golangci-lint with root `.golangci.yml`).

Test seams: unexported package vars such as `errWriter` (`stderr.go`) and `rocktelebot.newBot` (swap in an offline bot with a fake poller) are overridden in tests; `rockfiber` tests call `Init` + `setupEndpoints` and use `fiber.App.Test` without listening. Each lifecycle module has `compat_test.go` asserting it still matches the `rockengine.App` signature — keep it when changing `Init/Exec/Stop`.

Releasing: all modules share one version; tag each module on the merged `main` commit, never on a feature branch (`for m in rock*; do git tag -a "$m/vX.Y.Z" -m "$m vX.Y.Z"; done; git push origin --tags`), and move the `Unreleased` section of `CHANGELOG.md` under that version.

## Architecture

- **Modules do not import each other.** Integration is structural: lifecycle components (`rockfiber`, `rockbus`, `rockcron`, `rocktelebot`) implement the `rockengine.App` interface (`Init(ctx) error / Exec(ctx) error / Stop() []error`) without depending on `rockengine`. Keep it that way — don't add cross-module `require`s; match signatures instead.
- **rockengine** (`engine.go`) is the backbone: `Init` runs sequentially in registration order (failure stops already-inited apps in reverse order), then each app's `Exec` runs in its own goroutine. App contexts derive from `context.WithoutCancel(ctx)`, so the caller's ctx does not cancel them all at once: shutdown (ctx cancel, SIGINT/SIGTERM, or an app failing while `stopOnFatal` is on, the default) stops apps **one at a time** in reverse order — cancel that app's ctx → `Stop` → wait for its `Exec` — all under the shutdown timeout. Invariants to preserve: per-entry `stopped` flag ⇒ `Stop` exactly once per successful `Init`; per-entry `lc` mutex serialises `Init`/`Stop` of one app; anything `Exec` returns after its ctx is cancelled is a clean stop; panics in Init/Exec/Stop/fatal handlers are recovered (an Exec panic follows `RestartPolicy`). Restart = Stop → Delay → Init → Exec; `ResetAfter` resets the retry counter after a long healthy run.
- **Config convention:** each module exposes a `Config`/`Configs` struct loadable by `rockconfig.InitFromFile[T]` — fields map by snake_case name; tags `config:"name,omitempty,default=…,env=VAR"`; `config:"-"` for fields that must be set in code (funcs, third-party structs). A field without `omitempty`/`default=` is **required: the key must be present** (zero values are fine; strings must be non-empty). Anything with an in-code default (`if x == 0 { x = 1024 }`) must be tagged `omitempty`, or users are forced to write it. Unknown keys are errors. Durations must be strings (`"30s"`). Env refs in files are `${VAR}` / `${VAR:-x}`, expanded after decoding.
- **Default-instance pattern:** several modules (`rockengine`, `rockbus`, `rocklog`) keep a package-level default (`default.go`) with package functions delegating to it (`rockbus.SetDefault` must be called before package-level use).
- Lifecycle components keep per-`Init` state (queues, server, scheduler, stop channel, `stopped` flag) under a mutex and recreate it in every `Init`, so Stop → Init → Exec restarts work and `Stop` is idempotent and safe before `Init`/`Exec`. Don't reintroduce `sync.Once` fields that are never reset. Configuration errors belong in `Init` (fail startup), not `Exec` (would trigger restarts).
- `rockbus` creates queues in `Init` (the engine runs all `Init`s, then all `Exec`s concurrently — other apps may publish before the bus's `Exec` starts). Invariant: every `PublishAsync` event is handled or passed to `OnError`; `PublishAsync` holds `stateMu.RLock` across the non-blocking send, `Stop` takes the write lock before closing queues.
- Wrapper modules (`rockredis`, `rockbun`) expose a `Service` interface (`interface.go`) over the underlying client plus `configs.go`/`service.go`. They are not `App`s; they match `rockengine.Resource` (`Init(ctx) error` / `Stop() error`) and are registered with `RegisterResource` — initialised before all apps, stopped after all apps. Don't add methods to exported `Service` interfaces (breaks users' mocks); add package functions like `rockredis.Client(svc)` instead. A failed `Init` closes the new pool; `Stop` keeps the closed client so later calls error instead of panicking.
- Cross-module glue without imports: `rockfiber.TraceIdFromContext(c.UserContext())` + `rocklog.WithContext(ctx, fields...)` / `rocklog.FromContext(ctx)`.

## Documentation

Every module has `README.md` and `README_RU.md` (Russian). When changing a module's public API, update both READMEs, the module's `example_test.go` (README snippets as compile-only `Example` funcs — keep them in sync, CI compiles them), and `CHANGELOG.md`. The root README example is copied from `examples/fullapp/main.go`, a separate module (in `go.work`, built by CI) that imports every library via `replace ../../rockX`; it is the only place cross-module imports are allowed.
