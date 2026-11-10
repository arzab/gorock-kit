package rockservices

import (
	"fmt"
	"reflect"
	"sync"
)

// Key is the typed name of a service. Declare every key once, next to the
// registration in pkg/services/services.go:
//
//	var Orders = rockservices.NewKey[orders.Service]("orders")
//
//	rockservices.Add(c, Orders, orders.NewService, cfg.Orders) // the constructor's type is checked against the key
//	services.Orders.Get().Insert(ctx, order)                   // typed, no cast
//
// Declared keys are remembered: Init of the default catalog fails if a key
// was declared but its service was never added, so a forgotten registration
// stops the project at startup instead of panicking on the first request.
type Key[S Service] struct {
	name string
}

// declared holds every key created by NewKey, for the startup check.
var (
	declaredMu sync.Mutex
	declared   []declaredKey
)

type declaredKey struct {
	id   any // the *Key[S]
	name string
}

// NewKey declares a key. It panics on an empty name.
func NewKey[S Service](name string) *Key[S] {
	if name == "" {
		panic(fmt.Sprintf("rockservices: NewKey[%s] with an empty name", reflect.TypeFor[S]()))
	}
	k := &Key[S]{name: name}
	declaredMu.Lock()
	declared = append(declared, declaredKey{id: k, name: name})
	declaredMu.Unlock()
	return k
}

// Name returns the name the service is stored under.
func (k *Key[S]) Name() string { return k.name }

// String returns the name.
func (k *Key[S]) String() string { return k.name }

// Get returns the service from the default catalog.
//
// It panics if the service is not in the catalog. With the default catalog
// that cannot happen after a successful Init (Init checks every declared
// key); it can when a test swaps in a catalog from New() without that
// service.
func (k *Key[S]) Get() S { return k.From(Default()) }

// From returns the service from catalog c.
func (k *Key[S]) From(c *Catalog) S {
	c.mu.RLock()
	e, ok := c.byName[k.name]
	c.mu.RUnlock()
	if !ok {
		panic(fmt.Sprintf("rockservices: service %q is not in the catalog (add it in services.Register)", k.name))
	}
	svc, ok := e.svc.(S)
	if !ok {
		panic(fmt.Sprintf("rockservices: service %q is %T, not %s", k.name, e.svc, reflect.TypeFor[S]()))
	}
	return svc
}

// missingKeys returns the names of declared keys that are not in c.
func (c *Catalog) missingKeys() []string {
	declaredMu.Lock()
	keys := append([]declaredKey(nil), declared...)
	declaredMu.Unlock()

	c.mu.RLock()
	defer c.mu.RUnlock()
	var missing []string
	for _, k := range keys {
		if _, ok := c.byKey[k.id]; !ok {
			missing = append(missing, k.name)
		}
	}
	return missing
}
