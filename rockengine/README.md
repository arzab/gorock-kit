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

`Run` blocks until `SIGINT` / `SIGTERM` is received, then gracefully stops all apps in reverse registration order.

## Restart Policy

By default, a failing app is marked as `failed` and the engine keeps running. To enable automatic restarts:

```go
rockengine.Register("worker", &WorkerApp{}, rockengine.RestartPolicy{
    MaxRetries: -1,              // -1 = unlimited, 0 = no restart (default), N = up to N times
    Delay:      2 * time.Second,
    OnFatal: func(err error) {  // called when retries are exhausted (overrides engine-level handler)
        log.Printf("worker is dead: %v", err)
    },
})
```

## Custom Engine

For more control, create an `Engine` directly:

```go
engine := rockengine.NewEngine().
    WithShutdownTimeout(15 * time.Second).
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
| `failed`     | Exceeded restart limit or fatal error in `Init`  |

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

### Engine methods

Same as above, plus:

| Method                        | Description                     |
|-------------------------------|---------------------------------|
| `NewEngine() *Engine`         | Create a new engine             |
| `WithShutdownTimeout(d)`      | Set shutdown timeout            |
| `WithFatalHandler(fn)`        | Set fatal handler               |

## Shutdown Contract

- `ctx` cancellation signals `Exec` to stop accepting new work and finish in-flight operations.
- `Stop` is called immediately after to unblock a blocking `Exec` (e.g. `server.Shutdown`).
- `Stop` must not forcefully cut ongoing work — the engine enforces a timeout as a last resort.
- `Stop` should be idempotent.
- Apps are stopped in **reverse** registration order to respect inverse dependencies.
