package rockcron

import (
	"context"
	"time"
)

// Job is the interface all scheduled jobs must implement.
// Use Every or Cron for simple cases, or implement directly for complex ones.
//
// Example (custom struct):
//
//	type SyncCacheJob struct{ repo Repository }
//
//	func (j *SyncCacheJob) Schedule() string                { return "@every 5m" }
//	func (j *SyncCacheJob) JobName() string                 { return "sync-cache" }
//	func (j *SyncCacheJob) Run(ctx context.Context) error   { return j.repo.SyncCache(ctx) }
type Job interface {
	// Schedule returns a 5-field cron expression or an @every <duration> string.
	//
	// Examples:
	//   "@every 5m"    — every 5 minutes
	//   "0 3 * * *"    — every day at 03:00
	//   "*/15 * * * *" — every 15 minutes
	Schedule() string

	// Run executes the job. Returning an error triggers OnError. Panics are recovered.
	Run(ctx context.Context) error
}

// Namer is an optional interface. If a Job implements Namer its name is used
// in OnError calls instead of the schedule string.
type Namer interface {
	JobName() string
}

// JobName returns the display name for the job.
// Falls back to the schedule string if the job does not implement Namer
// or JobName returns an empty string.
func JobName(j Job) string {
	if n, ok := j.(Namer); ok {
		if name := n.JobName(); name != "" {
			return name
		}
	}
	return j.Schedule()
}

// runChain executes handlers sequentially. Stops on the first error.
func runChain(ctx context.Context, handlers []Handler) error {
	for _, h := range handlers {
		if err := h(ctx); err != nil {
			return err
		}
	}
	return nil
}

// IntervalJob runs its handler chain at a fixed time interval.
// Create with Every.
type IntervalJob struct {
	name     string
	interval time.Duration
	handlers []Handler
}

// Every creates an IntervalJob that runs at the given interval.
// Panics if interval is zero or negative, or if no handlers are provided.
//
// Example:
//
//	rockcron.Every("sync-cache", 5*time.Minute, validateData, syncCache)
func Every(name string, interval time.Duration, handlers ...Handler) *IntervalJob {
	if interval <= 0 {
		panic("rockcron: Every interval must be positive")
	}
	if len(handlers) == 0 {
		panic("rockcron: Every requires at least one handler")
	}
	return &IntervalJob{name: name, interval: interval, handlers: handlers}
}

func (j *IntervalJob) Schedule() string                { return "@every " + j.interval.String() }
func (j *IntervalJob) JobName() string                 { return j.name }
func (j *IntervalJob) Run(ctx context.Context) error   { return runChain(ctx, j.handlers) }

// CronJob runs its handler chain on a 5-field cron expression.
// Create with Cron.
type CronJob struct {
	name     string
	expr     string
	handlers []Handler
}

// Cron creates a CronJob that runs on the given 5-field cron expression.
// Panics if no handlers are provided.
// An invalid expression is caught later at Exec time and returned as an error.
//
// Examples:
//
//	rockcron.Cron("healthcheck",   "* * * * *",  handler)
//	rockcron.Cron("daily-report",  "0 3 * * *",  prepare, report)
func Cron(name, expr string, handlers ...Handler) *CronJob {
	if len(handlers) == 0 {
		panic("rockcron: Cron requires at least one handler")
	}
	return &CronJob{name: name, expr: expr, handlers: handlers}
}

func (j *CronJob) Schedule() string                { return j.expr }
func (j *CronJob) JobName() string                 { return j.name }
func (j *CronJob) Run(ctx context.Context) error   { return runChain(ctx, j.handlers) }
