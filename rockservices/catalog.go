// Package rockservices is the services layer of a GOROCK project: one catalog
// that creates, stores, hands out, initialises and stops every service.
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
	"time"
)

const defaultStopTimeout = 30 * time.Second

type entry struct {
	name   string
	svc    Service
	inited bool
}

// Catalog holds services by name, in the order they were added.
// Init order is add order; Stop runs in reverse.
type Catalog struct {
	mu          sync.RWMutex
	order       []*entry
	byName      map[string]*entry
	started     bool // Init was called; no more Add
	stopTimeout time.Duration
}

// New creates an empty catalog. Projects normally use Default; New is for
// tests and for running several isolated sets of services.
func New() *Catalog {
	return &Catalog{byName: make(map[string]*entry), stopTimeout: defaultStopTimeout}
}

// WithStopTimeout bounds Stop (default 30s). A service that is still stopping
// when it expires is reported by name.
func (c *Catalog) WithStopTimeout(d time.Duration) *Catalog {
	c.mu.Lock()
	c.stopTimeout = d
	c.mu.Unlock()
	return c
}

// Add calls ctor(cfg) right away and stores the result under svc.Name().
// The constructor may Get services added before it — that is how shared
// dependencies are wired; add dependencies first. Constructors must not open
// connections: that belongs in Init.
//
//	rockservices.Add(c, repository.NewService, cfg.Repository)
//
// Add panics on a nil service, an empty or duplicate name, a constructor
// panic, or a call after Init: all of them are wiring bugs found at startup.
func Add[C any, S Service](c *Catalog, ctor func(C) S, cfg C) S {
	kind := reflect.TypeFor[S]().String()
	c.mu.RLock()
	started := c.started
	c.mu.RUnlock()
	if started {
		panic(fmt.Sprintf("rockservices: %s added after Init", kind))
	}

	// Construct without holding the lock: the constructor may Get earlier services.
	svc := construct(kind, ctor, cfg)
	if any(svc) == nil {
		panic(fmt.Sprintf("rockservices: constructor of %s returned nil", kind))
	}
	name := svc.Name()
	if name == "" {
		panic(fmt.Sprintf("rockservices: %s has an empty Name()", kind))
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if _, dup := c.byName[name]; dup {
		panic(fmt.Sprintf("rockservices: service %q added twice", name))
	}
	e := &entry{name: name, svc: svc}
	c.order = append(c.order, e)
	c.byName[name] = e
	return svc
}

func construct[C any, S Service](kind string, ctor func(C) S, cfg C) S {
	defer func() {
		if r := recover(); r != nil {
			panic(fmt.Sprintf("rockservices: constructing %s: %v", kind, r))
		}
	}()
	return ctor(cfg)
}

// GetFrom returns the service stored under name as type S.
// It panics if there is no such service or it is not an S.
func GetFrom[S any](c *Catalog, name string) S {
	c.mu.RLock()
	e, ok := c.byName[name]
	c.mu.RUnlock()
	if !ok {
		panic(fmt.Sprintf("rockservices: service %q not found (add it in services.Register before it is used)", name))
	}
	svc, ok := e.svc.(S)
	if !ok {
		var want S
		panic(fmt.Sprintf("rockservices: service %q is %T, not %T", name, e.svc, &want))
	}
	return svc
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
func (c *Catalog) Init(ctx context.Context) error {
	c.mu.Lock()
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

	var current sync.Map // "name" -> service being stopped, for the timeout error
	done := make(chan []error, 1)
	go func() { done <- c.stopTracking(&current) }()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case errs := <-done:
		return errors.Join(errs...)
	case <-timer.C:
		name, _ := current.Load("name")
		return fmt.Errorf("rockservices: stop timed out after %s while stopping service %q", timeout, name)
	}
}

func (c *Catalog) stop() []error { return c.stopTracking(nil) }

func (c *Catalog) stopTracking(current *sync.Map) []error {
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
			current.Store("name", e.name)
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
