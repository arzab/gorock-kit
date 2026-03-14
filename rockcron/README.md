# rockcron

A scheduled job runner for Go applications. Integrates with [rockengine](../rockengine) and provides a clean interface for periodic task execution.

Each job defines its own schedule — no need to wire schedule and logic in separate places. Jobs run in isolated goroutines: if a job is still running when its next tick arrives, the tick is skipped. On shutdown, jobs that are mid-execution are waited on — jobs waiting for their next tick are simply never triggered again.

Satisfies the rockengine `App` interface (`Init / Exec / Stop`).

## Quick Start

```go
app := rockcron.NewApp(cfg,
    func(ctx context.Context, job rockcron.Job, err error) {
        log.Error("cron error", rocklog.Str("job", rockcron.JobName(job)), rocklog.Err(err))
    },
    rockcron.Every("sync-cache",  5*time.Minute, syncCache),
    rockcron.Cron("daily-report", "0 3 * * *",  prepareReport, sendReport),
)

engine.MustRegister("cron", app, rockengine.RestartPolicy{})
engine.Run()
```

## Config

```go
type Config struct {
    // IANA timezone name for cron schedule evaluation.
    // Example: "Europe/Moscow". Default: UTC.
    Location string `config:",omitempty"`
}
```

## Lifecycle

```
NewApp(cfg, onError, jobs...) → Init → Exec (blocks) → Stop
```

- **Init** resets internal state. Safe to call again after `Stop` for restart scenarios.
- **Exec** registers all jobs, starts the scheduler, and blocks until `ctx` is cancelled or `Stop` is called.
- **Stop** is idempotent and safe to call before `Init`.

## Defining Jobs

### Inline helpers (simple cases)

```go
// Every — runs at a fixed interval
rockcron.Every("sync-cache", 5*time.Minute, syncCache)

// Cron — runs on a 5-field cron expression
rockcron.Cron("daily-report", "0 3 * * *", prepareReport, sendReport)
```

Cron expression reference:

```
┌───── minute (0-59)
│ ┌─── hour (0-23)
│ │ ┌─ day of month (1-31)
│ │ │ ┌ month (1-12)
│ │ │ │ ┌ day of week (0-6, Sunday=0)
* * * * *
```

### Custom struct (complex cases with dependencies)

When a job needs injected dependencies (db, repo, mailer), implement the `Job` interface directly. The schedule lives next to the business logic.

```go
type SyncCacheJob struct {
    repo Repository
}

func (j *SyncCacheJob) Schedule() string                { return "@every 5m" }
func (j *SyncCacheJob) JobName()  string                { return "sync-cache" }
func (j *SyncCacheJob) Run(ctx context.Context) error   { return j.repo.SyncCache(ctx) }
```

```go
app := rockcron.NewApp(cfg, onError, &SyncCacheJob{repo: repo})
```

`JobName()` is optional — implement `rockcron.Namer` to provide a name for logging.

## Handler Chain

Both `Every` and `Cron` accept multiple handlers that execute sequentially. If one handler returns an error, the remaining handlers are skipped and `onError` is called.

```go
rockcron.Every("pipeline", 10*time.Minute,
    fetchData,    // step 1: if this fails → step 2 and 3 are skipped
    processData,  // step 2
    saveResults,  // step 3
)
```

## Error Handling

`onError` is called when a job returns an error or panics:

```go
func(ctx context.Context, job rockcron.Job, err error) {
    switch {
    case errors.Is(err, rockcron.ErrPanic):
        log.Error("job panicked", rocklog.Str("job", rockcron.JobName(job)), rocklog.Err(err))
    default:
        log.Warn("job failed", rocklog.Str("job", rockcron.JobName(job)), rocklog.Err(err))
    }
}
```

A panic inside `onError` itself is recovered and written to `stderr`.

### Sentinel errors

```go
errors.Is(err, rockcron.ErrPanic) // job handler panicked, panic recovered
```

## Behaviour

- **SkipIfStillRunning** — if a job's previous run has not finished when the next tick arrives, the tick is silently skipped. No duplicate concurrent runs.
- **Graceful shutdown** — when stopped, the scheduler waits for all currently running jobs to finish before `Exec` returns.
- **Context** — the `ctx` passed to handlers is the same context as `Exec`. When the App is stopping, `ctx` is cancelled — long-running jobs should respect it.
- **Panic recovery** — panics in any handler are caught and converted to an `ErrPanic`-wrapped error, which is passed to `onError`. The goroutine is never brought down.

## Limitations

- **No dynamic registration** — all jobs must be passed to `NewApp`. Adding jobs after `Exec` starts is not supported.
- **No per-job concurrency** — each job has at most one running instance at a time (SkipIfStillRunning).
- **Minute precision for Cron** — the standard 5-field expression has 1-minute granularity. Use `Every` for sub-minute intervals.
