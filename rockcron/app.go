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
// (SkipIfStillRunning). When the App stops, all running jobs finish
// before Exec returns.
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
	cfg      Config
	onError  func(ctx context.Context, job Job, err error)
	jobs     []Job
	doneMu   sync.Mutex
	done     chan struct{}
	stopOnce sync.Once
	running  bool
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

// Init prepares the App for execution.
// Safe to call again after Stop — resets internal state for restart.
func (a *App) Init(_ context.Context) error {
	a.doneMu.Lock()
	defer a.doneMu.Unlock()

	a.done = make(chan struct{})
	a.stopOnce = sync.Once{}
	return nil
}

// Exec starts the scheduler and blocks until ctx is cancelled or Stop is called.
// All running jobs complete before Exec returns.
func (a *App) Exec(ctx context.Context) error {
	a.doneMu.Lock()
	if a.done == nil {
		a.doneMu.Unlock()
		return fmt.Errorf("rockcron: Init must be called before Exec")
	}
	if a.running {
		a.doneMu.Unlock()
		return fmt.Errorf("rockcron: Exec is already running")
	}
	a.running = true
	done := a.done
	a.doneMu.Unlock()

	defer func() {
		a.doneMu.Lock()
		a.running = false
		a.doneMu.Unlock()
	}()

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
		if _, err := c.AddFunc(j.Schedule(), func() {
			if err := safeCall(ctx, j); err != nil {
				a.callOnError(ctx, j, err)
			}
		}); err != nil {
			return fmt.Errorf("rockcron: invalid schedule for job %q: %w", JobName(j), err)
		}
	}

	c.Start()

	select {
	case <-ctx.Done():
		a.Stop()
	case <-done:
	}

	// Wait for all running jobs to complete.
	<-c.Stop().Done()

	return nil
}

// Stop signals the scheduler to stop and waits for running jobs to finish.
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
