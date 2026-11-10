# gorock-kit

The core of [GOROCK](https://arzab.github.io/gorock/) — the structural pieces every GOROCK service needs, whatever it does: how it starts and stops, and how it is configured.

| Module | Description |
|---|---|
| [rockengine](./rockengine) | Apps lifecycle: start in registration order, ordered graceful shutdown, restart policies, panic recovery, shutdown timeout |
| [rockconfig](./rockconfig) | One config file (YAML/JSON) → typed `Configs`: snake_case mapping, `${ENV:-default}`, defaults, required fields and unknown-key checks |
| [rockservices](./rockservices) | Services layer: one catalog that creates, stores by name, hands out, initialises and stops the services of `pkg/services` |
| [rockhandlers](./rockhandlers) | Transport-independent request handling: a chain of typed steps with `Next`, Locals, middlewares, validation, panic recovery and error kinds — any transport (HTTP, queue, CLI) runs the same routes |

Go 1.26+. `rockengine`, `rockservices` and `rockhandlers` use only the standard library; `rockconfig` adds `gopkg.in/yaml.v3`.

HTTP server, PostgreSQL/Redis services, logger, event bus, scheduler and Telegram bot live in [gorock-catalog](https://github.com/arzab/gorock-catalog), including `rockhttp`, the HTTP transport for rockhandlers. Their last versions inside the kit, `v0.2.0`, stay available.

## How they fit

```
cmd/main:  rockconfig → rockservices.Init → rockengine.Run (apps) → rockservices.Stop
```

Services are a layer of their own: they start before the apps and stop after them. The engine runs only apps:

```go
// an App: HTTP server, consumer, scheduler, bot
type App interface {
    Init(ctx context.Context) error
    Exec(ctx context.Context) error // return when ctx is cancelled
    Stop() []error
}

// a service in pkg/services: DB, cache, external API
type Service interface {
    Init(ctx context.Context) error
    Stop() error
}
```

## Example

```go
type Configs struct {
    Services services.Configs
    Apps     apps.Configs
}

func Exec(ctx context.Context) error {
    cfg, err := rockconfig.InitFromFile[Configs]("./configs/configs.yaml")
    if err != nil {
        return err
    }

    services.Register(rockservices.Default(), cfg.Services)
    if err := rockservices.Init(ctx); err != nil {
        return err
    }
    defer rockservices.Stop()

    engine := rockengine.NewEngine()
    apps.Register(engine, cfg.Apps)
    return engine.Run() // blocks until SIGINT/SIGTERM or an app fails
}
```

```go
// pkg/services/services.go
const RepositoryName = "repository" // the name is given at registration

type Configs struct {
    Repository repository.Configs
}

func Register(c *rockservices.Catalog, cfg Configs) {
    rockservices.Add(c, RepositoryName, repository.NewService, cfg.Repository)
}

func Repository() repository.Service { return rockservices.Get[repository.Service](RepositoryName) }

// anywhere: services.Repository().Insert(ctx, order)
```

Where each piece of code goes and why: [GOROCK architecture](https://arzab.github.io/gorock/architecture/).

## Installation

```sh
go get github.com/arzab/gorock-kit/rockengine
go get github.com/arzab/gorock-kit/rockconfig
go get github.com/arzab/gorock-kit/rockservices
go get github.com/arzab/gorock-kit/rockhandlers
```

## AI coding agents

A Claude Code plugin teaches the agent the GOROCK layout (skill `gorock:service`):

```sh
claude plugin marketplace add arzab/gorock
claude plugin install gorock@gorock
```

For Codex, Cursor and other agents, put [AGENTS.md](https://github.com/arzab/gorock/blob/main/plugin/skills/service/templates/AGENTS.md) in your project root. Note: the plugin's recipes still target the full kit `v0.2.0` and will be updated to the core + catalog split.

## Local development

`go.work` at the repo root wires the modules. There is no root module, so `go build ./...` from the root matches nothing — use the check script:

```sh
bash scripts/check.sh all                # build + vet + test, every module
bash scripts/check.sh test rockengine    # one module
bash scripts/check.sh tidy-check         # go.mod / go.sum must be tidy
```

See [CHANGELOG.md](./CHANGELOG.md).
