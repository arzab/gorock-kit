package rockcron

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// ErrPanic is wrapped around a recovered panic value from a job handler.
var ErrPanic = errors.New("rockcron: job panic")

// App is the rockcron scheduler.
// It satisfies the rockengine App interface (Init / Exec / Stop).
//
// Each job runs in its own goroutine on its defined schedule.
// If a job is still running when its next tick arrives it is skipped
// (SkipIfStillRunning). When the App stops, the context passed to running
// jobs is cancelled and Exec returns only after they finish.
//
// Configuration errors (invalid location or schedule) are returned from
// Init, so they fail engine startup instead of triggering restarts.
//
// Usage:
//
//	app := rockcron.NewApp(cfg,
//	    func(ctx context.Context, job rockcron.Job, err error) {
//	        log.Error("cron error", rocklog.Str("job", rockcron.JobName(job)), rocklog.Err(err))
//	    },
//	    &SyncCacheJob{repo: repo},
//	    &DailyReportJob{db: db},
//	)
//	engine.MustRegister("cron", app, rockengine.RestartPolicy{})
type App struct {
	cfg     Config
	onError func(ctx context.Context, job Job, err error)
	jobs    []Job

	// mu guards the per-Init state below.
	mu      sync.Mutex
	cron    *cron.Cron
	stopCh  chan struct{} // closed by Stop
	stopped bool          // Stop was called since the last Init
	running bool          // Exec is active
	jobCtx  context.Context
}

// NewApp creates an App with the given jobs.
// onError is called when a job returns an error or panics. Nil = silently ignored.
func NewApp(cfg Config, onError func(ctx context.Context, job Job, err error), jobs ...Job) *App {
	return &App{
		cfg:     cfg,
		onError: onError,
		jobs:    jobs,
	}
}

// Init validates the location and every schedule and builds the scheduler.
// Safe to call again after Stop — resets internal state for restart.
func (a *App) Init(_ context.Context) error {
	loc := time.UTC
	if a.cfg.Location != "" {
		var err error
		loc, err = time.LoadLocation(a.cfg.Location)
		if err != nil {
			return fmt.Errorf("rockcron: invalid location %q: %w", a.cfg.Location, err)
		}
	}

	c := cron.New(
		cron.WithLocation(loc),
		cron.WithChain(cron.SkipIfStillRunning(cron.DiscardLogger)),
	)
	for _, job := range a.jobs {
		j := job
		if _, err := c.AddFunc(j.Schedule(), func() { a.runJob(j) }); err != nil {
			return fmt.Errorf("rockcron: invalid schedule for job %q: %w", JobName(j), err)
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.running {
		return fmt.Errorf("rockcron: Init called while Exec is running")
	}
	a.cron = c
	a.stopCh = make(chan struct{})
	a.stopped = false
	a.jobCtx = nil
	return nil
}

// Exec starts the scheduler and blocks until ctx is cancelled or Stop is called.
// Running jobs see their context cancelled and complete before Exec returns.
func (a *App) Exec(ctx context.Context) error {
	a.mu.Lock()
	if a.cron == nil {
		a.mu.Unlock()
		return fmt.Errorf("rockcron: Init must be called before Exec")
	}
	if a.running {
		a.mu.Unlock()
		return fmt.Errorf("rockcron: Exec is already running")
	}
	if a.stopped {
		a.mu.Unlock()
		return nil // Stop arrived between Init and Exec
	}
	jobCtx, cancelJobs := context.WithCancel(ctx)
	defer cancelJobs()
	a.running = true
	a.jobCtx = jobCtx
	c, stopCh := a.cron, a.stopCh
	a.mu.Unlock()

	c.Start()

	select {
	case <-ctx.Done():
	case <-stopCh:
	}

	cancelJobs()
	<-c.Stop().Done() // wait for running jobs

	a.mu.Lock()
	a.running = false
	a.mu.Unlock()
	return nil
}

// Stop signals the scheduler to stop. It does not wait: Exec returns once
// running jobs have finished. Safe to call multiple times and before Init.
func (a *App) Stop() []error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopCh == nil || a.stopped {
		return nil
	}
	a.stopped = true
	close(a.stopCh)
	return nil
}

func (a *App) runJob(j Job) {
	a.mu.Lock()
	ctx := a.jobCtx
	a.mu.Unlock()
	if ctx == nil || ctx.Err() != nil {
		return // fired while stopping
	}
	if err := safeCall(ctx, j); err != nil {
		a.callOnError(ctx, j, err)
	}
}

func (a *App) callOnError(ctx context.Context, job Job, err error) {
	if a.onError == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			_, _ = fmt.Fprintf(errWriter, "rockcron: OnError panicked: %v\n%s\n", r, debug.Stack())
		}
	}()
	a.onError(ctx, job, err)
}

func safeCall(ctx context.Context, j Job) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: %v\n%s", ErrPanic, r, debug.Stack())
		}
	}()
	return j.Run(ctx)
}
