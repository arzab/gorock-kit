package rockservices

import (
	"context"
	"sync/atomic"
)

// defaultCatalog is the project-wide catalog behind the package-level functions.
var defaultCatalog atomic.Pointer[Catalog]

func init() { defaultCatalog.Store(New()) }

// Default returns the project-wide catalog. Pass it to services.Register.
func Default() *Catalog { return defaultCatalog.Load() }

// SetDefault replaces the project-wide catalog and returns the previous one.
// It exists for tests: build a catalog with fakes, make it the default, and
// restore the old one afterwards, so code that calls Get sees the fakes.
//
//	c := rockservices.New()
//	rockservices.Add(c, services.Orders, fakeOrders, struct{}{})
//	t.Cleanup(func() { rockservices.SetDefault(rockservices.SetDefault(c)) })
//
// Don't call it in production code: services.Register fills Default().
func SetDefault(c *Catalog) (previous *Catalog) {
	if c == nil {
		panic("rockservices: SetDefault(nil)")
	}
	return defaultCatalog.Swap(c)
}

// Get returns the service stored under name in the default catalog as type S.
// It panics if there is no such service or it is not an S.
//
//	orders := rockservices.Get[orders.Service](services.Orders)
//
// Projects wrap it in getters next to the registration, so the name and the
// type are written once: services.OrdersRepository().
func Get[S any](name string) S { return GetFrom[S](Default(), name) }

// Init initialises the default catalog. See Catalog.Init.
func Init(ctx context.Context) error { return Default().Init(ctx) }

// Stop stops the default catalog. See Catalog.Stop.
func Stop() error { return Default().Stop() }
