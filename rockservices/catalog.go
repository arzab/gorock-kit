// Package rockservices is the services layer of a GOROCK project: one catalog
// that creates, stores, hands out, initialises and stops every service.
//
// Services are looked up by typed keys declared next to the registration:
//
//	// pkg/services/services.go
//	var Orders = rockservices.NewKey[orders.Service]("orders")
//
//	func Register(c *rockservices.Catalog, cfg Configs) {
//	    rockservices.Add(c, Orders, orders.NewService, cfg.Orders)
//	}
//
//	// anywhere
//	services.Orders.Get().Insert(ctx, order)
//
// Services are independent of apps. cmd/main initialises the catalog before
// the apps start and stops it after they have stopped:
//
//	services.Register(rockservices.Default(), cfg.Services)
//	if err := rockservices.Init(ctx); err != nil {
//	    return err
//	}
//	defer rockservices.Stop()
//
//	engine := rockengine.NewEngine()
//	apps.Register(engine, cfg.Apps)
//	return engine.Run()
package rockservices

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

// ErrAlreadyStarted is returned by a second Init of the same catalog.
var ErrAlreadyStarted = errors.New("rockservices: catalog already initialised")

const defaultStopTimeout = 30 * time.Second

type entry struct {
	name   string
	key    any // the *Key[S] it was added under
	svc    Service
	inited bool
}

// Catalog holds services by key, in the order they were added.
// Init order is add order; Stop runs in reverse.
type Catalog struct {
	mu          sync.RWMutex
	order       []*entry
	byName      map[string]*entry
	byKey       map[any]*entry
	started     bool // Init was called; no more Add
	stopTimeout time.Duration

	// checkDeclared: Init fails if a declared key was not added. Only the
	// built-in default catalog checks; catalogs from New() (tests) don't.
	checkDeclared bool
}

// New creates an empty catalog. Projects normally use Default; New is for
// tests and for running several isolated sets of services.
func New() *Catalog {
	return &Catalog{byName: make(map[string]*entry), byKey: make(map[any]*entry), stopTimeout: defaultStopTimeout}
}

// WithStopTimeout bounds Stop (default 30s). A service that is still stopping
// when it expires is reported by name.
func (c *Catalog) WithStopTimeout(d time.Duration) *Catalog {
	c.mu.Lock()
	c.stopTimeout = d
	c.mu.Unlock()
	return c
}

// Add calls ctor(cfg) right away and stores the result under key. The
// constructor's service type must match the key's type — checked at compile
// time. The name belongs to the key, not to the service: declare two keys to
// add the same service twice.
//
//	rockservices.Add(c, Orders, orders.NewService, cfg.Orders)
//
// Constructors must not open connections: that belongs in Init.
//
// To wire a dependency, pass the value Add returned for it:
//
//	pg := rockservices.Add(c, Postgres, postgres.NewService, cfg.Postgres)
//	rockservices.Add(c, Orders, func(cfg orders.Configs) orders.Service {
//	    return orders.NewService(cfg, pg)
//	}, cfg.Orders)
//
// Don't call key.Get() inside a constructor: it reads Default(), not the
// catalog c being filled, so Register(rockservices.New(), cfg) would wire the
// wrong service or panic.
//
// Add panics on a nil key, an empty or duplicate name, a nil service, a
// constructor panic, or a call after Init: all of them are wiring bugs found
// at startup.
func Add[C any, S Service](c *Catalog, key *Key[S], ctor func(C) S, cfg C) S {
	kind := reflect.TypeFor[S]().String()
	if key == nil {
		panic(fmt.Sprintf("rockservices: %s added with a nil key", kind))
	}
	name := key.name
	if name == "" {
		panic(fmt.Sprintf("rockservices: %s added with an empty name", kind))
	}
	c.mu.RLock()
	started := c.started
	_, dup := c.byName[name]
	c.mu.RUnlock()
	if started {
		panic(fmt.Sprintf("rockservices: service %q (%s) added after Init", name, kind))
	}
	if dup {
		panic(fmt.Sprintf("rockservices: service %q added twice", name))
	}

	// Construct without holding the lock: the constructor may Get earlier services.
	svc := construct(name, kind, ctor, cfg)
	if any(svc) == nil {
		panic(fmt.Sprintf("rockservices: constructor of service %q (%s) returned nil", name, kind))
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if _, dup := c.byName[name]; dup {
		panic(fmt.Sprintf("rockservices: service %q added twice", name))
	}
	e := &entry{name: name, key: key, svc: svc}
	c.order = append(c.order, e)
	c.byName[name] = e
	c.byKey[key] = e
	return svc
}

func construct[C any, S Service](name, kind string, ctor func(C) S, cfg C) S {
	defer func() {
		if r := recover(); r != nil {
			panic(fmt.Sprintf("rockservices: constructing service %q (%s): %v", name, kind, r))
		}
	}()
	return ctor(cfg)
}

// Names returns the service names in add order.
func (c *Catalog) Names() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	names := make([]string, len(c.order))
	for i, e := range c.order {
		names[i] = e.name
	}
	return names
}

// Init initialises services in add order. If one fails, the ones already
// initialised are stopped in reverse order and the error names the service.
// A catalog is initialised once: a second Init returns ErrAlreadyStarted.
//
// The default catalog first checks that every key declared with NewKey was
// added, and fails naming the missing ones — a forgotten registration stops
// the project at startup. Catalogs from New() skip this check: tests put only
// the fakes they need there.
func (c *Catalog) Init(ctx context.Context) error {
	if c.checkDeclared {
		if missing := c.missingKeys(); len(missing) > 0 {
			return fmt.Errorf("rockservices: services declared but not added in services.Register: %q", missing)
		}
	}
	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return ErrAlreadyStarted
	}
	c.started = true
	order := append([]*entry(nil), c.order...)
	c.mu.Unlock()

	for _, e := range order {
		if err := safeInit(ctx, e.svc); err != nil {
			initErr := fmt.Errorf("service %q: init: %w", e.name, err)
			return errors.Join(append([]error{initErr}, c.stop()...)...)
		}
		c.mu.Lock()
		e.inited = true
		c.mu.Unlock()
	}
	return nil
}

// Stop stops initialised services in reverse order, collecting every error.
// Safe to call more than once and when Init failed.
func (c *Catalog) Stop() error {
	c.mu.RLock()
	timeout := c.stopTimeout
	c.mu.RUnlock()

	var current atomic.Pointer[string] // the service being stopped, for the timeout error
	done := make(chan []error, 1)
	go func() { done <- c.stopTracking(&current) }()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case errs := <-done:
		return errors.Join(errs...)
	case <-timer.C:
		name := ""
		if p := current.Load(); p != nil {
			name = *p
		}
		return fmt.Errorf("rockservices: stop timed out after %s while stopping service %q", timeout, name)
	}
}

func (c *Catalog) stop() []error { return c.stopTracking(nil) }

func (c *Catalog) stopTracking(current *atomic.Pointer[string]) []error {
	c.mu.Lock()
	order := append([]*entry(nil), c.order...)
	c.mu.Unlock()

	var errs []error
	for i := len(order) - 1; i >= 0; i-- {
		e := order[i]
		c.mu.Lock()
		inited := e.inited
		e.inited = false
		c.mu.Unlock()
		if !inited {
			continue
		}
		if current != nil {
			name := e.name
			current.Store(&name)
		}
		if err := safeStop(e.svc); err != nil {
			errs = append(errs, fmt.Errorf("service %q: stop: %w", e.name, err))
		}
	}
	return errs
}

func safeInit(ctx context.Context, s Service) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v\n%s", r, debug.Stack())
		}
	}()
	return s.Init(ctx)
}

func safeStop(s Service) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v\n%s", r, debug.Stack())
		}
	}()
	return s.Stop()
}
