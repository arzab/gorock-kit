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
// that had no handlers registered before Exec was called.
// Fix: pass the Subscription to NewApp or call Subscribe before starting the engine.
var ErrNoWorker = errors.New("rockbus: no worker for topic, Subscribe must be called before Exec")

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
// Usage:
//
//	app := rockbus.NewApp(cfg,
//	    rockbus.On("user.created", handlers.OnUserCreated),
//	    rockbus.On("order.placed", handlers.OnOrderPlaced),
//	)
//	rockbus.SetDefault(app)
//	engine.MustRegister("bus", app, rockengine.RestartPolicy{})
type App struct {
	cfg      Config
	mu       sync.RWMutex
	handlers map[Topic][]Handler
	queues   map[Topic]chan asyncJob // one channel per topic, created at Exec time
	doneMu   sync.Mutex             // guards done, stopOnce, and running
	done     chan struct{}
	stopOnce sync.Once
	running  bool
}

// NewApp creates an App with the given subscriptions.
// Additional handlers can be registered later via Subscribe.
func NewApp(cfg Config, subs ...Subscription) *App {
	app := &App{
		cfg:      cfg,
		handlers: make(map[Topic][]Handler),
	}
	for _, s := range subs {
		app.handlers[s.topic] = append(app.handlers[s.topic], s.handler)
	}
	return app
}

// Init prepares the app for execution.
// Safe to call again after Stop — resets internal state for restart.
func (a *App) Init(_ context.Context) error {
	a.doneMu.Lock()
	defer a.doneMu.Unlock()

	a.done = make(chan struct{})
	a.stopOnce = sync.Once{} // safe under doneMu
	a.queues = nil
	return nil
}

// Exec creates one worker goroutine per registered topic and blocks until
// ctx is cancelled or Stop is called. Each worker drains its queue before exiting.
//
// All subscriptions must be registered before Exec — topics added afterwards
// will not have a worker and PublishAsync for them will call OnError.
func (a *App) Exec(ctx context.Context) error {
	a.doneMu.Lock()
	if a.done == nil {
		a.doneMu.Unlock()
		return fmt.Errorf("rockbus: Init must be called before Exec")
	}
	if a.running {
		a.doneMu.Unlock()
		return fmt.Errorf("rockbus: Exec is already running")
	}
	a.running = true
	done := a.done
	a.doneMu.Unlock()

	defer func() {
		a.doneMu.Lock()
		a.running = false
		a.doneMu.Unlock()
	}()

	queueSize := a.cfg.QueueSize
	if queueSize <= 0 {
		queueSize = 1024
	}

	// Snapshot topics and create per-topic queues.
	a.mu.Lock()
	queues := make(map[Topic]chan asyncJob, len(a.handlers))
	for topic := range a.handlers {
		queues[topic] = make(chan asyncJob, queueSize)
	}
	a.queues = queues
	a.mu.Unlock()

	// Start one worker goroutine per topic.
	var wg sync.WaitGroup
	for _, ch := range queues {
		wg.Add(1)
		go func(ch chan asyncJob) {
			defer wg.Done()
			for {
				select {
				case job := <-ch:
					a.processJob(job)
				case <-done:
					// drain remaining jobs in this topic's queue before exiting
					for {
						select {
						case job := <-ch:
							a.processJob(job)
						default:
							return
						}
					}
				}
			}
		}(ch)
	}

	select {
	case <-ctx.Done():
		a.Stop()
	case <-done:
	}

	wg.Wait()

	// Clear queues so PublishAsync after shutdown gets ErrAppStopped, not ErrNoWorker.
	a.mu.Lock()
	a.queues = nil
	a.mu.Unlock()

	return nil
}

// Stop signals all workers to drain and exit.
// Safe to call multiple times and before Init.
func (a *App) Stop() []error {
	a.doneMu.Lock()
	done := a.done
	a.doneMu.Unlock()

	if done == nil {
		return nil
	}
	a.stopOnce.Do(func() { close(done) })
	return nil
}

// Subscribe registers a raw handler for topic.
// Must be called before Exec so the topic gets a dedicated worker.
// Prefer On + NewApp for the primary registration flow.
func (a *App) Subscribe(topic Topic, handler Handler) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.handlers[topic] = append(a.handlers[topic], handler)
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
// Returns immediately. If the queue is full, the app is stopped, or the topic
// has no worker, OnError is called with the dropped event.
// Context cancellation and deadline are detached — the worker runs to completion
// regardless of the caller's lifecycle.
func (a *App) PublishAsync(ctx context.Context, event Event) {
	a.doneMu.Lock()
	done := a.done
	a.doneMu.Unlock()

	if done == nil {
		a.callOnError(ctx, event, fmt.Errorf("rockbus: Init must be called before PublishAsync"))
		return
	}

	// Fast path: already stopped.
	select {
	case <-done:
		a.callOnError(ctx, event, ErrAppStopped)
		return
	default:
	}

	a.mu.RLock()
	ch, ok := a.queues[event.Topic]
	a.mu.RUnlock()

	if !ok {
		a.callOnError(ctx, event, fmt.Errorf("%w: %q", ErrNoWorker, event.Topic))
		return
	}

	detached := context.WithoutCancel(ctx)
	job := asyncJob{ctx: detached, event: event}

	select {
	case ch <- job:
	case <-done:
		a.callOnError(ctx, event, ErrAppStopped)
	default:
		a.callOnError(ctx, event, ErrQueueFull)
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
	if a.cfg.OnError == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			_, _ = fmt.Fprintf(errWriter, "rockbus: OnError panicked: %v\n%s\n", r, debug.Stack())
		}
	}()
	a.cfg.OnError(ctx, event, err)
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
