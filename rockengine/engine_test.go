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
// channel with Run's result.
func run(t *testing.T, e *Engine, names ...string) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	res := make(chan error, 1)
	go func() { res <- e.Run(ctx, names...) }()
	t.Cleanup(cancel)
	return cancel, res
}

func result(t *testing.T, res <-chan error) error {
	t.Helper()
	select {
	case err := <-res:
		return err
	case <-time.After(testTimeout):
		t.Fatal("Run did not return")
		return nil
	}
}

func index(log []string, s string) int {
	for i, v := range log {
		if v == s {
			return i
		}
	}
	return -1
}

func TestOrderedStartAndShutdown(t *testing.T) {
	ev := &events{}
	a, b := newFake("a", ev), newFake("b", ev)
	cancel, res := run(t, New(map[string]App{"a": a, "b": b}), "a", "b")
	a.waitRunning(t)
	b.waitRunning(t)
	cancel()
	if err := result(t, res); err != nil {
		t.Fatalf("Run = %v", err)
	}
	log := ev.snapshot()
	if index(log, "a:init") > index(log, "b:init") {
		t.Fatalf("init order: %v", log)
	}
	// b stops first and fully (its Exec returns) before a is touched
	if index(log, "b:exec-done") > index(log, "a:stop") {
		t.Fatalf("shutdown order: %v", log)
	}
	if ev.count("a:stop") != 1 || ev.count("b:stop") != 1 {
		t.Fatalf("Stop called more than once: %v", log)
	}
}

func TestErrorAfterCancelIsCleanStop(t *testing.T) {
	ev := &events{}
	srv := newFake("http", ev)
	srv.execFn = func(ctx context.Context, _ int) error {
		<-ctx.Done()
		return http.ErrServerClosed
	}
	cancel, res := run(t, New(map[string]App{"http": srv}), "http")
	srv.waitRunning(t)
	cancel()
	if err := result(t, res); err != nil {
		t.Fatalf("Run = %v", err)
	}
}

func TestExecFailureStopsEverything(t *testing.T) {
	ev := &events{}
	a, b := newFake("a", ev), newFake("b", ev)
	b.execFn = func(context.Context, int) error { return errors.New("port busy") }
	_, res := run(t, New(map[string]App{"a": a, "b": b}), "a", "b")
	err := result(t, res)
	if err == nil || !strings.Contains(err.Error(), `app "b" failed: port busy`) {
		t.Fatalf("Run = %v", err)
	}
	if ev.count("a:stop") != 1 || ev.count("b:stop") != 1 {
		t.Fatalf("stops: %v", ev.snapshot())
	}
}

func TestPanics(t *testing.T) {
	ev := &events{}
	p := newFake("p", ev)
	p.execFn = func(context.Context, int) error { panic("exec boom") }
	if err := result(t, second(run(t, New(map[string]App{"p": p}), "p"))); err == nil || !strings.Contains(err.Error(), "exec boom") {
		t.Fatalf("exec panic: %v", err)
	}

	q := newFake("q", ev)
	q.initFn = func(context.Context, int) error { panic("init boom") }
	if err := New(map[string]App{"q": q}).Run(context.Background(), "q"); err == nil || !strings.Contains(err.Error(), "init boom") {
		t.Fatalf("init panic: %v", err)
	}

	s := newFake("s", ev)
	s.stopFn = func() []error { panic("stop boom") }
	cancel, res := run(t, New(map[string]App{"s": s}), "s")
	s.waitRunning(t)
	cancel()
	if err := result(t, res); err == nil || !strings.Contains(err.Error(), "stop boom") {
		t.Fatalf("stop panic: %v", err)
	}
}

func second(_ context.CancelFunc, res <-chan error) <-chan error { return res }

func TestInitFailureRollsBack(t *testing.T) {
	ev := &events{}
	a, b, c := newFake("a", ev), newFake("b", ev), newFake("c", ev)
	b.initFn = func(context.Context, int) error { return errors.New("no db") }
	err := New(map[string]App{"a": a, "b": b, "c": c}).Run(context.Background(), "a", "b", "c")
	if err == nil || !strings.Contains(err.Error(), `app "b": init: no db`) {
		t.Fatalf("Run = %v", err)
	}
	if got := fmt.Sprint(ev.snapshot()); got != "[a:init b:init a:stop]" {
		t.Fatalf("events: %s", got)
	}
}

func TestRestarts(t *testing.T) {
	ev := &events{}
	a := newFake("a", ev)
	a.execFn = func(ctx context.Context, n int) error {
		if n <= 2 {
			return errors.New("lost connection")
		}
		<-ctx.Done()
		return ctx.Err()
	}
	cancel, res := run(t, New(map[string]App{"a": a}).WithRestarts(2, time.Millisecond), "a")
	for range 3 {
		a.waitRunning(t)
	}
	cancel()
	if err := result(t, res); err != nil {
		t.Fatalf("Run = %v", err)
	}
	if a.inits.Load() != 3 || ev.count("a:stop") != 3 {
		t.Fatalf("inits %d, events %v", a.inits.Load(), ev.snapshot())
	}
}

func TestRestartsExhausted(t *testing.T) {
	ev := &events{}
	a := newFake("a", ev)
	a.execFn = func(context.Context, int) error { return errors.New("down") }
	_, res := run(t, New(map[string]App{"a": a}).WithRestarts(2, 0), "a")
	if err := result(t, res); err == nil || !strings.Contains(err.Error(), `app "a" failed: down`) {
		t.Fatalf("Run = %v", err)
	}
	if a.execs.Load() != 3 {
		t.Fatalf("execs = %d, want 3", a.execs.Load())
	}

	b := newFake("b", ev)
	b.execFn = func(context.Context, int) error { return errors.New("down") }
	b.initFn = func(_ context.Context, n int) error {
		if n > 1 {
			return errors.New("still down")
		}
		return nil
	}
	_, res = run(t, New(map[string]App{"b": b}).WithRestarts(5, 0), "b")
	if err := result(t, res); err == nil || !strings.Contains(err.Error(), `app "b": restart: init: still down`) {
		t.Fatalf("restart init: %v", err)
	}
}

// An Exec that returns nil before shutdown just ends; the engine goes on.
func TestCleanExitKeepsEngineRunning(t *testing.T) {
	ev := &events{}
	job, srv := newFake("job", ev), newFake("srv", ev)
	job.execFn = func(context.Context, int) error { return nil }
	cancel, res := run(t, New(map[string]App{"job": job, "srv": srv}), "job", "srv")
	srv.waitRunning(t)
	select {
	case err := <-res:
		t.Fatalf("engine stopped after a clean exit: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
	if err := result(t, res); err != nil {
		t.Fatalf("Run = %v", err)
	}
}

func TestShutdownTimeoutNamesApp(t *testing.T) {
	ev := &events{}
	release := make(chan struct{})
	defer close(release)
	slow := newFake("slow", ev)
	slow.stopFn = func() []error { <-release; return nil }
	cancel, res := run(t, New(map[string]App{"slow": slow}).WithShutdownTimeout(50*time.Millisecond), "slow")
	slow.waitRunning(t)
	cancel()
	if err := result(t, res); err == nil || !strings.Contains(err.Error(), `while stopping app "slow"`) {
		t.Fatalf("Run = %v", err)
	}
}

func TestStopErrorsAreReturned(t *testing.T) {
	ev := &events{}
	a := newFake("a", ev)
	a.stopFn = func() []error { return []error{errors.New("flush failed"), nil} }
	cancel, res := run(t, New(map[string]App{"a": a}), "a")
	a.waitRunning(t)
	cancel()
	if err := result(t, res); err == nil || !strings.Contains(err.Error(), `app "a": stop: flush failed`) {
		t.Fatalf("Run = %v", err)
	}
}

func TestMisuse(t *testing.T) {
	mustPanic := func(want string, fn func()) {
		t.Helper()
		defer func() {
			if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), want) {
				t.Fatalf("panic = %v, want %q", r, want)
			}
		}()
		fn()
	}
	ev := &events{}
	mustPanic("empty name", func() { New(map[string]App{"": newFake("x", ev)}) })
	mustPanic(`app "x" is nil`, func() { New(map[string]App{"x": nil}) })
	e := New(map[string]App{"a": newFake("a", ev)})
	mustPanic(`app "nope" not found`, func() { _ = e.Run(context.Background(), "nope") })
	mustPanic(`app "a" listed twice`, func() { _ = e.Run(context.Background(), "a", "a") })
}
