# gorock-kit

A collection of Go modules for building production-ready applications. Each module is independent — install only what you need.

## Modules

| Module | Description |
|---|---|
| [rockengine](./rockengine) | App lifecycle orchestration: init, concurrent exec, graceful shutdown, restart policies |
| [rockconfig](./rockconfig) | Config loader from JSON/YAML with snake_case mapping, env expansion, and validation |
| [rocklog](./rocklog) | Structured logger interface with a `log/slog` backend and global/instance usage |
| [rockfiber](./rockfiber) | Fiber HTTP server with typed endpoints, middlewares, and rockengine integration |
| [rockredis](./rockredis) | Redis client wrapper (go-redis v9) with typed Service interface |
| [rockbun](./rockbun) | PostgreSQL wrapper (bun ORM) with connection pool and transaction helpers |
| [rockbus](./rockbus) | In-process event bus with per-topic ordered delivery and rockengine integration |
| [rockcron](./rockcron) | Scheduled job runner with cron expressions, interval jobs, and handler chains |
| [rocktelebot](./rocktelebot) | Telegram bot wrapper with structured handlers, built-in middlewares, and keyboard builders |

## Architecture

- **The modules do not import each other** — install only what you need. Lifecycle components (`rockfiber`, `rockbus`, `rockcron`, `rocktelebot`) implement `rockengine.App` (`Init / Exec / Stop`) by signature, and the database / cache wrappers (`rockbun`, `rockredis`) implement `rockengine.Resource` (`Init / Stop`).
- **rockengine** runs them: resources start first, apps start in registration order, and on shutdown apps stop one by one in reverse order before the resources are closed.
- **rockconfig** loads one typed config file; every module exposes a `Config`/`Configs` struct that fits into it.
- **rocklog** is a structured logger. Modules that report errors take callbacks (`OnError`, `onError`), so you can plug in rocklog or any other logger.

```
rockengine ── resources: rockbun, rockredis     (init first, stop last)
           └─ apps:      rockbus, rockcron,
                         rockfiber, rocktelebot (stop in reverse order)

rockconfig ──► one file → every module's Config
rocklog    ◄── wired in through OnError callbacks and rocklog.WithContext
```

## Example: full application

The complete, runnable version is [`examples/fullapp`](./examples/fullapp) — it is built in CI, so this snippet cannot drift from the API.

```go
func main() {
    cfg, err := rockconfig.InitFromFile[AppConfig]("config.yaml")
    if err != nil {
        log.Fatal(err)
    }

    rocklog.Init(rocklog.Config{Level: rocklog.LevelInfo, Format: rocklog.FormatJSON})

    engine := rockengine.NewEngine()

    // Resources: initialised before every app, closed after every app.
    db := rockbun.NewService(cfg.DB)
    engine.MustRegisterResource("db", db)
    engine.MustRegisterResource("redis", rockredis.NewService(cfg.Redis))

    // Event bus
    bus := rockbus.NewApp(cfg.Bus,
        func(ctx context.Context, event rockbus.Event, err error) { /* handle */ },
        rockbus.On("user.created", onUserCreated),
    )
    rockbus.SetDefault(bus)
    engine.MustRegister("bus", bus)

    // Scheduler
    cron := rockcron.NewApp(cfg.Cron,
        func(ctx context.Context, job rockcron.Job, err error) { /* handle */ },
        rockcron.Every("ping-db", time.Minute, func(ctx context.Context) error { return db.Ping(ctx) }),
    )
    engine.MustRegister("cron", cron)

    // HTTP server — registered last, so it is stopped first.
    server := rockfiber.New(cfg.HTTP,
        rockfiber.GET("/health", healthHandler),
        rockfiber.POST("/users", createUserHandler),
    )
    engine.MustRegister("http", server, rockengine.RestartPolicy{MaxRetries: 3, Delay: time.Second})

    if err := engine.Run(); err != nil { // blocks until SIGINT/SIGTERM or an app fails
        log.Fatal(err)
    }
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

`go.work` at the repo root wires all modules (and `examples/fullapp`) together. There is no root module, so `go build ./...` from the root matches nothing — use the check script, which loops over modules:

```sh
bash scripts/check.sh all                # build + vet + test, every module
bash scripts/check.sh test rockengine    # one module
bash scripts/check.sh tidy-check         # go.mod / go.sum must be tidy
```

Tests run with `-race` when the toolchain supports it (needs cgo and a C compiler). See [CHANGELOG.md](./CHANGELOG.md) for behaviour changes.
