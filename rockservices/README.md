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

A service has no name of its own: its name is the **key** it is registered under, so the same service can be registered twice under two keys.

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

Keys and registration live in one file, `pkg/services/services.go`. A key is typed: the service type is written once, in the key, and `Add` checks the constructor's type against it at compile time.

```go
// pkg/services/services.go
var (
    Logger = rockservices.NewKey[logger.Service]("logger")
    Orders = rockservices.NewKey[orders.Service]("orders")
)

type Configs struct {
    Logger logger.Configs
    Orders orders.Configs
}

func Register(c *rockservices.Catalog, cfg Configs) {
    rockservices.Add(c, Logger, logger.NewService, cfg.Logger) // add order = Init order
    rockservices.Add(c, Orders, orders.NewService, cfg.Orders)
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
return engine.Run() // services stop after every app
```

```go
// anywhere — usually in handlers
err := services.Orders.Get().Insert(ctx, order) // typed, no cast
```

**A forgotten registration is caught at startup.** `Init` of the default catalog checks that every declared key was added and names the missing ones — the project does not start, instead of panicking on the first request. Catalogs from `New()` skip the check: tests put only the fakes they need there.

## Dependencies between services

- **A service owns a child service** (a repository creates its own pool): nothing to do. The catalog sees only the parent, which opens and closes the child itself.
- **A shared dependency** is a service in the catalog too. Register it first and pass the **value `Add` returned** to the constructor:

```go
func Register(c *rockservices.Catalog, cfg Configs) {
    pg := rockservices.Add(c, Postgres, postgres.NewService, cfg.Postgres)
    rockservices.Add(c, Orders, func(cfg orders.Configs) orders.Service {
        return orders.NewService(cfg, pg) // created, not opened yet — use it from Init on
    }, cfg.Orders)
}
```

Don't call `key.Get()` inside a constructor: it reads the default catalog, not the one being filled, so `Register(rockservices.New(), cfg)` would wire the wrong service or panic. A service package doesn't know the key its dependency is registered under; all wiring stays in `services.go`.

**Two instances of one service** are two keys and two registrations:

```go
rockservices.Add(c, OrdersDB, postgres.NewService, cfg.OrdersDB)
rockservices.Add(c, AnalyticsDB, postgres.NewService, cfg.AnalyticsDB)
```

**The logger is a service too.** Register it first, so the others can log from their `Init`.

## Testing

Business code calls `key.Get()`, which reads the default catalog. In a test, build a catalog with fakes and make it the default:

```go
func TestCreateOrder(t *testing.T) {
    c := rockservices.New()
    rockservices.Add(c, services.Orders, func(struct{}) orders.Service { return &fakeOrders{} }, struct{}{})
    prev := rockservices.SetDefault(c)
    t.Cleanup(func() { rockservices.SetDefault(prev) })

    // ... call the handler under test
}
```

`SetDefault` is for tests; in production `services.Register` fills `Default()`. There is one default catalog per process, so tests that swap it **must not run with `t.Parallel()`**.

## Rules

- Services are stored **by key**. `key.Get()` reads the default catalog, `key.From(c)` a given one. A service missing from the catalog panics with its name (never after a successful `Init` of the default catalog).
- `Init` of the default catalog first checks that every declared key was added and names the missing ones. Catalogs from `New()` don't check.
- `Add` calls the constructor immediately. It panics on a nil key, an empty or duplicate name (also across two different keys), a nil service, a constructor panic, or a call after `Init` — wiring bugs surface at startup.
- `Init` runs in add order, once: a second call returns `ErrAlreadyStarted`. If one fails, the services already opened are closed in reverse order, and the error names the service.
- `Stop` runs in reverse order, collects every error, is safe to call twice, and is bounded by a timeout (30s, `WithStopTimeout`) that names the hanging service.
- Panics in `Init`/`Stop` are returned as errors.
- `rockservices.New()` is an isolated catalog (`key.From(c)`); `SetDefault` makes it the default in tests.
