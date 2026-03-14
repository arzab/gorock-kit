package rockengine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

const defaultShutdownTimeout = 30 * time.Second

// entry holds the runtime state of a single registered app.
type entry struct {
	app    App
	policy RestartPolicy

	mu      sync.RWMutex
	state   State
	lastErr error
	retries int
	cancel  context.CancelFunc
	wg      sync.WaitGroup
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
	running         bool

	engineCtx    context.Context
	engineCancel context.CancelFunc
}

// NewEngine creates a new Engine.
func NewEngine() *Engine {
	return &Engine{
		entries:         make(map[string]*entry),
		shutdownTimeout: defaultShutdownTimeout,
	}
}

// WithShutdownTimeout sets the maximum time to wait for all apps to stop.
func (e *Engine) WithShutdownTimeout(d time.Duration) *Engine {
	e.shutdownTimeout = d
	return e
}

// WithFatalHandler sets a callback invoked when an app exceeds its restart limit.
// The engine keeps running; use this to observe or react to fatal app failures.
func (e *Engine) WithFatalHandler(fn func(name string, err error)) *Engine {
	e.onFatal = fn
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
		app:    app,
		policy: p,
		state:  StateIdle,
		cancel: func() {}, // no-op until started
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
// Blocks until ctx is cancelled or SIGINT/SIGTERM is received.
// Returns an error if the engine is already running.
// App failures are handled per-app restart policy and do not stop the engine.
func (e *Engine) RunContext(ctx context.Context) error {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return fmt.Errorf("engine is already running")
	}
	e.running = true
	order := make([]string, len(e.order))
	copy(order, e.order)
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		e.running = false
		e.mu.Unlock()
	}()

	engineCtx, engineCancel := context.WithCancel(ctx)
	defer engineCancel()

	e.mu.Lock()
	e.engineCtx = engineCtx
	e.engineCancel = engineCancel
	e.mu.Unlock()

	// Init sequentially to respect dependency order.
	// On failure (including panic), stop already-initialised apps in reverse order.
	var initDone []string
	for _, name := range order {
		if err := e.safeInit(engineCtx, name); err != nil {
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
	case <-engineCtx.Done():
	case <-quit:
	}

	engineCancel()

	shutdownErrs := e.stopAll(order)

	// Wait for all goroutines with timeout.
	done := make(chan struct{})
	go func() {
		for _, name := range order {
			e.entries[name].wg.Wait()
		}
		close(done)
	}()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), e.shutdownTimeout)
	defer shutdownCancel()

	select {
	case <-done:
	case <-shutdownCtx.Done():
		shutdownErrs = append(shutdownErrs, errors.New("shutdown timed out"))
	}

	return errors.Join(shutdownErrs...)
}

// safeInit calls app.Init catching any panic and converting it to an error.
func (e *Engine) safeInit(ctx context.Context, name string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return e.entries[name].app.Init(ctx)
}

// stopInited stops already-initialised apps in reverse order after an Init failure.
func (e *Engine) stopInited(names []string) {
	for i := len(names) - 1; i >= 0; i-- {
		e.entries[names[i]].app.Stop() //nolint:errcheck
	}
}

// startApp creates a per-app context and launches its run goroutine.
func (e *Engine) startApp(name string) {
	e.mu.RLock()
	engineCtx := e.engineCtx
	e.mu.RUnlock()

	ent := e.entries[name]
	appCtx, appCancel := context.WithCancel(engineCtx)

	ent.mu.Lock()
	ent.cancel = appCancel
	ent.mu.Unlock()

	ent.wg.Add(1)
	go e.runApp(appCtx, name, ent)
}

// runApp runs an app in a retry loop according to its restart policy.
func (e *Engine) runApp(ctx context.Context, name string, ent *entry) {
	defer ent.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			err := fmt.Errorf("panic: %v", r)
			ent.setFailed(err)
			ent.mu.RLock()
			policy := ent.policy
			ent.mu.RUnlock()
			e.notifyFatal(policy, name, err)
		}
	}()

	for {
		ent.mu.RLock()
		policy := ent.policy
		ent.mu.RUnlock()

		ent.setState(StateRunning)
		err := ent.app.Exec(ctx)

		// ctx cancelled — always a clean stop, no restart.
		if errors.Is(err, context.Canceled) {
			ent.setState(StateStopped)
			return
		}

		// Exec returned nil: clean exit.
		// Restart only if RestartOnExit is set, otherwise treat as stopped.
		if err == nil {
			if !policy.RestartOnExit {
				ent.setState(StateStopped)
				return
			}
			if ok := e.doRestart(ctx, name, ent, policy); !ok {
				return
			}
			continue
		}

		// Exec returned an error.
		ent.mu.Lock()
		ent.lastErr = err
		ent.retries++
		retries := ent.retries
		ent.mu.Unlock()

		canRestart := policy.MaxRetries < 0 || retries <= policy.MaxRetries
		if !canRestart {
			fatalErr := fmt.Errorf("exceeded retries (%d): %w", retries, err)
			ent.setFailed(fatalErr)
			e.notifyFatal(policy, name, fatalErr)
			return
		}

		if ok := e.doRestart(ctx, name, ent, policy); !ok {
			return
		}
	}
}

// doRestart performs the Stop → delay → Init sequence before the next Exec.
// Returns false if the restart should be aborted (ctx cancelled or Init failed).
func (e *Engine) doRestart(ctx context.Context, name string, ent *entry, policy RestartPolicy) bool {
	ent.setState(StateRestarting)
	ent.app.Stop() //nolint:errcheck

	if policy.Delay > 0 {
		timer := time.NewTimer(policy.Delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			ent.setState(StateStopped)
			return false
		case <-timer.C:
		}
	} else if ctx.Err() != nil {
		ent.setState(StateStopped)
		return false
	}

	if initErr := ent.app.Init(ctx); initErr != nil {
		if errors.Is(initErr, context.Canceled) {
			ent.setState(StateStopped)
		} else {
			fatalErr := fmt.Errorf("re-init failed: %w", initErr)
			ent.setFailed(fatalErr)
			e.notifyFatal(policy, name, fatalErr)
		}
		return false
	}

	return true
}

// notifyFatal calls the per-app OnFatal handler if set, otherwise falls back
// to the engine-level fatal handler.
func (e *Engine) notifyFatal(policy RestartPolicy, name string, err error) {
	if policy.OnFatal != nil {
		policy.OnFatal(err)
		return
	}
	if e.onFatal != nil {
		e.onFatal(name, err)
	}
}

// StopApp stops a single app without affecting the others.
func (e *Engine) StopApp(name string) error {
	e.mu.RLock()
	ent, ok := e.entries[name]
	e.mu.RUnlock()
	if !ok {
		return fmt.Errorf("app %q not found", name)
	}

	ent.mu.RLock()
	cancel := ent.cancel
	ent.mu.RUnlock()

	// Cancel context and call Stop — Stop may be needed to unblock
	// a blocking Exec (e.g. HTTP server).
	cancel()
	errs := ent.app.Stop()
	ent.wg.Wait()

	return errors.Join(errs...)
}

// RestartApp stops an app and restarts it from Init.
// Returns an error if the engine is not running.
func (e *Engine) RestartApp(name string) error {
	e.mu.RLock()
	running := e.running
	engineCtx := e.engineCtx
	e.mu.RUnlock()

	if !running {
		return fmt.Errorf("engine is not running")
	}

	if err := e.StopApp(name); err != nil {
		return err
	}

	// Re-check after StopApp — engine may have shut down in the meantime.
	if engineCtx.Err() != nil {
		return fmt.Errorf("engine stopped during restart")
	}

	ent := e.entries[name]
	ent.mu.Lock()
	ent.retries = 0
	ent.lastErr = nil
	ent.mu.Unlock()

	if err := ent.app.Init(engineCtx); err != nil {
		return fmt.Errorf("app %q: init: %w", name, err)
	}

	e.startApp(name)
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

// stopAll cancels and stops apps in reverse order.
func (e *Engine) stopAll(order []string) []error {
	var errs []error
	for i := len(order) - 1; i >= 0; i-- {
		ent := e.entries[order[i]]

		// Read cancel and decide whether to call Stop() under the same lock
		// to avoid TOCTOU between state check and Stop() call.
		ent.mu.Lock()
		cancel := ent.cancel
		skip := ent.state == StateStopped || ent.state == StateIdle
		ent.mu.Unlock()

		cancel()

		if skip {
			continue
		}
		if stopErrs := ent.app.Stop(); len(stopErrs) > 0 {
			errs = append(errs, stopErrs...)
		}
	}
	return errs
}
