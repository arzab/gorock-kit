# rockengine

App lifecycle orchestration engine for Go.

## Overview

`rockengine` manages the lifecycle of multiple named applications: sequential initialisation, concurrent execution, per-app restart policies, and graceful shutdown.

**App lifecycle:**

```
Init → Exec → Stop
```

- **Init** — called sequentially in registration order; sets up resources.
- **Exec** — runs concurrently in a goroutine; should respect `ctx` cancellation.
- **Stop** — called on shutdown; must unblock a blocking `Exec` and wait for in-flight work.

## Implementing App

```go
type App interface {
    Init(ctx context.Context) error
    Exec(ctx context.Context) error
    Stop() []error
}
```

**Example:**

```go
type WorkerApp struct {
    ticker *time.Ticker
    done   chan struct{}
}

func (a *WorkerApp) Init(_ context.Context) error {
    a.ticker = time.NewTicker(time.Second)
    a.done = make(chan struct{})
    return nil
}

func (a *WorkerApp) Exec(ctx context.Context) error {
    for {
        select {
        case <-ctx.Done():
            return nil
        case <-a.ticker.C:
            fmt.Println("tick")
        }
    }
}

func (a *WorkerApp) Stop() []error {
    a.ticker.Stop()
    close(a.done)
    return nil
}
```

## Quick Start

```go
package main

import (
    "log"
    "github.com/arzab/gorock-kit/rockengine"
)

func main() {
    rockengine.Register("worker", &WorkerApp{})
    rockengine.Register("http",   &HTTPApp{})

    if err := rockengine.Run(); err != nil {
        log.Fatal(err)
    }
}
```

`Run` blocks until `SIGINT` / `SIGTERM` is received (or an app fails, see below), then gracefully stops all apps in reverse registration order.

## Restart Policy

By default, an app whose `Exec` returns an error (or panics) is not restarted: it is marked as `failed`, its `Stop` is called, and the **whole engine shuts down** — `Run` returns an error naming the app. This lets an orchestrator (systemd, Kubernetes) restart a process that lost a component instead of leaving it half-alive.

To enable automatic restarts:

```go
rockengine.Register("worker", &WorkerApp{}, rockengine.RestartPolicy{
    MaxRetries: -1,               // -1 = unlimited, 0 = no restart (default), N = up to N times
    Delay:      2 * time.Second,
    ResetAfter: time.Minute,      // a run that lasted >=1m resets the retry counter; 0 = never reset
    OnFatal: func(err error) {   // called when the app is marked failed (overrides engine-level handler)
        log.Printf("worker is dead: %v", err)
    },
})
```

- A restart is `Stop → Delay → Init → Exec`. Every successful `Init` is matched by exactly one `Stop`.
- A panic in `Exec` counts as a failure and follows the policy. Panics in `Init`, `Stop` and fatal handlers are recovered and reported as errors.
- Whatever `Exec` returns after its `ctx` was cancelled (`nil`, `ctx.Err()`, `http.ErrServerClosed`, ...) is a clean stop, never a failure.
- Without `ResetAfter`, `MaxRetries` counts failures over the whole process lifetime.

To keep the engine running when an app fails, use `SetStopOnFatal(false)` / `WithStopOnFatal(false)` and observe failures via the fatal handler or `AppStatuses`.

## Custom Engine

For more control, create an `Engine` directly:

```go
engine := rockengine.NewEngine().
    WithShutdownTimeout(15 * time.Second).
    WithStopOnFatal(false). // keep other apps running when one fails
    WithFatalHandler(func(name string, err error) {
        log.Printf("app %s is dead: %v", name, err)
    })

engine.Register("worker", &WorkerApp{}, rockengine.RestartPolicy{MaxRetries: 3, Delay: time.Second})
engine.Register("http",   &HTTPApp{})

if err := engine.Run(); err != nil {
    log.Fatal(err)
}
```

## Orchestration

Individual apps can be controlled at runtime without affecting others:

```go
// Stop a single app
rockengine.StopApp("worker")

// Restart a single app (Stop → Init → Exec)
rockengine.RestartApp("worker")

// Inspect status
info, err := rockengine.AppStatus("worker")
fmt.Println(info.State, info.Retries, info.Err)

// Inspect all apps
for _, info := range rockengine.AppStatuses() {
    fmt.Printf("%s: %s\n", info.Name, info.State)
}
```

## App States

| State        | Description                                      |
|--------------|--------------------------------------------------|
| `idle`       | Registered, not yet started                      |
| `running`    | `Exec` is active                                 |
| `restarting` | Waiting between restart attempts                 |
| `stopped`    | Stopped cleanly                                  |
| `failed`     | Exceeded restart limit or re-`Init` failed       |

## API Reference

### Package-level (default engine)

| Function                                          | Description                              |
|---------------------------------------------------|------------------------------------------|
| `Register(name, app, policy?)`                   | Register an app                          |
| `Run() error`                                     | Start and block                          |
| `RunContext(ctx) error`                           | Start with a custom context              |
| `StopApp(name) error`                             | Stop a single app                        |
| `RestartApp(name) error`                          | Restart a single app                     |
| `AppStatus(name) (AppInfo, error)`                | Status of one app                        |
| `AppStatuses() []AppInfo`                         | Status of all apps                       |
| `Shutdown() []error`                              | Stop all apps                            |
| `SetShutdownTimeout(d)`                           | Set global shutdown timeout (default 30s)|
| `SetFatalHandler(fn)`                             | Set handler for fatal app failures       |
| `SetStopOnFatal(bool)`                            | Shut down the engine when an app fails (default `true`) |

### Engine methods

Same as above, plus:

| Method                        | Description                     |
|-------------------------------|---------------------------------|
| `NewEngine() *Engine`         | Create a new engine             |
| `WithShutdownTimeout(d)`      | Set shutdown timeout            |
| `WithFatalHandler(fn)`        | Set fatal handler               |
| `WithStopOnFatal(bool)`       | Shut down on app failure (default `true`) |

## Shutdown Contract

Apps are stopped **one at a time** in reverse registration order. For each app:

1. its `ctx` is cancelled — `Exec` should stop accepting new work and finish in-flight operations;
2. `Stop` is called right after, to unblock a blocking `Exec` (e.g. `server.Shutdown`);
3. the engine waits for `Exec` to return before moving on to the previous app.

So an HTTP server registered after the event bus is fully stopped before the bus is touched.

- `Stop` must not forcefully cut ongoing work. The shutdown timeout (default 30s) covers the whole sequence including `Stop` calls; on timeout `Run` returns an error naming the app that hung.
- The engine calls `Stop` at most once per successful `Init`, but `Stop` may run concurrently with `Exec` — it must be safe for that.
