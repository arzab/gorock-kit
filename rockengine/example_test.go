package rockengine_test

import (
	"context"
	"log"
	"time"

	"github.com/arzab/gorock-kit/rockengine"
)

// Examples are compiled by `go test` (they have no Output, so they are not
// run). They mirror the README so that a drifting API breaks the build.

type WorkerApp struct{ ticker *time.Ticker }

func (a *WorkerApp) Init(context.Context) error {
	a.ticker = time.NewTicker(time.Second)
	return nil
}

func (a *WorkerApp) Exec(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-a.ticker.C:
		}
	}
}

func (a *WorkerApp) Stop() []error {
	a.ticker.Stop()
	return nil
}

func Example() {
	engine := rockengine.New(map[string]rockengine.App{
		"worker": &WorkerApp{},
	}).WithShutdownTimeout(15 * time.Second)

	// Blocks until SIGINT/SIGTERM, ctx cancellation or an App failure.
	if err := engine.Run(context.Background(), "worker"); err != nil {
		log.Fatal(err)
	}
}

func ExampleEngine_WithRestarts() {
	// Without an orchestrator: restart a failed App up to 3 times, a second apart.
	engine := rockengine.New(map[string]rockengine.App{"worker": &WorkerApp{}}).
		WithRestarts(3, time.Second)

	if err := engine.Run(context.Background(), "worker"); err != nil {
		log.Fatal(err)
	}
}
