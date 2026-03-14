# gorock-kit

A collection of Go modules for building production-ready applications. Each module is independent — install only what you need.

## Modules

| Module | Description |
|---|---|
| [rockengine](./rockengine) | App lifecycle orchestration: init, concurrent exec, graceful shutdown, restart policies |
| [rockconfig](./rockconfig) | Config loader from JSON/YAML with snake_case mapping, env expansion, and validation |
| [rocklog](./rocklog) | Structured logger interface with logrus backend and global/instance usage |
| [rockfiber](./rockfiber) | Fiber HTTP server with typed endpoints, middlewares, and rockengine integration |
| [rockredis](./rockredis) | Redis client wrapper (go-redis v9) with typed Service interface |
| [rockbun](./rockbun) | PostgreSQL wrapper (bun ORM) with connection pool and transaction helpers |
| [rockbus](./rockbus) | In-process event bus with per-topic ordered delivery and rockengine integration |
| [rockcron](./rockcron) | Scheduled job runner with cron expressions, interval jobs, and handler chains |
| [rocktelebot](./rocktelebot) | Telegram bot wrapper with structured handlers, built-in middlewares, and keyboard builders |

## Architecture

All modules follow a consistent pattern:

- **rockengine** is the backbone — any component that needs a managed lifecycle implements `App` (`Init / Exec / Stop`) and registers with the engine.
- **rockconfig** loads typed config structs from a file; each module exposes its own `Config`/`Configs` struct compatible with rockconfig's field mapping.
- **rocklog** provides structured logging used across all modules.

```
rockengine
    ├── rockfiber   (HTTP server)
    ├── rockbus     (event bus)
    ├── rockcron    (scheduler)
    ├── rocktelebot (telegram bot)
    └── your apps

rockconfig  ──►  all module Configs
rocklog     ──►  all modules
```

## Example: full application

```go
func main() {
    cfg, err := rockconfig.InitFromFile[AppConfig]("config.yaml")
    if err != nil {
        log.Fatal(err)
    }

    rocklog.Init(rocklog.Config{Level: rocklog.LevelInfo, Format: rocklog.FormatJSON})

    engine := rockengine.New()

    // HTTP server
    server := rockfiber.New(cfg.HTTP,
        rockfiber.GET("/health", healthHandler),
        rockfiber.POST("/users", createUserHandler),
    )
    engine.MustRegister("http", server, rockengine.RestartPolicy{})

    // Event bus
    bus := rockbus.NewApp(cfg.Bus,
        func(ctx context.Context, event rockbus.Event, err error) { /* handle */ },
        rockbus.On("user.created", onUserCreated),
        rockbus.On("order.placed", onOrderPlaced),
    )
    rockbus.SetDefault(bus)
    engine.MustRegister("bus", bus, rockengine.RestartPolicy{})

    // Scheduler
    cron := rockcron.NewApp(cfg.Cron,
        func(ctx context.Context, job rockcron.Job, err error) { /* handle */ },
        rockcron.Every("sync-cache",  5*time.Minute, syncCache),
        rockcron.Cron("daily-report", "0 3 * * *",  dailyReport),
    )
    engine.MustRegister("cron", cron, rockengine.RestartPolicy{})

    engine.Run()
}
```

## Installation

Each module is a separate Go module — install only what you need:

```sh
go get github.com/arzab/gorock-kit/rockengine
go get github.com/arzab/gorock-kit/rockconfig
go get github.com/arzab/gorock-kit/rocklog
go get github.com/arzab/gorock-kit/rockfiber
go get github.com/arzab/gorock-kit/rockredis
go get github.com/arzab/gorock-kit/rockbun
go get github.com/arzab/gorock-kit/rockbus
go get github.com/arzab/gorock-kit/rockcron
go get github.com/arzab/gorock-kit/rocktelebot
```

## Local development

A `go.work` file at the repo root wires all modules together:

```sh
go work sync
go build ./...
```
