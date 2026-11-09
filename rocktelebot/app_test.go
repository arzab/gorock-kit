package rocktelebot

import (
	"context"
	"sync"
	"testing"
	"time"

	tele "gopkg.in/telebot.v3"
)

// fakePoller blocks until telebot asks it to stop and reports when polling starts.
type fakePoller struct {
	started chan struct{}
}

func (p *fakePoller) Poll(_ *tele.Bot, _ chan tele.Update, stop chan struct{}) {
	p.started <- struct{}{}
	<-stop
}

// useOfflineBot replaces newBot for the duration of the test so no network is used.
func useOfflineBot(t *testing.T) *fakePoller {
	t.Helper()
	p := &fakePoller{started: make(chan struct{}, 16)}
	orig := newBot
	newBot = func(s tele.Settings) (*tele.Bot, error) {
		s.Offline = true
		s.Poller = p
		return tele.NewBot(s)
	}
	t.Cleanup(func() { newBot = orig })
	return p
}

const testTimeout = 2 * time.Second

func waitStarted(t *testing.T, p *fakePoller) {
	t.Helper()
	select {
	case <-p.started:
	case <-time.After(testTimeout):
		t.Fatal("bot did not start polling")
	}
}

// within fails the test if fn does not return in time — the bug this guards
// against is a hang, so a deadlock must surface as a failure, not a stuck test.
func within(t *testing.T, name string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(testTimeout):
		t.Fatalf("%s did not return within %s (deadlock)", name, testTimeout)
	}
}

func startApp(t *testing.T, a *App, ctx context.Context) <-chan error {
	t.Helper()
	if err := a.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	execErr := make(chan error, 1)
	go func() { execErr <- a.Exec(ctx) }()
	return execErr
}

func waitExec(t *testing.T, execErr <-chan error) {
	t.Helper()
	select {
	case err := <-execErr:
		if err != nil {
			t.Fatalf("Exec returned error: %v", err)
		}
	case <-time.After(testTimeout):
		t.Fatal("Exec did not return")
	}
}

// Regression: engine shutdown cancels ctx and calls Stop at the same time.
// Previously both paths called bot.Stop and the second one blocked forever.
func TestCancelAndStopConcurrently(t *testing.T) {
	p := useOfflineBot(t)
	a := NewApp(Config{Token: "test"}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	execErr := startApp(t, a, ctx)
	waitStarted(t, p)

	within(t, "cancel+Stop", func() {
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); cancel() }()
		go func() { defer wg.Done(); a.Stop() }()
		wg.Wait()
	})
	waitExec(t, execErr)
}

func TestStopTwice(t *testing.T) {
	p := useOfflineBot(t)
	a := NewApp(Config{Token: "test"}, nil)
	execErr := startApp(t, a, context.Background())
	waitStarted(t, p)

	within(t, "first Stop", func() { a.Stop() })
	waitExec(t, execErr)
	within(t, "second Stop", func() { a.Stop() })
}

func TestStopBeforeInit(t *testing.T) {
	a := NewApp(Config{Token: "test"}, nil)
	within(t, "Stop", func() { a.Stop() })
}

func TestStopBeforeExec(t *testing.T) {
	useOfflineBot(t)
	a := NewApp(Config{Token: "test"}, nil)
	if err := a.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	within(t, "Stop", func() { a.Stop() })
	within(t, "Exec after Stop", func() {
		if err := a.Exec(context.Background()); err != nil {
			t.Errorf("Exec: %v", err)
		}
	})
}

func TestExecWithoutInit(t *testing.T) {
	a := NewApp(Config{Token: "test"}, nil)
	if err := a.Exec(context.Background()); err == nil {
		t.Fatal("expected error when Exec is called before Init")
	}
}

// Mirrors the engine's restart sequence: Stop → Init → Exec, then shutdown.
// Previously the goroutine from the first Exec outlived the restart and later
// stopped the new bot a second time.
func TestRestart(t *testing.T) {
	p := useOfflineBot(t)
	a := NewApp(Config{Token: "test"}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	execErr := startApp(t, a, ctx)
	waitStarted(t, p)
	within(t, "Stop", func() { a.Stop() })
	waitExec(t, execErr)

	execErr = startApp(t, a, ctx)
	waitStarted(t, p)
	within(t, "cancel+Stop after restart", func() {
		cancel()
		a.Stop()
	})
	waitExec(t, execErr)
}
