package rockengine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const defaultShutdownTimeout = 30 * time.Second

// entry holds the runtime state of a single registered app.
type entry struct {
	name   string
	app    App
	policy RestartPolicy

	// lc serialises Init and Stop calls of this app, so a restart (Stop → Init)
	// never interleaves with a shutdown-triggered Stop. Exec is not covered.
	lc sync.Mutex

	mu      sync.RWMutex
	state   State
	lastErr error
	retries int
	cancel  context.CancelFunc
	done    chan struct{} // closed when the current run goroutine exits; nil if never started
	// stopped reports whether Stop has been called since the last successful Init.
	// It guarantees Stop is called exactly once per Init.
	stopped bool
}

func (ent *entry) setState(s State) {
	ent.mu.Lock()
	ent.state = s
	ent.mu.Unlock()
}

// setFailed atomically sets state to failed and records the last error.
func (ent *entry) setFailed(err error) {
	ent.mu.Lock()
	ent.state = StateFailed
	ent.lastErr = err
	ent.mu.Unlock()
}

// Engine manages the lifecycle of registered apps.
type Engine struct {
	entries map[string]*entry
	order   []string // registration order

	mu              sync.RWMutex
	shutdownTimeout time.Duration
	onFatal         func(name string, err error)
	stopOnFatal     bool
	running         bool
	stopping        bool

	// baseCtx is the parent of every app context. It does not inherit
	// cancellation from the caller's ctx, so apps can be cancelled one by one
	// in reverse order during shutdown.
	baseCtx    context.Context
	baseCancel context.CancelFunc

	fatalOnce sync.Once
	fatalCh   chan struct{} // closed when an app fails and stopOnFatal is set
	fatalErr  error

	stoppingApp atomic.Value // string: app currently being stopped, for timeout errors
}

// NewEngine creates a new Engine.
func NewEngine() *Engine {
	return &Engine{
		entries:         make(map[string]*entry),
		shutdownTimeout: defaultShutdownTimeout,
		stopOnFatal:     true,
	}
}

// WithShutdownTimeout sets the maximum time to wait for all apps to stop,
// including their Stop calls.
func (e *Engine) WithShutdownTimeout(d time.Duration) *Engine {
	e.mu.Lock()
	e.shutdownTimeout = d
	e.mu.Unlock()
	return e
}

// WithFatalHandler sets a callback invoked when an app is marked as failed
// (restart limit exceeded or re-init failed).
func (e *Engine) WithFatalHandler(fn func(name string, err error)) *Engine {
	e.mu.Lock()
	e.onFatal = fn
	e.mu.Unlock()
	return e
}

// WithStopOnFatal controls what happens when an app is marked as failed.
//
// true (default): the engine shuts down all apps and RunContext returns an
// error naming the failed app. Use this when a process with a dead component
// should be restarted by the orchestrator.
//
// false: the engine keeps running the remaining apps; observe failures via
// WithFatalHandler or AppStatuses.
func (e *Engine) WithStopOnFatal(stop bool) *Engine {
	e.mu.Lock()
	e.stopOnFatal = stop
	e.mu.Unlock()
	return e
}

// Register adds a named app with an optional restart policy.
// Returns an error if the name is already registered, app is nil, or the engine is running.
// Returns the Engine for chaining.
func (e *Engine) Register(name string, app App, policy ...RestartPolicy) (*Engine, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if app == nil {
		return e, fmt.Errorf("cannot register app %q: app is nil", name)
	}
	if e.running {
		return e, fmt.Errorf("cannot register app %q: engine is already running", name)
	}
	if _, exists := e.entries[name]; exists {
		return e, fmt.Errorf("app %q is already registered", name)
	}

	var p RestartPolicy
	if len(policy) > 0 {
		p = policy[0]
	}

	e.entries[name] = &entry{
		name:    name,
		app:     app,
		policy:  p,
		state:   StateIdle,
		cancel:  func() {}, // no-op until started
		stopped: true,      // nothing to stop until Init succeeds
	}
	e.order = append(e.order, name)
	return e, nil
}

// MustRegister is like Register but panics on error.
func (e *Engine) MustRegister(name string, app App, policy ...RestartPolicy) *Engine {
	if _, err := e.Register(name, app, policy...); err != nil {
		panic(err)
	}
	return e
}

// Run starts the engine with a background context.
func (e *Engine) Run() error {
	return e.RunContext(context.Background())
}

// RunContext initialises all apps sequentially, then runs each in a goroutine.
// Blocks until ctx is cancelled, SIGINT/SIGTERM is received, or (with
// WithStopOnFatal, the default) an app is marked as failed.
//
// Shutdown stops apps one by one in reverse registration order: each app's
// context is cancelled, its Stop is called, and its Exec is awaited before the
// next app is stopped. The whole shutdown is bounded by the shutdown timeout.
//
// Returns the init error, the fatal app error (if that caused the shutdown),
// and any Stop errors, joined.
func (e *Engine) RunContext(ctx context.Context) error {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return fmt.Errorf("engine is already running")
	}
	e.running = true
	e.stopping = false
	order := make([]string, len(e.order))
	copy(order, e.order)
	baseCtx, baseCancel := context.WithCancel(context.WithoutCancel(ctx))
	e.baseCtx = baseCtx
	e.baseCancel = baseCancel
	e.fatalOnce = sync.Once{}
	e.fatalCh = make(chan struct{})
	e.fatalErr = nil
	fatalCh := e.fatalCh
	timeout := e.shutdownTimeout
	e.mu.Unlock()

	defer func() {
		baseCancel()
		e.mu.Lock()
		e.running = false
		e.mu.Unlock()
	}()

	// Init sequentially to respect dependency order.
	// On failure (including panic), stop already-initialised apps in reverse order.
	var initDone []string
	for _, name := range order {
		ent := e.entries[name]
		ent.lc.Lock()
		err := safeInit(baseCtx, ent)
		ent.lc.Unlock()
		if err != nil {
			e.stopInited(initDone)
			return fmt.Errorf("app %q: init: %w", name, err)
		}
		initDone = append(initDone, name)
	}

	for _, name := range order {
		e.startApp(name)
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)

	select {
	case <-ctx.Done():
	case <-quit:
	case <-fatalCh:
	}

	e.mu.Lock()
	e.stopping = true
	e.mu.Unlock()

	var errs []error

	shutdownDone := make(chan []error, 1)
	go func() { shutdownDone <- e.stopAll(order) }()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case stopErrs := <-shutdownDone:
		errs = append(errs, stopErrs...)
	case <-timer.C:
		name, _ := e.stoppingApp.Load().(string)
		errs = append(errs, fmt.Errorf("shutdown timed out after %s while stopping app %q", timeout, name))
	}

	e.mu.RLock()
	fatalErr := e.fatalErr
	e.mu.RUnlock()
	if fatalErr != nil {
		errs = append([]error{fatalErr}, errs...)
	}

	return errors.Join(errs...)
}

// safeInit calls app.Init, converting a panic to an error.
// On success the app is marked as needing a Stop. Caller must hold ent.lc.
func safeInit(ctx context.Context, ent *entry) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v\n%s", r, debug.Stack())
		}
	}()
	if err := ent.app.Init(ctx); err != nil {
		return err
	}
	ent.mu.Lock()
	ent.stopped = false
	ent.mu.Unlock()
	return nil
}

// safeStop calls app.Stop at most once per successful Init, converting a panic
// to an error. Caller must hold ent.lc.
func safeStop(ent *entry) (errs []error) {
	ent.mu.Lock()
	if ent.stopped {
		ent.mu.Unlock()
		return nil
	}
	ent.stopped = true
	ent.mu.Unlock()

	defer func() {
		if r := recover(); r != nil {
			errs = append(errs, fmt.Errorf("app %q: stop: panic: %v\n%s", ent.name, r, debug.Stack()))
		}
	}()
	return ent.app.Stop()
}

// safeExec calls app.Exec, converting a panic to an error so it goes through
// the restart policy like any other failure.
func safeExec(ctx context.Context, app App) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v\n%s", r, debug.Stack())
		}
	}()
	return app.Exec(ctx)
}

// stopInited stops already-initialised apps in reverse order after an Init failure.
func (e *Engine) stopInited(names []string) {
	for i := len(names) - 1; i >= 0; i-- {
		ent := e.entries[names[i]]
		ent.lc.Lock()
		safeStop(ent)
		ent.lc.Unlock()
	}
}

// startApp creates a per-app context and launches its run goroutine.
func (e *Engine) startApp(name string) {
	e.mu.RLock()
	baseCtx := e.baseCtx
	e.mu.RUnlock()

	ent := e.entries[name]
	appCtx, appCancel := context.WithCancel(baseCtx)
	done := make(chan struct{})

	ent.mu.Lock()
	ent.cancel = appCancel
	ent.done = done
	ent.mu.Unlock()

	go e.runApp(appCtx, ent, done)
}

// runApp runs an app in a retry loop according to its restart policy.
func (e *Engine) runApp(ctx context.Context, ent *entry, done chan struct{}) {
	defer close(done)
	policy := ent.policy

	for {
		if ctx.Err() != nil {
			ent.setState(StateStopped)
			return
		}

		ent.setState(StateRunning)
		started := time.Now()
		err := safeExec(ctx, ent.app)

		// Context cancelled — always a clean stop, whatever Exec returned
		// (http.ErrServerClosed, a wrapped ctx error, nil, ...).
		if ctx.Err() != nil {
			ent.setState(StateStopped)
			return
		}

		if err == nil {
			// Clean exit. Restart only if RestartOnExit is set.
			if !policy.RestartOnExit {
				ent.setState(StateStopped)
				return
			}
		} else {
			ent.mu.Lock()
			if policy.ResetAfter > 0 && time.Since(started) >= policy.ResetAfter {
				ent.retries = 0
			}
			ent.lastErr = err
			ent.retries++
			retries := ent.retries
			ent.mu.Unlock()

			canRestart := policy.MaxRetries < 0 || retries <= policy.MaxRetries
			if !canRestart {
				e.fail(ent, fmt.Errorf("exceeded retries (%d): %w", retries, err))
				return
			}
		}

		if ok := e.doRestart(ctx, ent); !ok {
			return
		}
	}
}

// doRestart performs the Stop → delay → Init sequence before the next Exec.
// Returns false if the restart should be aborted (ctx cancelled or Init failed).
func (e *Engine) doRestart(ctx context.Context, ent *entry) bool {
	ent.setState(StateRestarting)

	ent.lc.Lock()
	safeStop(ent) //nolint:errcheck // stop errors during restart are not actionable
	ent.lc.Unlock()

	if d := ent.policy.Delay; d > 0 {
		timer := time.NewTimer(d)
		select {
		case <-ctx.Done():
			timer.Stop()
			ent.setState(StateStopped)
			return false
		case <-timer.C:
		}
	}

	ent.lc.Lock()
	if ctx.Err() != nil {
		ent.lc.Unlock()
		ent.setState(StateStopped)
		return false
	}
	initErr := safeInit(ctx, ent)
	ent.lc.Unlock()

	if initErr != nil {
		if ctx.Err() != nil {
			ent.setState(StateStopped)
			return false
		}
		e.fail(ent, fmt.Errorf("re-init failed: %w", initErr))
		return false
	}
	return true
}

// fail marks the app as failed, releases its resources, notifies the fatal
// handlers, and — with stopOnFatal — triggers engine shutdown.
func (e *Engine) fail(ent *entry, err error) {
	ent.setFailed(err)

	ent.lc.Lock()
	safeStop(ent) //nolint:errcheck // the app already failed; its stop errors add nothing
	ent.lc.Unlock()

	e.notifyFatal(ent, err)

	e.mu.Lock()
	stopOnFatal := e.stopOnFatal && e.running
	fatalCh := e.fatalCh
	e.mu.Unlock()

	if stopOnFatal {
		e.fatalOnce.Do(func() {
			e.mu.Lock()
			e.fatalErr = fmt.Errorf("app %q failed: %w", ent.name, err)
			e.mu.Unlock()
			close(fatalCh)
		})
	}
}

// notifyFatal calls the per-app OnFatal handler if set, otherwise falls back
// to the engine-level fatal handler. A panicking handler is recovered.
func (e *Engine) notifyFatal(ent *entry, err error) {
	defer func() {
		if r := recover(); r != nil {
			_, _ = fmt.Fprintf(os.Stderr, "rockengine: fatal handler for app %q panicked: %v\n%s\n", ent.name, r, debug.Stack())
		}
	}()
	if ent.policy.OnFatal != nil {
		ent.policy.OnFatal(err)
		return
	}
	e.mu.RLock()
	onFatal := e.onFatal
	e.mu.RUnlock()
	if onFatal != nil {
		onFatal(ent.name, err)
	}
}

// stopEntry cancels an app's context, calls Stop (once per Init), and waits
// for its run goroutine to exit.
func (e *Engine) stopEntry(ent *entry) []error {
	ent.mu.RLock()
	cancel := ent.cancel
	done := ent.done
	ent.mu.RUnlock()

	// Cancel first, then Stop — Stop may be needed to unblock a blocking Exec
	// (e.g. an HTTP server).
	cancel()

	ent.lc.Lock()
	errs := safeStop(ent)
	ent.lc.Unlock()

	if done != nil {
		<-done
	}
	return errs
}

// StopApp stops a single app without affecting the others.
func (e *Engine) StopApp(name string) error {
	e.mu.RLock()
	ent, ok := e.entries[name]
	e.mu.RUnlock()
	if !ok {
		return fmt.Errorf("app %q not found", name)
	}
	return errors.Join(e.stopEntry(ent)...)
}

// RestartApp stops an app and restarts it from Init.
// Returns an error if the engine is not running or is shutting down.
func (e *Engine) RestartApp(name string) error {
	if err := e.checkAcceptingRestarts(); err != nil {
		return err
	}

	ent, ok := e.entries[name]
	if !ok {
		return fmt.Errorf("app %q not found", name)
	}

	if err := e.StopApp(name); err != nil {
		return err
	}

	// Re-check after StopApp — the engine may have begun shutting down meanwhile.
	if err := e.checkAcceptingRestarts(); err != nil {
		return err
	}

	e.mu.RLock()
	baseCtx := e.baseCtx
	e.mu.RUnlock()

	ent.mu.Lock()
	ent.retries = 0
	ent.lastErr = nil
	ent.mu.Unlock()

	ent.lc.Lock()
	err := safeInit(baseCtx, ent)
	ent.lc.Unlock()
	if err != nil {
		ent.setFailed(fmt.Errorf("init: %w", err))
		return fmt.Errorf("app %q: init: %w", name, err)
	}

	e.startApp(name)
	return nil
}

func (e *Engine) checkAcceptingRestarts() error {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if !e.running {
		return fmt.Errorf("engine is not running")
	}
	if e.stopping {
		return fmt.Errorf("engine is shutting down")
	}
	return nil
}

// AppStatus returns the current runtime status of the named app.
func (e *Engine) AppStatus(name string) (AppInfo, error) {
	e.mu.RLock()
	ent, ok := e.entries[name]
	e.mu.RUnlock()
	if !ok {
		return AppInfo{}, fmt.Errorf("app %q not found", name)
	}

	ent.mu.RLock()
	defer ent.mu.RUnlock()
	return AppInfo{
		Name:    name,
		State:   ent.state,
		Err:     ent.lastErr,
		Retries: ent.retries,
	}, nil
}

// AppStatuses returns the status of all apps in registration order.
func (e *Engine) AppStatuses() []AppInfo {
	e.mu.RLock()
	order := make([]string, len(e.order))
	copy(order, e.order)
	e.mu.RUnlock()

	infos := make([]AppInfo, 0, len(order))
	for _, name := range order {
		if info, err := e.AppStatus(name); err == nil {
			infos = append(infos, info)
		}
	}
	return infos
}

// Shutdown stops all apps in reverse registration order.
func (e *Engine) Shutdown() []error {
	e.mu.RLock()
	order := make([]string, len(e.order))
	copy(order, e.order)
	e.mu.RUnlock()
	return e.stopAll(order)
}

// stopAll stops apps one by one in reverse registration order, so an app is
// fully stopped before the apps it depends on (registered earlier) are touched.
func (e *Engine) stopAll(order []string) []error {
	var errs []error
	for i := len(order) - 1; i >= 0; i-- {
		name := order[i]
		e.stoppingApp.Store(name)
		errs = append(errs, e.stopEntry(e.entries[name])...)
	}
	e.stoppingApp.Store("")
	return errs
}
