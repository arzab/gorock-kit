# rockservices

The services layer of a GOROCK project: one catalog that creates, stores, hands out, opens and closes every service in `pkg/services`. Standard library only.

A service wraps a technology (a database, cache, broker, external API, logger) or a piece of logic (file parsing, PDF generation) that business code uses. Services are independent of apps: `cmd/main` opens them before the apps start and closes them after the apps stop.

## Service

```go
type Service interface {
    Init(ctx context.Context) error // open connections
    Stop() error                    // release what Init opened
}
```

A service has no name of its own: the name is given at registration, so the same service can be registered twice under two names.

```go
// pkg/services/orders/interface.go
type Service interface {
    rockservices.Service
    Insert(ctx context.Context, order any) error
}

// pkg/services/orders/service.go
func NewService(cfg Configs) Service { return &service{cfg: cfg} } // no connections here
```

## Register, start, use

Names, registration and getters live in one file, `pkg/services/services.go`:

```go
// pkg/services/services.go
const (
    Logger = "logger"
    Orders = "orders"
)

type Configs struct {
    Logger logger.Configs
    Orders orders.Configs
}

func Register(c *rockservices.Catalog, cfg Configs) {
    rockservices.Add(c, Logger, logger.NewService, cfg.Logger) // add order = Init order
    rockservices.Add(c, Orders, orders.NewService, cfg.Orders)
}

// Getters: the name and the type are written once.
func OrdersRepository() orders.Service { return rockservices.Get[orders.Service](Orders) }
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
return engine.Run() // services stop after every app
```

```go
// anywhere — usually in handlers
err := services.OrdersRepository().Insert(ctx, order)
```

## Dependencies between services

- **A service owns a child service** (a repository creates its own pool): nothing to do. The catalog sees only the parent, which opens and closes the child itself.
- **A shared dependency** is a service in the catalog too. Register it first and pass it to the constructor through a closure:

```go
func Register(c *rockservices.Catalog, cfg Configs) {
    rockservices.Add(c, Postgres, postgres.NewService, cfg.Postgres)
    rockservices.Add(c, Orders, func(cfg orders.Configs) orders.Service {
        return orders.NewService(cfg, PostgresDB()) // created, not opened yet — use it from Init on
    }, cfg.Orders)
}
```

A service package doesn't know the name its dependency is registered under; all wiring stays in `services.go`.

**Two instances of one service** are two registrations under two names:

```go
rockservices.Add(c, OrdersDB, postgres.NewService, cfg.OrdersDB)
rockservices.Add(c, AnalyticsDB, postgres.NewService, cfg.AnalyticsDB)
```

**The logger is a service too.** Register it first, so the others can log from their `Init`.

## Testing

Business code calls the getters, and they read the default catalog. In a test, build a catalog with fakes and make it the default:

```go
func TestCreateOrder(t *testing.T) {
    c := rockservices.New()
    rockservices.Add(c, services.Orders, func(struct{}) orders.Service { return &fakeOrders{} }, struct{}{})
    prev := rockservices.SetDefault(c)
    t.Cleanup(func() { rockservices.SetDefault(prev) })

    // ... call the handler under test
}
```

`SetDefault` is for tests; in production `services.Register` fills `Default()`.

## Rules

- Services are stored **under their registration name**; `Get[T]` only returns the typed interface. An unknown name or a wrong type panics with the name.
- `Add` calls the constructor immediately. It panics on an empty or duplicate name, a nil service, a constructor panic, or a call after `Init` — wiring bugs surface at startup.
- `Init` runs in add order. If one fails, the services already opened are closed in reverse order, and the error names the service.
- `Stop` runs in reverse order, collects every error, is safe to call twice, and is bounded by a timeout (30s, `WithStopTimeout`) that names the hanging service.
- Panics in `Init`/`Stop` are returned as errors.
- `rockservices.New()` is an isolated catalog (`GetFrom[T](c, name)`); `SetDefault` makes it the default in tests.
