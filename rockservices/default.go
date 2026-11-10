package rockservices

import (
	"context"
	"sync/atomic"
)

// defaultCatalog is the project-wide catalog behind the package-level functions.
var defaultCatalog atomic.Pointer[Catalog]

func init() {
	c := New()
	c.checkDeclared = true
	defaultCatalog.Store(c)
}

// Default returns the project-wide catalog. Pass it to services.Register.
func Default() *Catalog { return defaultCatalog.Load() }

// SetDefault replaces the project-wide catalog and returns the previous one.
// It exists for tests: build a catalog with fakes, make it the default, and
// restore the old one afterwards, so code that calls key.Get() sees the fakes.
//
//	c := rockservices.New()
//	rockservices.Add(c, services.Orders, func(struct{}) orders.Service { return &fakeOrders{} }, struct{}{})
//	t.Cleanup(func() { rockservices.SetDefault(rockservices.SetDefault(c)) })
//
// Tests that swap the default catalog must not run with t.Parallel(): the
// default catalog is one per process.
//
// Don't call it in production code: services.Register fills Default().
func SetDefault(c *Catalog) (previous *Catalog) {
	if c == nil {
		panic("rockservices: SetDefault(nil)")
	}
	return defaultCatalog.Swap(c)
}

// Init initialises the default catalog. See Catalog.Init.
func Init(ctx context.Context) error { return Default().Init(ctx) }

// Stop stops the default catalog. See Catalog.Stop.
func Stop() error { return Default().Stop() }
