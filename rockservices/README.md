# rockservices

The services layer of a GOROCK project: one catalog that creates, stores, hands out, initialises and stops every service in `pkg/services`. Standard library only.

Services are independent of apps: `cmd/main` starts them before the apps and stops them after.

## Service

```go
type Service interface {
    Name() string                   // key in the catalog; by convention the package's Name constant
    Init(ctx context.Context) error // open connections
    Stop() error                    // release what Init opened
}
```

```go
// pkg/services/repository/interface.go
const Name = "repository"

type Service interface {
    rockservices.Service
    Insert(ctx context.Context, model any) error
}

// pkg/services/repository/service.go
func NewService(cfg Configs) Service { return &service{cfg: cfg} } // no connections here
func (s *service) Name() string    { return Name }
```

## Register, start, use

```go
// pkg/services/services.go — the only wiring place
type Configs struct {
    Postgres   postgres.Configs
    Repository repository.Configs
}

func Register(c *rockservices.Catalog, cfg Configs) {
    rockservices.Add(c, postgres.NewService, cfg.Postgres)     // add order = init order
    rockservices.Add(c, repository.NewService, cfg.Repository)
}
```

```go
// cmd/main
services.Register(rockservices.Default(), cfg.Services)
if err := rockservices.Init(ctx); err != nil {
    return err // no app starts
}
defer rockservices.Stop()

engine := rockengine.NewEngine()
apps.Register(engine, cfg.Apps)
return engine.Run() // services stop after every app has stopped
```

```go
// anywhere
repo := rockservices.Get[repository.Service](repository.Name)
```

## Dependencies between services

- **A service owns its child** (repository creates its own pool): nothing to do — the catalog only sees the parent, which inits and stops the child itself.
- **A shared dependency**: the constructor gets services **added before it**. Add dependencies first.

```go
func NewService(cfg Configs) Service {
    return &service{db: rockservices.Get[postgres.Service](postgres.Name)} // created, not yet initialised
}
```

The dependency is created but its `Init` runs later, in add order — keep the reference and use it from `Init` on.

## Conventions

**A getter in the service package** keeps the name and the type together, so callers can't mix them up:

```go
// pkg/services/repository/interface.go
const Name = "repository"

func Get() Service { return rockservices.Get[Service](Name) }

// delivery: repository.Get().Insert(ctx, order)
```

**Reusable components take their name from config.** A project's own service uses a constant name. A component that can be added more than once (two PostgreSQL databases) reads its name from config with a default, so the second instance doesn't collide:

```go
type Configs struct {
    Name string `config:",default=postgres"`
    // ...
}

func (s *service) Name() string { return s.cfg.Name }
```

**Extra constructor arguments** (hooks, clients set in code) go through a closure:

```go
rockservices.Add(c, func(cfg postgres.Configs) postgres.Service {
    return postgres.NewService(cfg, queryHooks)
}, cfg.Postgres)
```

## Testing

Business code calls `rockservices.Get`, which reads the default catalog. In a test, build a catalog with fakes and make it the default:

```go
func TestCreateOrder(t *testing.T) {
    c := rockservices.New()
    rockservices.Add(c, func(struct{}) repository.Service { return &fakeRepo{} }, struct{}{})
    prev := rockservices.SetDefault(c)
    t.Cleanup(func() { rockservices.SetDefault(prev) })

    // ... call the delivery code under test
}
```

`SetDefault` is for tests only; production code fills `Default()` in `services.Register`.

## Rules

- Services are stored **by name**; `Get[T]` only gives the typed interface back. Unknown name or wrong type → panic with the name.
- `Add` calls the constructor immediately. It panics on a nil service, an empty or duplicate `Name()`, a constructor panic, or a call after `Init` — wiring bugs surface at startup.
- `Init` runs in add order; if one fails, the ones already initialised are stopped in reverse and the error names the service.
- `Stop` runs in reverse order, collects every error, is safe to call twice, and is bounded by a timeout (30s, `WithStopTimeout`) that names a hanging service.
- Panics in `Init`/`Stop` are returned as errors.
- `rockservices.New()` gives an isolated catalog (`GetFrom[T](c, name)`); `SetDefault` makes one the default for tests.
