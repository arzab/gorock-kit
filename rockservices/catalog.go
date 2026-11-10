// Package rockservices is the services layer of a GOROCK project: a catalog
// of services stored by name, initialised and stopped in the order you give.
//
// A project keeps one catalog in its pkg/services package:
//
//	// pkg/services/services.go
//	var catalog *rockservices.Catalog
//
//	func Init(ctx context.Context, cfg Configs) error {
//	    catalog = rockservices.New(map[string]rockservices.Service{
//	        "logger":     logger.NewService(cfg.Logger),
//	        "repository": repository.NewService(cfg.Repository),
//	    })
//	    return catalog.Init(ctx, "logger", "repository")
//	}
//
//	func Stop() error { return catalog.Stop() }
//
//	func Repository() repository.Service { return catalog.Get("repository").(repository.Service) }
package rockservices

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sort"
	"sync"
	"time"
)

const defaultStopTimeout = 30 * time.Second

// Catalog stores services by name.
type Catalog struct {
	services map[string]Service

	mu          sync.Mutex
	inited      []string // names in the order they were initialised
	stopTimeout time.Duration
}

// New creates a catalog from services by name. Constructors must not open
// connections — that belongs in Init. New panics on an empty name or a nil
// service: a wiring bug found at startup.
//
// A child service that is initialised outside its parent is passed to the
// parent's constructor:
//
//	pg := postgres.NewService(cfg.Postgres)
//	rockservices.New(map[string]rockservices.Service{
//	    "postgres":   pg,
//	    "repository": repository.NewService(cfg.Repository, pg),
//	})
func New(services map[string]Service) *Catalog {
	m := make(map[string]Service, len(services))
	for name, svc := range services {
		if name == "" {
			panic("rockservices: New: a service with an empty name")
		}
		if svc == nil {
			panic(fmt.Sprintf("rockservices: New: service %q is nil", name))
		}
		m[name] = svc
	}
	return &Catalog{services: m, stopTimeout: defaultStopTimeout}
}

// WithStopTimeout bounds Stop (default 30s). A service still stopping when it
// expires is reported by name.
func (c *Catalog) WithStopTimeout(d time.Duration) *Catalog {
	c.mu.Lock()
	c.stopTimeout = d
	c.mu.Unlock()
	return c
}

// Get returns the service stored under name. It panics if there is none.
// Projects wrap it in typed getters:
//
//	func Repository() repository.Service { return catalog.Get("repository").(repository.Service) }
func (c *Catalog) Get(name string) Service {
	svc, ok := c.services[name]
	if !ok {
		panic(fmt.Sprintf("rockservices: service %q not found", name))
	}
	return svc
}

// Names returns the names of all services, sorted.
func (c *Catalog) Names() []string {
	names := make([]string, 0, len(c.services))
	for name := range c.services {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Init initialises the named services in the given order. It panics if a
// name is unknown. If a service fails, the ones this call already
// initialised are stopped in reverse order and the error names the service.
// A service that is already initialised returns an error.
func (c *Catalog) Init(ctx context.Context, names ...string) error {
	for _, name := range names {
		c.Get(name) // unknown names panic before anything starts
	}
	var started []string
	for _, name := range names {
		c.mu.Lock()
		already := contains(c.inited, name)
		c.mu.Unlock()
		if already {
			return errors.Join(fmt.Errorf("service %q: already initialised", name), c.stopNames(reverse(started)))
		}
		if err := safeInit(ctx, c.services[name]); err != nil {
			initErr := fmt.Errorf("service %q: init: %w", name, err)
			return errors.Join(initErr, c.stopNames(reverse(started)))
		}
		c.mu.Lock()
		c.inited = append(c.inited, name)
		c.mu.Unlock()
		started = append(started, name)
	}
	return nil
}

// Stop stops services and collects every error. Without names it stops every
// initialised service in reverse init order; with names — those, in the given
// order. It panics if a name is unknown. Services that are not initialised
// are skipped, so Stop is safe to call twice. Stop is bounded by the stop
// timeout, and the error names the service that hangs.
func (c *Catalog) Stop(names ...string) error {
	for _, name := range names {
		c.Get(name)
	}
	if len(names) == 0 {
		c.mu.Lock()
		names = reverse(c.inited)
		c.mu.Unlock()
	}
	return c.stopNames(names)
}

func (c *Catalog) stopNames(names []string) error {
	c.mu.Lock()
	timeout := c.stopTimeout
	c.mu.Unlock()

	var current string
	var curMu sync.Mutex
	done := make(chan error, 1)
	go func() {
		var errs []error
		for _, name := range names {
			c.mu.Lock()
			inited := contains(c.inited, name)
			if inited {
				c.inited = remove(c.inited, name)
			}
			c.mu.Unlock()
			if !inited {
				continue
			}
			curMu.Lock()
			current = name
			curMu.Unlock()
			if err := safeStop(c.services[name]); err != nil {
				errs = append(errs, fmt.Errorf("service %q: stop: %w", name, err))
			}
		}
		done <- errors.Join(errs...)
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-timer.C:
		curMu.Lock()
		defer curMu.Unlock()
		return fmt.Errorf("rockservices: stop timed out after %s while stopping service %q", timeout, current)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func remove(list []string, s string) []string {
	out := make([]string, 0, len(list))
	for _, v := range list {
		if v != s {
			out = append(out, v)
		}
	}
	return out
}

func reverse(list []string) []string {
	out := make([]string, len(list))
	for i, v := range list {
		out[len(list)-1-i] = v
	}
	return out
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
