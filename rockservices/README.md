# rockservices

The services layer of a GOROCK project: a catalog that stores services by name and starts and stops them in the order you give. Standard library only.

A service wraps a technology (a database, cache, broker, external API, logger) or a piece of logic (file parsing, PDF generation) that business code uses. `cmd/main` starts the services before the Apps and stops them after.

## API

```go
type Service interface {
    Init(ctx context.Context) error // open connections
    Stop() error                    // release what Init opened
}

func New(services map[string]Service) *Catalog
func (c *Catalog) Get(name string) Service                         // panics if there is none
func (c *Catalog) Init(ctx context.Context, names ...string) error // in this order
func (c *Catalog) Stop(names ...string) error                      // no names: reverse Init order
```

## The project's services package

The catalog lives in `pkg/services/services.go`, together with the services' configs, the start order and the getters:

```go
// pkg/services/services.go
package services

const (
    LoggerName     = "logger"
    RepositoryName = "repository"
)

type Configs struct {
    Logger     logger.Configs
    Repository repository.Configs
}

var catalog *rockservices.Catalog

// Init creates the services from their configs and starts them in order.
func Init(ctx context.Context, cfg Configs) error {
    catalog = rockservices.New(map[string]rockservices.Service{
        LoggerName:     logger.NewService(cfg.Logger),
        RepositoryName: repository.NewService(cfg.Repository),
    })
    return catalog.Init(ctx, LoggerName, RepositoryName) // the logger first
}

// Stop stops the services in reverse order.
func Stop() error { return catalog.Stop() }

// Getters, one per service.
func Repository() repository.Service { return catalog.Get(RepositoryName).(repository.Service) }
```

```go
// cmd/main
if err := services.Init(ctx, cfg.Services); err != nil {
    return err // no App starts
}
defer services.Stop()

return apps.Run(ctx, cfg.Apps) // rockengine
```

```go
// anywhere — usually in handlers
err := services.Repository().Insert(ctx, task)
```

## Child services

- **A child the parent opens itself** (a repository and its own pool): created and opened inside the parent; it is not in the catalog.
- **A child opened outside** — say, one pool shared by several services: create it first, put it into the catalog and pass it to the parent's constructor.

```go
pg := postgres.NewService(cfg.Postgres)
catalog = rockservices.New(map[string]rockservices.Service{
    PostgresName:   pg,
    RepositoryName: repository.NewService(cfg.Repository, pg),
})
return catalog.Init(ctx, PostgresName, RepositoryName) // the dependency first
```

## Testing

For tests, the project's `services` package can offer a function that swaps the catalog:

```go
// pkg/services/services.go
func UseCatalog(c *rockservices.Catalog) (restore func()) {
    prev := catalog
    catalog = c
    return func() { catalog = prev }
}

// a handler test
t.Cleanup(services.UseCatalog(rockservices.New(map[string]rockservices.Service{
    services.RepositoryName: &fakeRepository{},
})))
```

There is one catalog per package, so tests that swap it must not run with `t.Parallel()`.

## Rules

- `New` panics on an empty name or a nil service.
- `Get`, `Init` and `Stop` panic on an unknown name — before anything starts, so a typo shows at startup.
- `Init` starts services in the given order. If one fails, the ones this call started are stopped in reverse order and the error names the service. A second `Init` of the same service is an error.
- `Stop()` without names stops every started service in reverse `Init` order; `Stop(names...)` stops those, in that order. Services that are not started are skipped, so a second `Stop` is safe. Bounded by a timeout (30s, `WithStopTimeout`) that names the hanging service.
- Panics in `Init`/`Stop` are returned as errors.
