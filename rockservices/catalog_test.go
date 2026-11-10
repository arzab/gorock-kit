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

func (f *fake) Init(context.Context) error { f.l.add(f.name + ":init"); return f.initErr }
func (f *fake) Stop() error {
	f.l.add(f.name + ":stop")
	if f.block != nil {
		<-f.block
	}
	return f.stopErr
}

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

func TestInitOrderAndReverseStop(t *testing.T) {
	l := &log{}
	c := New(map[string]Service{
		"db":   &fake{name: "db", l: l},
		"repo": &fake{name: "repo", l: l},
	})
	if err := c.Init(context.Background(), "db", "repo"); err != nil {
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

func TestStopInGivenOrder(t *testing.T) {
	l := &log{}
	c := New(map[string]Service{"a": &fake{name: "a", l: l}, "b": &fake{name: "b", l: l}, "c": &fake{name: "c", l: l}})
	if err := c.Init(context.Background(), "a", "b", "c"); err != nil {
		t.Fatal(err)
	}
	if err := c.Stop("a", "c"); err != nil {
		t.Fatal(err)
	}
	if err := c.Stop(); err != nil { // stops what is left: b
		t.Fatal(err)
	}
	if got := l.String(); got != "a:init b:init c:init a:stop c:stop b:stop" {
		t.Fatalf("order = %s", got)
	}
}

func TestGet(t *testing.T) {
	repo := &fake{name: "repo", l: &log{}}
	c := New(map[string]Service{"repo": repo})
	if c.Get("repo").(*fake) != repo {
		t.Fatal("Get returned another service")
	}
	mustPanic(t, `service "nope" not found`, func() { c.Get("nope") })
}

// A child initialised outside its parent is passed to the parent's constructor.
func TestChildThroughConstructor(t *testing.T) {
	l := &log{}
	db := &fake{name: "db", l: l}
	repo := &fake{name: "repo", l: l, dep: db}
	c := New(map[string]Service{"db": db, "repo": repo})
	if c.Get("repo").(*fake).dep != c.Get("db").(*fake) {
		t.Fatal("dependency not shared")
	}
}

func TestMisuse(t *testing.T) {
	l := &log{}
	mustPanic(t, "empty name", func() { New(map[string]Service{"": &fake{l: l}}) })
	mustPanic(t, `service "x" is nil`, func() { New(map[string]Service{"x": nil}) })

	c := New(map[string]Service{"db": &fake{name: "db", l: l}})
	mustPanic(t, `service "nope" not found`, func() { _ = c.Init(context.Background(), "db", "nope") })
	if l.String() != "" {
		t.Fatalf("Init started services before checking names: %s", l)
	}
	mustPanic(t, `service "nope" not found`, func() { _ = c.Stop("nope") })

	if err := c.Init(context.Background(), "db"); err != nil {
		t.Fatal(err)
	}
	if err := c.Init(context.Background(), "db"); err == nil || !strings.Contains(err.Error(), "already initialised") {
		t.Fatalf("second Init = %v", err)
	}
	_ = c.Stop()
}

func TestInitFailureRollsBack(t *testing.T) {
	l := &log{}
	c := New(map[string]Service{
		"db":    &fake{name: "db", l: l},
		"cache": &fake{name: "cache", l: l, initErr: errors.New("refused")},
		"repo":  &fake{name: "repo", l: l},
	})
	err := c.Init(context.Background(), "db", "cache", "repo")
	if err == nil || !strings.Contains(err.Error(), `service "cache": init: refused`) {
		t.Fatalf("err = %v", err)
	}
	if got := l.String(); got != "db:init cache:init db:stop" {
		t.Fatalf("order = %s (cache failed: only db is stopped, repo never inits)", got)
	}
	if err := c.Stop(); err != nil { // nothing left to stop
		t.Fatal(err)
	}
}

func TestStopCollectsErrorsAndIsIdempotent(t *testing.T) {
	l := &log{}
	c := New(map[string]Service{
		"a": &fake{name: "a", l: l, stopErr: errors.New("a failed")},
		"b": &fake{name: "b", l: l, stopErr: errors.New("b failed")},
	})
	if err := c.Init(context.Background(), "a", "b"); err != nil {
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
	release := make(chan struct{})
	defer close(release)
	c := New(map[string]Service{"pool": &fake{name: "pool", l: &log{}, block: release}}).WithStopTimeout(50 * time.Millisecond)
	if err := c.Init(context.Background(), "pool"); err != nil {
		t.Fatal(err)
	}
	err := c.Stop()
	if err == nil || !strings.Contains(err.Error(), `while stopping service "pool"`) {
		t.Fatalf("err = %v", err)
	}
}

type panicky struct{ inInit, inStop bool }

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
	c := New(map[string]Service{"p": &panicky{inInit: true}})
	if err := c.Init(context.Background(), "p"); err == nil || !strings.Contains(err.Error(), "init boom") {
		t.Fatalf("init err = %v", err)
	}
	c = New(map[string]Service{"p": &panicky{inStop: true}})
	if err := c.Init(context.Background(), "p"); err != nil {
		t.Fatal(err)
	}
	if err := c.Stop(); err == nil || !strings.Contains(err.Error(), "stop boom") {
		t.Fatalf("stop err = %v", err)
	}
}
