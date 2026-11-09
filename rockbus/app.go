package rockbus

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
)

// ErrPanic is wrapped around a recovered panic value from a handler.
var ErrPanic = errors.New("rockbus: handler panic")

// ErrQueueFull is passed to OnError when PublishAsync cannot enqueue an event.
var ErrQueueFull = errors.New("rockbus: async queue is full, event dropped")

// ErrAppStopped is passed to OnError when PublishAsync is called after Stop.
var ErrAppStopped = errors.New("rockbus: app is stopped, event dropped")

// ErrNoWorker is passed to OnError when PublishAsync is called for a topic
// that has no handlers, and returned by Subscribe for a new topic after Init.
// Fix: pass the Subscription to NewApp or call Subscribe before starting the engine.
var ErrNoWorker = errors.New("rockbus: no worker for topic, Subscribe must be called before Init")

type asyncJob struct {
	ctx   context.Context
	event Event
}

// App is the rockbus event bus.
// It satisfies the rockengine App interface (Init / Exec / Stop).
//
// Each topic gets its own dedicated goroutine and buffered queue, which
// guarantees that events within the same topic are processed in order.
// Different topics are processed concurrently and do not block each other.
//
// Delivery is in-memory and at-most-once: events still queued when the
// process dies are lost. A slow handler delays later events of its topic.
//
// Every event passed to PublishAsync is either delivered to the topic's
// handlers or reported to OnError — never dropped silently.
//
// Usage:
//
//	app := rockbus.NewApp(cfg, onError,
//	    rockbus.On("user.created", handlers.OnUserCreated),
//	    rockbus.On("order.placed", handlers.OnOrderPlaced),
//	)
//	rockbus.SetDefault(app)
//	engine.MustRegister("bus", app, rockengine.RestartPolicy{})
type App struct {
	cfg     Config
	onError func(ctx context.Context, event Event, err error)

	mu       sync.RWMutex // guards handlers
	handlers map[Topic][]Handler

	// stateMu guards the per-Init state below. PublishAsync holds it for
	// reading while enqueueing; Stop takes it for writing before closing the
	// queues, so no send can race with a close.
	stateMu sync.RWMutex
	inited  bool                    // Init was called and queues exist
	stopped bool                    // Stop was called since the last Init
	running bool                    // Exec is active
	queues  map[Topic]chan asyncJob // one channel per topic, created in Init
	stopCh  chan struct{}           // closed by Stop
}

// NewApp creates an App with the given subscriptions.
// onError is called when an async handler errors, panics, the queue is full,
// the app is stopped, or a topic has no worker. Nil = silently dropped.
// Additional handlers can be registered later via Subscribe.
func NewApp(cfg Config, onError func(ctx context.Context, event Event, err error), subs ...Subscription) *App {
	app := &App{
		cfg:      cfg,
		onError:  onError,
		handlers: make(map[Topic][]Handler),
	}
	for _, s := range subs {
		app.handlers[s.topic] = append(app.handlers[s.topic], s.handler)
	}
	return app
}

// Init creates one queue per subscribed topic. Events published with
// PublishAsync after Init are buffered until Exec starts the workers, so
// other apps may publish as soon as the engine has initialised the bus.
// Safe to call again after Stop — resets internal state for restart.
func (a *App) Init(_ context.Context) error {
	queueSize := a.cfg.QueueSize
	if queueSize <= 0 {
		queueSize = 1024
	}

	// Lock order: stateMu, then mu. Holding stateMu while snapshotting the
	// topics means a concurrent Subscribe either lands before the snapshot
	// (and gets a queue) or sees inited and is rejected.
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	if a.running {
		return fmt.Errorf("rockbus: Init called while Exec is running")
	}

	a.mu.RLock()
	queues := make(map[Topic]chan asyncJob, len(a.handlers))
	for topic := range a.handlers {
		queues[topic] = make(chan asyncJob, queueSize)
	}
	a.mu.RUnlock()

	if a.inited && !a.stopped {
		// Re-Init without Stop: release the previous generation first.
		a.closeLocked()
	}
	a.queues = queues
	a.stopCh = make(chan struct{})
	a.inited = true
	a.stopped = false
	return nil
}

// Exec starts one worker goroutine per topic and blocks until ctx is
// cancelled or Stop is called. Each worker drains its queue before exiting.
//
// All subscriptions must be registered before Init — topics added afterwards
// have no queue and PublishAsync for them calls OnError with ErrNoWorker.
func (a *App) Exec(ctx context.Context) error {
	a.stateMu.Lock()
	if !a.inited {
		a.stateMu.Unlock()
		return fmt.Errorf("rockbus: Init must be called before Exec")
	}
	if a.running {
		a.stateMu.Unlock()
		return fmt.Errorf("rockbus: Exec is already running")
	}
	a.running = true
	queues, stopCh := a.queues, a.stopCh
	a.stateMu.Unlock()

	var wg sync.WaitGroup
	for _, ch := range queues {
		wg.Add(1)
		go func(ch chan asyncJob) {
			defer wg.Done()
			// range exits once Stop has closed the channel and it is drained.
			for job := range ch {
				a.processJob(job)
			}
		}(ch)
	}

	select {
	case <-ctx.Done():
		a.Stop()
	case <-stopCh:
	}

	wg.Wait()

	a.stateMu.Lock()
	a.running = false
	a.stateMu.Unlock()
	return nil
}

// Stop stops accepting events, then lets the workers drain their queues.
// Safe to call multiple times, before Init and before Exec. If Exec is not
// running, queued events are reported to OnError with ErrAppStopped.
func (a *App) Stop() []error {
	a.stateMu.Lock()
	if !a.inited || a.stopped {
		a.stateMu.Unlock()
		return nil
	}
	queues, running := a.queues, a.running
	a.closeLocked()
	a.stateMu.Unlock()

	if !running {
		// No workers will consume these queues; report what was buffered.
		// Runs outside the lock so OnError may publish without deadlocking.
		// If Exec starts concurrently, its workers and this loop share the
		// drain — every job is still handled exactly once.
		for _, ch := range queues {
			for job := range ch {
				a.callOnError(job.ctx, job.event, ErrAppStopped)
			}
		}
	}
	return nil
}

// closeLocked marks the current generation stopped and closes its queues.
// Caller must hold stateMu for writing.
func (a *App) closeLocked() {
	a.stopped = true
	close(a.stopCh)
	for _, ch := range a.queues {
		close(ch)
	}
}

// Subscribe registers a raw handler for topic.
// A new topic must be subscribed before Init so it gets a queue and a worker;
// afterwards Subscribe returns an error wrapping ErrNoWorker (until the next
// Init after Stop). Adding handlers to an already subscribed topic works at
// any time.
func (a *App) Subscribe(topic Topic, handler Handler) error {
	if handler == nil {
		return fmt.Errorf("rockbus: nil handler for topic %q", topic)
	}

	a.stateMu.RLock()
	defer a.stateMu.RUnlock()
	if a.inited && !a.stopped {
		if _, ok := a.queues[topic]; !ok {
			return fmt.Errorf("%w: %q is a new topic and the bus is already initialised", ErrNoWorker, topic)
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	a.handlers[topic] = append(a.handlers[topic], handler)
	return nil
}

// Publish delivers event synchronously in the caller's goroutine.
// All handlers are called even if one fails; panics are recovered.
// Returns a combined error if any handler fails.
// Does not require Init or Exec — works as soon as handlers are registered.
func (a *App) Publish(ctx context.Context, event Event) error {
	handlers := a.snapshot(event.Topic)

	var errs []error
	for _, h := range handlers {
		if err := safeCall(ctx, event, h); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

// PublishAsync routes event into the topic's dedicated queue.
// Returns immediately. If the queue is full, the app is stopped or not yet
// initialised, or the topic has no worker, OnError is called with the event.
// Context cancellation and deadline are detached — the worker runs to completion
// regardless of the caller's lifecycle.
func (a *App) PublishAsync(ctx context.Context, event Event) {
	err := a.enqueue(ctx, event)
	if err != nil {
		a.callOnError(ctx, event, err)
	}
}

// enqueue holds stateMu for reading for the whole send, so Stop cannot close
// the channel in between. The send is non-blocking, so the read lock is held
// only briefly.
func (a *App) enqueue(ctx context.Context, event Event) error {
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()

	if !a.inited {
		return fmt.Errorf("rockbus: Init must be called before PublishAsync")
	}
	if a.stopped {
		return ErrAppStopped
	}
	ch, ok := a.queues[event.Topic]
	if !ok {
		return fmt.Errorf("%w: %q", ErrNoWorker, event.Topic)
	}

	select {
	case ch <- asyncJob{ctx: context.WithoutCancel(ctx), event: event}:
		return nil
	default:
		return ErrQueueFull
	}
}

func (a *App) processJob(job asyncJob) {
	handlers := a.snapshot(job.event.Topic)
	var errs []error
	for _, h := range handlers {
		if err := safeCall(job.ctx, job.event, h); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		a.callOnError(job.ctx, job.event, errors.Join(errs...))
	}
}

// callOnError calls OnError safely — a panic inside OnError is recovered
// and written to stderr to avoid bringing down a worker goroutine.
func (a *App) callOnError(ctx context.Context, event Event, err error) {
	if a.onError == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			_, _ = fmt.Fprintf(errWriter, "rockbus: OnError panicked: %v\n%s\n", r, debug.Stack())
		}
	}()
	a.onError(ctx, event, err)
}

func (a *App) snapshot(topic Topic) []Handler {
	a.mu.RLock()
	defer a.mu.RUnlock()
	src := a.handlers[topic]
	if len(src) == 0 {
		return nil
	}
	out := make([]Handler, len(src))
	copy(out, src)
	return out
}

// safeCall injects the event into ctx, then invokes h recovering any panic.
func safeCall(ctx context.Context, event Event, h Handler) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: %v\n%s", ErrPanic, r, debug.Stack())
		}
	}()
	return h(injectEvent(ctx, event))
}
