# rockengine

The App engine of a GOROCK project: HTTP servers, queue consumers, schedulers, bots. It starts them in the order you give, waits for a signal, context cancellation or an App failure, and stops them one by one in reverse order. Standard library only.

## App

```go
type App interface {
    Init(ctx context.Context) error // prepare: connections, routes
    Exec(ctx context.Context) error // work until ctx is cancelled
    Stop() []error                  // unblock Exec and wait for work in progress
}
```

- **`Exec`**, when `ctx` is cancelled, stops taking new work, finishes what it started and returns. Whatever it returns after cancellation is a clean stop.
- **`Stop`** is called right after the context is cancelled, while `Exec` may still run. It must gracefully unblock a blocking `Exec` (an HTTP server). The engine calls `Stop` at most once per successful `Init` — also when `Exec` never ran, so `Stop` must not wait for `Exec`.

## Running

```go
// cmd/apps/apps.go
func Run(ctx context.Context, cfg Configs) error {
    engine := rockengine.New(map[string]rockengine.App{
        "bus":  rockbus.NewApp(cfg.Bus),
        "http": rockhttp.New(cfg.HTTP, httpEndpoints()...),
    })
    return engine.Run(ctx, "bus", "http") // http starts last and stops first
}
```

```go
// cmd/main
if err := services.Init(ctx, cfg.Services); err != nil {
    return err
}
defer services.Stop()
return apps.Run(ctx, cfg.Apps)
```

## How `Run` works

1. **Start:** `Init` of each App in the given order. If one fails, the ones already initialised are stopped in reverse order and `Run` returns an error naming the App.
2. **Work:** `Exec` of each App in its own goroutine.
3. **Stop** — on `SIGINT`/`SIGTERM`, `ctx` cancellation or an App failure. Apps stop one by one in reverse order: cancel its context → `Stop` → wait for `Exec`. Bounded by a timeout (30s, `WithShutdownTimeout`) that names the hanging App.
4. **Result:** the reason (an App failure) joined with every `Stop` error. A clean stop returns `nil`.

**An App failure** is its `Exec` returning an error or panicking while its context was not cancelled. An `Exec` that returns `nil` before shutdown just ends; the other Apps go on.

## Restarts

By default the first App failure stops everything, and the orchestrator (Kubernetes, systemd, Docker) restarts the process. Without one:

```go
rockengine.New(apps).WithRestarts(3, time.Second) // Stop → pause → Init → Exec, up to 3 times
```

When the restarts run out, or `Init` fails during a restart, the engine stops everything. Reconnecting to a broker after a network drop is better done inside the App itself, in its `Exec`.

## Rules

- `New` panics on an empty name or a nil App; `Run` panics on an unknown or repeated name. Wiring bugs show at startup.
- Panics in `Init`, `Exec` and `Stop` become errors.
- App contexts don't inherit the given `ctx`'s cancellation directly: the engine cancels them itself, one by one, in reverse order.
