package rockengine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testTimeout = 3 * time.Second

// events is a shared, ordered log of lifecycle calls across apps.
type events struct {
	mu  sync.Mutex
	log []string
}

func (ev *events) add(s string) {
	ev.mu.Lock()
	ev.log = append(ev.log, s)
	ev.mu.Unlock()
}

func (ev *events) snapshot() []string {
	ev.mu.Lock()
	defer ev.mu.Unlock()
	return append([]string(nil), ev.log...)
}

func (ev *events) count(s string) int {
	n := 0
	for _, e := range ev.snapshot() {
		if e == s {
			n++
		}
	}
	return n
}

// fakeApp records calls and delegates behaviour to optional hooks.
// Default Exec blocks until ctx is cancelled and returns ctx.Err().
type fakeApp struct {
	name string
	ev   *events

	initFn func(ctx context.Context, call int) error
	execFn func(ctx context.Context, call int) error
	stopFn func() []error

	inits, execs atomic.Int32
	running      chan struct{} // receives a value each time Exec starts
}

func newFake(name string, ev *events) *fakeApp {
	return &fakeApp{name: name, ev: ev, running: make(chan struct{}, 64)}
}

func (f *fakeApp) Init(ctx context.Context) error {
	n := int(f.inits.Add(1))
	f.ev.add(f.name + ":init")
	if f.initFn != nil {
		return f.initFn(ctx, n)
	}
	return nil
}

func (f *fakeApp) Exec(ctx context.Context) error {
	n := int(f.execs.Add(1))
	f.ev.add(f.name + ":exec")
	select { // non-blocking: crash loops may start Exec far more often than tests read
	case f.running <- struct{}{}:
	default:
	}
	if f.execFn != nil {
		return f.execFn(ctx, n)
	}
	<-ctx.Done()
	f.ev.add(f.name + ":exec-done")
	return ctx.Err()
}

func (f *fakeApp) Stop() []error {
	f.ev.add(f.name + ":stop")
	if f.stopFn != nil {
		return f.stopFn()
	}
	return nil
}

func (f *fakeApp) waitRunning(t *testing.T) {
	t.Helper()
	select {
	case <-f.running:
	case <-time.After(testTimeout):
		t.Fatalf("%s: Exec did not start", f.name)
	}
}

// run starts the engine in the background and returns a cancel func and a
// channel with RunContext's result.
func run(t *testing.T, e *Engine) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	res := make(chan error, 1)
	go func() { res <- e.RunContext(ctx) }()
	t.Cleanup(cancel)
	return cancel, res
}

func result(t *testing.T, res <-chan error) error {
	t.Helper()
	select {
	case err := <-res:
		return err
	case <-time.After(testTimeout):
		t.Fatal("RunContext did not return")
		return nil
	}
}

func waitState(t *testing.T, e *Engine, name string, want State) AppInfo {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for {
		info, err := e.AppStatus(name)
		if err != nil {
			t.Fatal(err)
		}
		if info.State == want {
			return info
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: state %q, want %q (err: %v)", name, info.State, want, info.Err)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestCleanShutdown(t *testing.T) {
	ev := &events{}
	a := newFake("a", ev)
	e := NewEngine().MustRegister("a", a)
	cancel, res := run(t, e)
	a.waitRunning(t)
	cancel()
	if err := result(t, res); err != nil {
		t.Fatalf("RunContext: %v", err)
	}
	if got := ev.count("a:stop"); got != 1 {
		t.Fatalf("Stop called %d times, want 1", got)
	}
	waitState(t, e, "a", StateStopped)
}

// 2.1: whatever Exec returns after its ctx is cancelled is a clean stop.
func TestErrorAfterCancelIsCleanStop(t *testing.T) {
	ev := &events{}
	a := newFake("a", ev)
	a.execFn = func(ctx context.Context, _ int) error {
		<-ctx.Done()
		return http.ErrServerClosed
	}
	var fatal atomic.Int32
	e := NewEngine().WithFatalHandler(func(string, error) { fatal.Add(1) }).MustRegister("a", a)
	cancel, res := run(t, e)
	a.waitRunning(t)
	cancel()
	if err := result(t, res); err != nil {
		t.Fatalf("RunContext: %v", err)
	}
	info := waitState(t, e, "a", StateStopped)
	if info.Retries != 0 || fatal.Load() != 0 {
		t.Fatalf("clean shutdown counted as failure: retries=%d fatal=%d", info.Retries, fatal.Load())
	}
}

// context.Canceled from Exec without the engine cancelling is an app error.
func TestCanceledWithoutCancelIsFailure(t *testing.T) {
	ev := &events{}
	a := newFake("a", ev)
	a.execFn = func(context.Context, int) error { return context.Canceled }
	e := NewEngine().WithStopOnFatal(false).MustRegister("a", a)
	_, _ = run(t, e)
	info := waitState(t, e, "a", StateFailed)
	if !errors.Is(info.Err, context.Canceled) {
		t.Fatalf("err = %v", info.Err)
	}
}

func TestRestartPolicy(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name       string
		maxRetries int
		failures   int // Exec fails this many times, then blocks
		wantState  State
		wantExecs  int
	}{
		{"no restart by default", 0, 1, StateFailed, 1},
		{"restarts up to limit then succeeds", 2, 2, StateRunning, 3},
		{"exceeds limit", 2, 3, StateFailed, 3},
		{"unlimited", -1, 5, StateRunning, 6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := &events{}
			a := newFake("a", ev)
			a.execFn = func(ctx context.Context, call int) error {
				if call <= tt.failures {
					return boom
				}
				<-ctx.Done()
				return ctx.Err()
			}
			e := NewEngine().WithStopOnFatal(false).MustRegister("a", a, RestartPolicy{MaxRetries: tt.maxRetries})
			cancel, res := run(t, e)
			for i := 0; i < tt.wantExecs; i++ {
				a.waitRunning(t)
			}
			waitState(t, e, "a", tt.wantState)
			if got := int(a.execs.Load()); got != tt.wantExecs {
				t.Fatalf("Exec called %d times, want %d", got, tt.wantExecs)
			}
			// Every Init is matched by exactly one Stop by the end.
			cancel()
			_ = result(t, res)
			if inits, stops := ev.count("a:init"), ev.count("a:stop"); inits != stops {
				t.Fatalf("Init called %d times but Stop %d times", inits, stops)
			}
		})
	}
}

// 2.2: a panic in Exec goes through the restart policy.
func TestPanicInExecIsRestarted(t *testing.T) {
	ev := &events{}
	a := newFake("a", ev)
	a.execFn = func(ctx context.Context, call int) error {
		if call == 1 {
			panic("kaboom")
		}
		<-ctx.Done()
		return ctx.Err()
	}
	e := NewEngine().MustRegister("a", a, RestartPolicy{MaxRetries: 1})
	cancel, res := run(t, e)
	a.waitRunning(t)
	a.waitRunning(t)
	info := waitState(t, e, "a", StateRunning)
	if info.Retries != 1 || !strings.Contains(info.Err.Error(), "kaboom") {
		t.Fatalf("retries=%d err=%v", info.Retries, info.Err)
	}
	cancel()
	if err := result(t, res); err != nil {
		t.Fatalf("RunContext: %v", err)
	}
}

func TestPanicInInit(t *testing.T) {
	ev := &events{}
	a := newFake("a", ev)
	b := newFake("b", ev)
	b.initFn = func(context.Context, int) error { panic("bad init") }
	e := NewEngine().MustRegister("a", a).MustRegister("b", b)
	_, res := run(t, e)
	err := result(t, res)
	if err == nil || !strings.Contains(err.Error(), `app "b": init`) {
		t.Fatalf("err = %v", err)
	}
	if ev.count("a:stop") != 1 || ev.count("b:stop") != 0 {
		t.Fatalf("expected only a to be stopped, log: %v", ev.snapshot())
	}
}

// 2.6: a panic in Stop does not crash shutdown and is reported.
func TestPanicInStop(t *testing.T) {
	ev := &events{}
	a := newFake("a", ev)
	a.stopFn = func() []error { panic("bad stop") }
	e := NewEngine().MustRegister("a", a)
	cancel, res := run(t, e)
	a.waitRunning(t)
	cancel()
	err := result(t, res)
	if err == nil || !strings.Contains(err.Error(), "bad stop") {
		t.Fatalf("err = %v", err)
	}
}

// 2.3: ResetAfter makes MaxRetries limit crash loops, not lifetime failures.
func TestResetAfter(t *testing.T) {
	ev := &events{}
	a := newFake("a", ev)
	var gate = make(chan struct{})
	a.execFn = func(ctx context.Context, call int) error {
		if call == 3 {
			<-ctx.Done()
			return ctx.Err()
		}
		select { // each run lasts long enough to qualify for a reset
		case <-gate:
		case <-time.After(20 * time.Millisecond):
		}
		return errors.New("fail")
	}
	e := NewEngine().WithStopOnFatal(false).MustRegister("a", a, RestartPolicy{MaxRetries: 1, ResetAfter: 10 * time.Millisecond})
	_, _ = run(t, e)
	for i := 0; i < 3; i++ {
		a.waitRunning(t)
	}
	info := waitState(t, e, "a", StateRunning)
	if info.Retries != 1 {
		t.Fatalf("retries = %d, want 1 (reset before each failure)", info.Retries)
	}
}

func TestRestartOnExit(t *testing.T) {
	ev := &events{}
	a := newFake("a", ev)
	a.execFn = func(ctx context.Context, call int) error {
		if call == 1 {
			return nil
		}
		<-ctx.Done()
		return ctx.Err()
	}
	e := NewEngine().MustRegister("a", a, RestartPolicy{RestartOnExit: true})
	_, _ = run(t, e)
	a.waitRunning(t)
	a.waitRunning(t)
	info := waitState(t, e, "a", StateRunning)
	if info.Retries != 0 {
		t.Fatalf("clean exit must not count as retry, got %d", info.Retries)
	}
}

// 2.4: apps are stopped one at a time in reverse order; each app's Exec has
// finished before the previous app's context is cancelled.
func TestOrderedShutdown(t *testing.T) {
	ev := &events{}
	apps := []*fakeApp{newFake("db", ev), newFake("bus", ev), newFake("http", ev)}
	e := NewEngine()
	for _, a := range apps {
		e.MustRegister(a.name, a)
	}
	cancel, res := run(t, e)
	for _, a := range apps {
		a.waitRunning(t)
	}
	cancel()
	if err := result(t, res); err != nil {
		t.Fatal(err)
	}

	var shutdown []string
	for _, s := range ev.snapshot() {
		if strings.HasSuffix(s, ":stop") || strings.HasSuffix(s, ":exec-done") {
			shutdown = append(shutdown, s)
		}
	}
	// Within one app, Stop and exec-done race (cancel happens first), so
	// compare per-app groups: all of http's events, then bus, then db.
	var groups []string
	for _, s := range shutdown {
		name := strings.SplitN(s, ":", 2)[0]
		if len(groups) == 0 || groups[len(groups)-1] != name {
			groups = append(groups, name)
		}
	}
	want := []string{"http", "bus", "db"}
	if fmt.Sprint(groups) != fmt.Sprint(want) {
		t.Fatalf("shutdown order %v, want %v (events: %v)", groups, want, shutdown)
	}
}

// 2.5: a hanging Stop is bounded by the shutdown timeout and named in the error.
func TestShutdownTimeoutCoversStop(t *testing.T) {
	ev := &events{}
	a := newFake("db", ev)
	b := newFake("bot", ev)
	release := make(chan struct{})
	defer close(release)
	b.stopFn = func() []error { <-release; return nil }
	e := NewEngine().WithShutdownTimeout(50 * time.Millisecond).MustRegister("db", a).MustRegister("bot", b)
	cancel, res := run(t, e)
	a.waitRunning(t)
	b.waitRunning(t)
	cancel()
	err := result(t, res)
	if err == nil || !strings.Contains(err.Error(), `while stopping app "bot"`) {
		t.Fatalf("err = %v", err)
	}
}

// 2.7: by default a failed app shuts the engine down and RunContext reports it.
func TestStopOnFatalDefault(t *testing.T) {
	ev := &events{}
	a := newFake("a", ev)
	b := newFake("b", ev)
	b.execFn = func(context.Context, int) error { return errors.New("port in use") }
	e := NewEngine().MustRegister("a", a).MustRegister("b", b)
	_, res := run(t, e)
	err := result(t, res)
	if err == nil || !strings.Contains(err.Error(), `app "b" failed`) || !strings.Contains(err.Error(), "port in use") {
		t.Fatalf("err = %v", err)
	}
	if ev.count("a:stop") != 1 || ev.count("b:stop") != 1 {
		t.Fatalf("each app must be stopped once, log: %v", ev.snapshot())
	}
}

func TestStopOnFatalDisabled(t *testing.T) {
	ev := &events{}
	a := newFake("a", ev)
	b := newFake("b", ev)
	b.execFn = func(context.Context, int) error { return errors.New("boom") }
	var fatalName atomic.Value
	e := NewEngine().WithStopOnFatal(false).
		WithFatalHandler(func(name string, _ error) { fatalName.Store(name) }).
		MustRegister("a", a).MustRegister("b", b)
	cancel, res := run(t, e)
	waitState(t, e, "b", StateFailed)
	waitState(t, e, "a", StateRunning)
	// 2.8: the failed app is stopped right away, not at shutdown.
	if ev.count("b:stop") != 1 {
		t.Fatalf("failed app not stopped, log: %v", ev.snapshot())
	}
	if fatalName.Load() != "b" {
		t.Fatalf("fatal handler got %v", fatalName.Load())
	}
	cancel()
	if err := result(t, res); err != nil {
		t.Fatalf("RunContext: %v", err)
	}
	if ev.count("b:stop") != 1 {
		t.Fatalf("Stop called again for failed app, log: %v", ev.snapshot())
	}
}

func TestPerAppOnFatalOverridesEngineHandler(t *testing.T) {
	ev := &events{}
	a := newFake("a", ev)
	a.execFn = func(context.Context, int) error { return errors.New("boom") }
	var engineCalled, appCalled atomic.Bool
	e := NewEngine().
		WithFatalHandler(func(string, error) { engineCalled.Store(true) }).
		MustRegister("a", a, RestartPolicy{OnFatal: func(error) { appCalled.Store(true) }})
	_, res := run(t, e)
	_ = result(t, res)
	if !appCalled.Load() || engineCalled.Load() {
		t.Fatalf("appCalled=%v engineCalled=%v", appCalled.Load(), engineCalled.Load())
	}
}

func TestReinitFailure(t *testing.T) {
	ev := &events{}
	a := newFake("a", ev)
	a.initFn = func(_ context.Context, call int) error {
		if call == 2 {
			return errors.New("db gone")
		}
		return nil
	}
	a.execFn = func(context.Context, int) error { return errors.New("boom") }
	e := NewEngine().WithStopOnFatal(false).MustRegister("a", a, RestartPolicy{MaxRetries: 5})
	_, _ = run(t, e)
	info := waitState(t, e, "a", StateFailed)
	if !strings.Contains(info.Err.Error(), "re-init failed: db gone") {
		t.Fatalf("err = %v", info.Err)
	}
}

func TestStopAppAndRestartApp(t *testing.T) {
	ev := &events{}
	a := newFake("a", ev)
	e := NewEngine().MustRegister("a", a)
	cancel, res := run(t, e)
	a.waitRunning(t)

	if err := e.StopApp("a"); err != nil {
		t.Fatal(err)
	}
	waitState(t, e, "a", StateStopped)

	if err := e.RestartApp("a"); err != nil {
		t.Fatal(err)
	}
	a.waitRunning(t)
	waitState(t, e, "a", StateRunning)

	cancel()
	if err := result(t, res); err != nil {
		t.Fatal(err)
	}
	if inits, stops := ev.count("a:init"), ev.count("a:stop"); inits != 2 || stops != 2 {
		t.Fatalf("inits=%d stops=%d, want 2/2; log: %v", inits, stops, ev.snapshot())
	}
}

func TestRestartAppWhenNotRunning(t *testing.T) {
	e := NewEngine().MustRegister("a", newFake("a", &events{}))
	if err := e.RestartApp("a"); err == nil {
		t.Fatal("expected error")
	}
}

func TestRestartAppPanicInInit(t *testing.T) {
	ev := &events{}
	a := newFake("a", ev)
	a.initFn = func(_ context.Context, call int) error {
		if call == 2 {
			panic("bad reinit")
		}
		return nil
	}
	e := NewEngine().MustRegister("a", a)
	_, _ = run(t, e)
	a.waitRunning(t)
	err := e.RestartApp("a")
	if err == nil || !strings.Contains(err.Error(), "bad reinit") {
		t.Fatalf("err = %v", err)
	}
}

func TestRegisterValidation(t *testing.T) {
	e := NewEngine()
	if _, err := e.Register("a", nil); err == nil {
		t.Fatal("nil app accepted")
	}
	e.MustRegister("a", newFake("a", &events{}))
	if _, err := e.Register("a", newFake("a", &events{})); err == nil {
		t.Fatal("duplicate name accepted")
	}
}

func TestRunTwiceConcurrently(t *testing.T) {
	a := newFake("a", &events{})
	e := NewEngine().MustRegister("a", a)
	_, _ = run(t, e)
	a.waitRunning(t)
	if err := e.RunContext(context.Background()); err == nil {
		t.Fatal("second RunContext must fail while running")
	}
}

// Stress: shutdown racing with a crash-looping app must not deadlock or
// leave an Init without a matching Stop.
func TestShutdownDuringRestartLoop(t *testing.T) {
	for i := 0; i < 50; i++ {
		ev := &events{}
		a := newFake("a", ev)
		a.execFn = func(context.Context, int) error { return errors.New("crash") }
		e := NewEngine().MustRegister("a", a, RestartPolicy{MaxRetries: -1})
		cancel, res := run(t, e)
		a.waitRunning(t)
		cancel()
		if err := result(t, res); err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		if inits, stops := ev.count("a:init"), ev.count("a:stop"); inits != stops {
			t.Fatalf("iteration %d: inits=%d stops=%d", i, inits, stops)
		}
	}
}
