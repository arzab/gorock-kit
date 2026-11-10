# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository layout

`gorock-kit` is the **core** of GOROCK: Go modules `rockengine` (apps lifecycle), `rockconfig` (config loading), `rockservices` (services layer) and `rockhandlers` (transport-independent request handling), each with its own `go.mod` (`github.com/arzab/gorock-kit/<name>`, Go 1.26 minimum; keep the `go` directives in sync). There is no root `go.mod`; the committed `go.work` wires them together.

Everything that is not core — the HTTP transport `rockhttp` (built on rockhandlers), the Fiber app, PostgreSQL/Redis services, logger, event bus, scheduler, Telegram bot — was moved to the sibling repo `../gorock-catalog` (module paths `github.com/arzab/gorock-catalog/<name>`). Don't add such modules back here; the kit keeps only structural pieces every service needs. The removed modules' last versions here are the `*/v0.2.0` tags.

## Commands

Because there's no root module, `./...` from the repo root does not match anything. `scripts/check.sh` loops over modules (the `Makefile` just wraps it; `make` is not installed on this Windows machine):

```sh
bash scripts/check.sh all                        # build + vet + test, every module
bash scripts/check.sh test rockengine            # one step, selected modules
bash scripts/check.sh tidy-check                 # fails if any go.mod/go.sum is not tidy
cd rockengine && go test -run TestOrderedShutdown -v .  # single test
```

`check.sh test` uses `-race` only if a race build actually works (needs cgo + a C compiler; locally it does not, CI on Linux does). Force with `RACE=1`/`RACE=0`. CI: `.github/workflows/ci.yml` (matrix per module, golangci-lint with root `.golangci.yml`).

Dependency updates: `go get -u` silently raises a module's `go` directive when a new dependency needs a newer Go — check the `go` lines in the diff; that is a breaking change for users. golangci-lint refuses modules whose `go` version is newer than the Go it was built with, so bump the lint version in `ci.yml` together with the Go version. Check reachability with `govulncheck ./...` per module (on the latest Go patch, e.g. in `golang:1.26` Docker), not only Dependabot.

Releasing: all modules share one version; tag each module on the merged `main` commit, never on a feature branch (`for m in rock*; do git tag -a "$m/vX.Y.Z" -m "$m vX.Y.Z"; done; git push origin --tags`), and move the `Unreleased` section of `CHANGELOG.md` under that version.

## Architecture

- **The modules do not import each other.** Components elsewhere (gorock-catalog, user code) implement `rockengine.App` (`Init(ctx) error / Exec(ctx) error / Stop() []error`) and `rockservices.Service` (`Init(ctx) error / Stop() error`) by signature — these are the public contracts; changing them breaks every component.
- **rockengine** (`engine.go`) runs **apps only** — there may be several, and it manages their lifecycle, not the whole program's. Apps' `Init` runs sequentially, then each app's `Exec` in its own goroutine. App contexts derive from `context.WithoutCancel(ctx)`, so the caller's ctx does not cancel them all at once: shutdown (ctx cancel, SIGINT/SIGTERM, or an app failing while `stopOnFatal` is on, the default) stops apps **one at a time** in reverse order — cancel that app's ctx → `Stop` → wait for its `Exec`, all under the shutdown timeout. Invariants to preserve: per-entry `stopped` flag ⇒ `Stop` exactly once per successful `Init`; per-entry `lc` mutex serialises `Init`/`Stop` of one app; anything `Exec` returns after its ctx is cancelled is a clean stop; panics in Init/Exec/Stop/fatal handlers are recovered (an Exec panic follows `RestartPolicy`). Restart = Stop → Delay → Init → Exec; `ResetAfter` resets the retry counter after a long healthy run. A package-level default engine (`default.go`) mirrors the `Engine` methods.
- **rockservices**: deliberately small. `Catalog` holds `map[string]Service` (`Service` = `Init(ctx) error` + `Stop() error`, no names inside services). `New(map)`, `Get(name)` (panics if missing), `Init(ctx, names...)` in the given order (on failure stops what this call started, in reverse), `Stop(names...)` (no names: reverse init order; skips not-started; 30s timeout naming the hanging service). Unknown names panic before anything starts; panics in Init/Stop become errors. The project's `pkg/services/services.go` owns the catalog: name constants, `Configs`, `Init(ctx, cfg)` that builds the map from constructors and calls `catalog.Init` in order, `Stop()`, and typed getters `func Repository() repository.Service { return catalog.Get(RepositoryName).(repository.Service) }`. A child initialised outside its parent is passed to the parent's constructor. No package-level default catalog, no keys.
- **rockhandlers**: a request is a chain `middlewares → fill (transport decoding) → Validate → steps`. Steps are `Handler[P, R] func(*Ctx[P, R]) error` with P, R pointer-to-struct types; a link that returns nil without calling `Next` continues the chain automatically, `return rockhandlers.Done` ends it early with success (Done never leaves `Serve`); `Ctx` is a `context.Context` with typed `Params`/`Result`, `Next`, `Route`, typed `Local[T]`. `Chain` erases the generics into a `Route`; a transport calls `route.Serve(ctx, fill)` and maps the result / `KindOf(err)` to its protocol. Reusable steps that need request data are generic with interface constraints (models satisfy them by embedding) — no casts in user code; untyped `Middleware` only for logic that does not look at data. Groups are plain generic functions (`Prepend`). Cancellation is cooperative (no goroutines per step). Guarantees: `Serve` never panics, a partly filled `Result` never leaks on error, a new `Ctx` per call. Keep protocol things (tags, statuses, ack/nack, streaming) out of this module.
- **rockconfig**: fields map by snake_case name; tags `config:"name,omitempty,default=…,env=VAR"`; `config:"-"` for fields set in code. A field without `omitempty`/`default=` is **required: the key must be present** (zero values are fine; strings must be non-empty). Unknown keys are errors (`AllowUnknownKeys()` opts out). Durations must be strings. Env refs `${VAR}` / `${VAR:-x}` are expanded in decoded string values only (never on the raw file). Validation happens in the same pass as population (`populate.go`), because only there is it known which keys were present.

## Documentation

Each module has `README.md` and `README_RU.md` (Russian). When changing a public API, update both READMEs, the module's `example_test.go` (README snippets as compile-only `Example` funcs — CI compiles them), and `CHANGELOG.md`. The GOROCK docs site and the Claude Code plugin live in `../gorock` and describe this API too.
