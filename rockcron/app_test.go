package rockcron

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// robfig/cron's smallest @every interval is one second, so jobs here tick at 1s.
const testTimeout = 4 * time.Second

func startCron(t *testing.T, a *App, ctx context.Context) <-chan error {
	t.Helper()
	if err := a.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	res := make(chan error, 1)
	go func() { res <- a.Exec(ctx) }()
	return res
}

func waitDone(t *testing.T, res <-chan error) {
	t.Helper()
	select {
	case err := <-res:
		if err != nil {
			t.Fatalf("Exec: %v", err)
		}
	case <-time.After(testTimeout):
		t.Fatal("Exec did not return")
	}
}

func noop(context.Context) error { return nil }

// Configuration errors must surface from Init (fail startup), not from Exec
// (where the engine would treat them as runtime failures and restart).
func TestInvalidScheduleFailsInit(t *testing.T) {
	a := NewApp(Config{}, nil, Cron("bad", "not a cron", noop))
	err := a.Init(context.Background())
	if err == nil || !strings.Contains(err.Error(), `job "bad"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestInvalidLocationFailsInit(t *testing.T) {
	a := NewApp(Config{Location: "Mars/Olympus"}, nil, Every("j", time.Second, noop))
	if err := a.Init(context.Background()); err == nil {
		t.Fatal("expected error for invalid location")
	}
}

func TestJobRunsAndReportsErrors(t *testing.T) {
	boom := errors.New("boom")
	got := make(chan error, 4)
	a := NewApp(Config{}, func(_ context.Context, j Job, err error) {
		if JobName(j) == "j" {
			got <- err
		}
	}, Every("j", time.Second, func(context.Context) error { return boom }))

	res := startCron(t, a, context.Background())
	select {
	case err := <-got:
		if !errors.Is(err, boom) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(testTimeout):
		t.Fatal("job did not run")
	}
	a.Stop()
	waitDone(t, res)
}

func TestPanicReported(t *testing.T) {
	got := make(chan error, 4)
	a := NewApp(Config{}, func(_ context.Context, _ Job, err error) { got <- err },
		Every("p", time.Second, func(context.Context) error { panic("kaboom") }))
	res := startCron(t, a, context.Background())
	select {
	case err := <-got:
		if !errors.Is(err, ErrPanic) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(testTimeout):
		t.Fatal("panic not reported")
	}
	a.Stop()
	waitDone(t, res)
}

// Stop must cancel running jobs' ctx (previously only the Exec ctx did) and
// Exec must wait for them.
func TestStopCancelsAndWaitsForRunningJob(t *testing.T) {
	started := make(chan struct{}, 1)
	var finished, sawCancel atomic.Bool
	a := NewApp(Config{}, nil, Every("long", time.Second, func(ctx context.Context) error {
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		time.Sleep(50 * time.Millisecond) // simulate cleanup
		sawCancel.Store(true)
		finished.Store(true)
		return nil
	}))
	res := startCron(t, a, context.Background())
	select {
	case <-started:
	case <-time.After(testTimeout):
		t.Fatal("job did not start")
	}
	a.Stop()
	waitDone(t, res)
	if !sawCancel.Load() || !finished.Load() {
		t.Fatalf("sawCancel=%v finished=%v: Exec returned before the job finished", sawCancel.Load(), finished.Load())
	}
}

func TestRestartAndIdempotentStop(t *testing.T) {
	var runs atomic.Int32
	a := NewApp(Config{}, nil, Every("j", time.Second, func(context.Context) error { runs.Add(1); return nil }))
	a.Stop() // before Init

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	res := startCron(t, a, ctx)
	a.Stop()
	a.Stop()
	waitDone(t, res)

	res = startCron(t, a, ctx)
	deadline := time.Now().Add(testTimeout)
	for runs.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("job did not run after restart")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	a.Stop()
	waitDone(t, res)
}

func TestStopBeforeExec(t *testing.T) {
	a := NewApp(Config{}, nil, Every("j", time.Second, noop))
	if err := a.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.Stop()
	done := make(chan error, 1)
	go func() { done <- a.Exec(context.Background()) }()
	waitDone(t, done)
}
