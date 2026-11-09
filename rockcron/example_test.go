package rockcron_test

import (
	"context"
	"log"
	"time"

	"github.com/arzab/gorock-kit/rockcron"
)

// Compiled, not run: mirrors the README Quick Start.

type SyncCacheJob struct{}

func (SyncCacheJob) Schedule() string              { return "@every 5m" }
func (SyncCacheJob) JobName() string               { return "sync-cache" }
func (SyncCacheJob) Run(ctx context.Context) error { return ctx.Err() }

func ExampleNewApp() {
	prepare := func(context.Context) error { return nil }
	send := func(context.Context) error { return nil }

	app := rockcron.NewApp(rockcron.Config{Location: "Europe/Moscow"},
		func(ctx context.Context, job rockcron.Job, err error) {
			log.Printf("cron %s: %v", rockcron.JobName(job), err)
		},
		rockcron.Every("heartbeat", 30*time.Second, prepare),
		rockcron.Cron("daily-report", "0 3 * * *", prepare, send),
		SyncCacheJob{},
	)

	ctx := context.Background()
	if err := app.Init(ctx); err != nil { // validates location and schedules
		log.Fatal(err)
	}
	if err := app.Exec(ctx); err != nil {
		log.Fatal(err)
	}
}
