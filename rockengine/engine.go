// Package rockengine runs the Apps of a GOROCK project: HTTP servers, queue
// consumers, schedulers, bots. It starts them in the order you give, waits
// for a signal, the context or a failure, and stops them one by one in
// reverse order.
//
//	engine := rockengine.New(map[string]rockengine.App{
//	    "bus":  bus,
//	    "http": http,
//	})
//	return engine.Run(ctx, "bus", "http") // http starts last and stops first
package rockengine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"sync"
	"syscall"
	"time"
)

const defaultShutdownTimeout = 30 * time.Second

// Engine runs Apps stored by name.
type Engine struct {
	apps            map[string]App
	shutdownTimeout time.Duration
	restarts        int
	restartDelay    time.Duration
}

// New creates an engine from Apps by name. It panics on an empty name or a
// nil App: a wiring bug found at startup.
func New(apps map[string]App) *Engine {
	m := make(map[string]App, len(apps))
	for name, app := range apps {
		if name == "" {
			panic("rockengine: New: an app with an empty name")
		}
		if app == nil {
			panic(fmt.Sprintf("rockengine: New: app %q is nil", name))
		}
		m[name] = app
	}
	return &Engine{apps: m, shutdownTimeout: defaultShutdownTimeout}
}

// WithShutdownTimeout bounds the whole shutdown (default 30s). An App still
// stopping when it expires is reported by name.
func (e *Engine) WithShutdownTimeout(d time.Duration) *Engine {
	e.shutdownTimeout = d
	return e
}

// WithRestarts restarts an App whose Exec failed — Stop, wait delay, Init,
// Exec — up to n times before the whole engine stops. Default 0: the first
// failure stops the engine, and the orchestrator (Kubernetes, systemd,
// Docker) restarts the process.
func (e *Engine) WithRestarts(n int, delay time.Duration) *Engine {
	e.restarts, e.restartDelay = n, delay
	return e
}

// running is one App's runtime state.
type running struct {
	name string
	app  App

	lc     sync.Mutex // serialises Init and Stop of this App
	inited bool       // Init succeeded and Stop was not called yet

	cancel context.CancelFunc
	done   chan struct{} // closed when the Exec loop exits
}

// Run starts the named Apps in the given order and blocks until ctx is
// cancelled, SIGINT/SIGTERM arrives, or an App fails. It panics on an
// unknown name.
//
//   - Start: Init of each App in order; if one fails, the ones already
//     initialised are stopped in reverse order. Then Exec of each App in its
//     own goroutine.
//   - Failure: Exec returned an error (or panicked) while its context was not
//     cancelled. With WithRestarts the App is restarted first.
//   - Stop: Apps are stopped one by one in reverse order — cancel the App's
//     context, call Stop, wait for Exec — under the shutdown timeout.
//   - An Exec that returns nil before shutdown just ends; the others go on.
//
// Run returns the failure that stopped it, joined with every Stop error.
func (e *Engine) Run(ctx context.Context, names ...string) error {
	apps := make([]*running, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		app, ok := e.apps[name]
		if !ok {
			panic(fmt.Sprintf("rockengine: app %q not found", name))
		}
		if seen[name] {
			panic(fmt.Sprintf("rockengine: app %q listed twice", name))
		}
		seen[name] = true
		apps = append(apps, &running{name: name, app: app})
	}

	// App contexts do not inherit the caller's cancellation: shutdown cancels
	// them one by one, in reverse order.
	base, cancelBase := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelBase()

	for i, r := range apps {
		if err := r.init(base); err != nil {
			errs := []error{fmt.Errorf("app %q: init: %w", r.name, err)}
			for j := i - 1; j >= 0; j-- {
				errs = append(errs, apps[j].stop()...)
			}
			return errors.Join(errs...)
		}
	}

	failed := make(chan error, len(apps))
	for _, r := range apps {
		appCtx, cancel := context.WithCancel(base)
		r.cancel, r.done = cancel, make(chan struct{})
		go e.loop(appCtx, r, failed)
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)

	var cause error
	select {
	case <-ctx.Done():
	case <-quit:
	case cause = <-failed:
	}

	errs := []error{cause}
	errs = append(errs, e.shutdown(apps)...)
	return errors.Join(errs...)
}

// loop runs Exec, restarting it according to WithRestarts.
func (e *Engine) loop(ctx context.Context, r *running, failed chan<- error) {
	defer close(r.done)
	for attempt := 0; ; attempt++ {
		err := safeExec(ctx, r.app)
		if ctx.Err() != nil || err == nil {
			return // a cancelled App stops cleanly whatever Exec returned; nil = done
		}
		if attempt >= e.restarts {
			failed <- fmt.Errorf("app %q failed: %w", r.name, err)
			return
		}
		r.stop()
		select {
		case <-ctx.Done():
			return
		case <-time.After(e.restartDelay):
		}
		if err := r.init(ctx); err != nil {
			if ctx.Err() == nil {
				failed <- fmt.Errorf("app %q: restart: init: %w", r.name, err)
			}
			return
		}
	}
}

// shutdown stops Apps one by one in reverse order under the timeout.
func (e *Engine) shutdown(apps []*running) []error {
	var mu sync.Mutex
	current := ""
	done := make(chan []error, 1)
	go func() {
		var errs []error
		for i := len(apps) - 1; i >= 0; i-- {
			r := apps[i]
			mu.Lock()
			current = r.name
			mu.Unlock()
			r.cancel() // cancel first: Stop may be what unblocks Exec
			errs = append(errs, r.stop()...)
			<-r.done
		}
		done <- errs
	}()

	timer := time.NewTimer(e.shutdownTimeout)
	defer timer.Stop()
	select {
	case errs := <-done:
		return errs
	case <-timer.C:
		mu.Lock()
		defer mu.Unlock()
		return []error{fmt.Errorf("rockengine: shutdown timed out after %s while stopping app %q", e.shutdownTimeout, current)}
	}
}

// init calls Init and marks the App as needing a Stop.
func (r *running) init(ctx context.Context) (err error) {
	r.lc.Lock()
	defer r.lc.Unlock()
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("panic: %v\n%s", v, debug.Stack())
		}
	}()
	if err := r.app.Init(ctx); err != nil {
		return err
	}
	r.inited = true
	return nil
}

// stop calls Stop at most once per successful Init.
func (r *running) stop() (errs []error) {
	r.lc.Lock()
	defer r.lc.Unlock()
	if !r.inited {
		return nil
	}
	r.inited = false
	defer func() {
		if v := recover(); v != nil {
			errs = []error{fmt.Errorf("app %q: stop: panic: %v\n%s", r.name, v, debug.Stack())}
		}
	}()
	for _, err := range r.app.Stop() {
		if err != nil {
			errs = append(errs, fmt.Errorf("app %q: stop: %w", r.name, err))
		}
	}
	return errs
}

func safeExec(ctx context.Context, app App) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("panic: %v\n%s", v, debug.Stack())
		}
	}()
	return app.Exec(ctx)
}
