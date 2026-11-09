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
	rockengine.Register("worker", &WorkerApp{}, rockengine.RestartPolicy{
		MaxRetries: -1,
		Delay:      2 * time.Second,
		ResetAfter: time.Minute,
	})
	if err := rockengine.Run(); err != nil {
		log.Fatal(err)
	}
}

func ExampleNewEngine() {
	engine := rockengine.NewEngine().
		WithShutdownTimeout(15 * time.Second).
		WithStopOnFatal(false).
		WithFatalHandler(func(name string, err error) { log.Printf("app %s is dead: %v", name, err) })

	engine.MustRegister("worker", &WorkerApp{}, rockengine.RestartPolicy{MaxRetries: 3, Delay: time.Second})

	if err := engine.Run(); err != nil {
		log.Fatal(err)
	}
}
