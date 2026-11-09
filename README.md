# gorock-kit

The core of [GOROCK](https://arzab.github.io/gorock/) — the structural pieces every GOROCK service needs, whatever it does: how it starts and stops, and how it is configured.

| Module | Description |
|---|---|
| [rockengine](./rockengine) | Lifecycle: resources first, apps in registration order, ordered graceful shutdown, restart policies, panic recovery, shutdown timeout |
| [rockconfig](./rockconfig) | One config file (YAML/JSON) → typed `Configs`: snake_case mapping, `${ENV:-default}`, defaults, required fields and unknown-key checks |

Go 1.26+. `rockengine` uses only the standard library; `rockconfig` adds `gopkg.in/yaml.v3`.

HTTP server, PostgreSQL/Redis services, logger, event bus, scheduler and Telegram bot were moved out of the kit to [gorock-catalog](https://github.com/arzab/gorock-catalog). Their last versions here, `v0.2.0`, stay available.

## Contracts

Everything the engine runs is one of two things:

```go
// App — something that works: an HTTP server, a consumer, a scheduler.
type App interface {
    Init(ctx context.Context) error
    Exec(ctx context.Context) error // return when ctx is cancelled
    Stop() []error
}

// Resource — something apps use: a DB pool, a cache client.
type Resource interface {
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

func main() {
    cfg, err := rockconfig.InitFromFile[Configs]("./configs/configs.yaml")
    if err != nil {
        log.Fatal(err)
    }

    engine := rockengine.NewEngine()
    services.Register(engine, cfg.Services) // resources: init first, stop last
    apps.Register(engine, cfg.Apps)         // apps: stop one by one in reverse order

    if err := engine.Run(); err != nil { // blocks until SIGINT/SIGTERM or an app fails
        log.Fatal(err)
    }
}
```

```go
// pkg/services/services.go
type Configs struct {
    Repository repository.Configs
}

var repo repository.Service

func Repository() repository.Service { return repo }

func Register(e *rockengine.Engine, cfg Configs) {
    repo = repository.NewService(cfg.Repository) // implements rockengine.Resource
    e.MustRegisterResource("repository", repo)
}
```

Where each piece of code goes and why: [GOROCK architecture](https://arzab.github.io/gorock/architecture/).

## Installation

```sh
go get github.com/arzab/gorock-kit/rockengine
go get github.com/arzab/gorock-kit/rockconfig
```

## AI coding agents

A Claude Code plugin teaches the agent the GOROCK layout (skill `gorock:service`):

```sh
claude plugin marketplace add arzab/gorock
claude plugin install gorock@gorock
```

For Codex, Cursor and other agents, put [AGENTS.md](https://github.com/arzab/gorock/blob/main/plugin/skills/service/templates/AGENTS.md) in your project root. Note: the plugin's recipes still target the full kit `v0.2.0` and will be updated to the core + catalog split.

## Local development

`go.work` at the repo root wires both modules. There is no root module, so `go build ./...` from the root matches nothing — use the check script:

```sh
bash scripts/check.sh all                # build + vet + test, every module
bash scripts/check.sh test rockengine    # one module
bash scripts/check.sh tidy-check         # go.mod / go.sum must be tidy
```

See [CHANGELOG.md](./CHANGELOG.md).
