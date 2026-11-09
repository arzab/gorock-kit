package rockservices

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

type log struct {
	mu sync.Mutex
	ev []string
}

func (l *log) add(s string) { l.mu.Lock(); l.ev = append(l.ev, s); l.mu.Unlock() }
func (l *log) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.ev, " ")
}

// fake is a service; its behaviour is set per test.
type fake struct {
	name    string
	l       *log
	initErr error
	stopErr error
	block   chan struct{}
	dep     *fake
}

func (f *fake) Name() string               { return f.name }
func (f *fake) Init(context.Context) error { f.l.add(f.name + ":init"); return f.initErr }
func (f *fake) Stop() error {
	f.l.add(f.name + ":stop")
	if f.block != nil {
		<-f.block
	}
	return f.stopErr
}

type Repo interface {
	Service
	Find() string
}

func (f *fake) Find() string { return "found by " + f.name }

func mustPanic(t *testing.T, want string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil || !strings.Contains(fmt.Sprint(r), want) {
			t.Fatalf("panic = %v, want containing %q", r, want)
		}
	}()
	fn()
}

func ctor(l *log, name string) func(struct{}) *fake {
	return func(struct{}) *fake { return &fake{name: name, l: l} }
}

func TestOrderAndTypedGet(t *testing.T) {
	l := &log{}
	c := New()
	Add(c, ctor(l, "db"), struct{}{})
	Add(c, ctor(l, "repo"), struct{}{})

	if got := GetFrom[Repo](c, "repo").Find(); got != "found by repo" {
		t.Fatalf("Find = %q", got)
	}
	if err := c.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.Stop(); err != nil {
		t.Fatal(err)
	}
	if got := l.String(); got != "db:init repo:init repo:stop db:stop" {
		t.Fatalf("order = %s", got)
	}
	if fmt.Sprint(c.Names()) != "[db repo]" {
		t.Fatalf("Names = %v", c.Names())
	}
}

// A constructor gets dependencies added before it.
func TestConstructorGetsEarlierService(t *testing.T) {
	l := &log{}
	c := New()
	Add(c, ctor(l, "db"), struct{}{})
	repo := Add(c, func(struct{}) *fake {
		return &fake{name: "repo", l: l, dep: GetFrom[*fake](c, "db")}
	}, struct{}{})
	if repo.dep == nil || repo.dep.name != "db" {
		t.Fatal("dependency not wired")
	}

	mustPanic(t, `constructing *rockservices.fake: rockservices: service "late" not found`, func() {
		Add(c, func(struct{}) *fake { return GetFrom[*fake](c, "late") }, struct{}{})
	})
}

func TestGetMisuse(t *testing.T) {
	c := New()
	Add(c, ctor(&log{}, "db"), struct{}{})
	mustPanic(t, `service "nope" not found`, func() { GetFrom[Repo](c, "nope") })
	mustPanic(t, `service "db" is *rockservices.fake, not *string`, func() { GetFrom[string](c, "db") })
}

func TestAddMisuse(t *testing.T) {
	l := &log{}
	c := New()
	mustPanic(t, "has an empty Name()", func() { Add(c, ctor(l, ""), struct{}{}) })
	Add(c, ctor(l, "db"), struct{}{})
	mustPanic(t, `"db" added twice`, func() { Add(c, ctor(l, "db"), struct{}{}) })
	mustPanic(t, `constructor of rockservices.Repo returned nil`, func() {
		Add(c, func(struct{}) Repo { return nil }, struct{}{})
	})
	mustPanic(t, `constructing *rockservices.fake: bad config`, func() {
		Add(c, func(struct{}) *fake { panic("bad config") }, struct{}{})
	})
	if err := c.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	mustPanic(t, `*rockservices.fake added after Init`, func() { Add(c, ctor(l, "late"), struct{}{}) })
}

func TestInitFailureRollsBack(t *testing.T) {
	l := &log{}
	c := New()
	Add(c, ctor(l, "db"), struct{}{})
	Add(c, func(struct{}) *fake { return &fake{name: "cache", l: l, initErr: errors.New("refused")} }, struct{}{})
	Add(c, ctor(l, "repo"), struct{}{})

	err := c.Init(context.Background())
	if err == nil || !strings.Contains(err.Error(), `service "cache": init: refused`) {
		t.Fatalf("err = %v", err)
	}
	if got := l.String(); got != "db:init cache:init db:stop" {
		t.Fatalf("order = %s (cache failed, so only db is stopped; repo never inits)", got)
	}
	if err := c.Stop(); err != nil { // nothing left to stop
		t.Fatal(err)
	}
}

func TestStopCollectsErrorsAndIsIdempotent(t *testing.T) {
	l := &log{}
	c := New()
	Add(c, func(struct{}) *fake { return &fake{name: "a", l: l, stopErr: errors.New("a failed")} }, struct{}{})
	Add(c, func(struct{}) *fake { return &fake{name: "b", l: l, stopErr: errors.New("b failed")} }, struct{}{})
	if err := c.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	err := c.Stop()
	if err == nil || !strings.Contains(err.Error(), `service "a": stop: a failed`) || !strings.Contains(err.Error(), `service "b": stop: b failed`) {
		t.Fatalf("err = %v", err)
	}
	if err := c.Stop(); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
	if strings.Count(l.String(), ":stop") != 2 {
		t.Fatalf("stopped more than once: %s", l)
	}
}

func TestStopTimeoutNamesService(t *testing.T) {
	l := &log{}
	release := make(chan struct{})
	defer close(release)
	c := New().WithStopTimeout(50 * time.Millisecond)
	Add(c, func(struct{}) *fake { return &fake{name: "pool", l: l, block: release} }, struct{}{})
	if err := c.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	err := c.Stop()
	if err == nil || !strings.Contains(err.Error(), `while stopping service "pool"`) {
		t.Fatalf("err = %v", err)
	}
}

type panicky struct{ inInit, inStop bool }

func (p *panicky) Name() string { return "p" }

func (p *panicky) Init(context.Context) error {
	if p.inInit {
		panic("init boom")
	}
	return nil
}
func (p *panicky) Stop() error {
	if p.inStop {
		panic("stop boom")
	}
	return nil
}

func TestPanicsAreErrors(t *testing.T) {
	c := New()
	Add(c, func(struct{}) *panicky { return &panicky{inInit: true} }, struct{}{})
	if err := c.Init(context.Background()); err == nil || !strings.Contains(err.Error(), "init boom") {
		t.Fatalf("init err = %v", err)
	}

	c = New()
	Add(c, func(struct{}) *panicky { return &panicky{inStop: true} }, struct{}{})
	if err := c.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.Stop(); err == nil || !strings.Contains(err.Error(), "stop boom") {
		t.Fatalf("stop err = %v", err)
	}
}

func TestDefaultCatalog(t *testing.T) {
	prev := SetDefault(New())
	t.Cleanup(func() { SetDefault(prev) })

	l := &log{}
	Add(Default(), ctor(l, "repo"), struct{}{})
	if Get[Repo]("repo").Find() != "found by repo" {
		t.Fatal("Get from default catalog failed")
	}
	if err := Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := Stop(); err != nil {
		t.Fatal(err)
	}
	if l.String() != "repo:init repo:stop" {
		t.Fatalf("order = %s", l)
	}
}

// Tests of business code swap in a catalog with fakes; Get sees them.
func TestSetDefaultForTests(t *testing.T) {
	fakes := New()
	Add(fakes, ctor(&log{}, "repository"), struct{}{})
	prev := SetDefault(fakes)
	if Default() != fakes || Get[Repo]("repository").Find() != "found by repository" {
		t.Fatal("Get does not see the swapped catalog")
	}
	if SetDefault(prev) != fakes || Default() != prev {
		t.Fatal("restore failed")
	}
	mustPanic(t, "SetDefault(nil)", func() { SetDefault(nil) })
}
