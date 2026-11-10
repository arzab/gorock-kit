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

// fk declares a key for a *fake service.
func fk(name string) *Key[*fake] { return NewKey[*fake](name) }

// isolateDeclared gives the test an empty registry of declared keys.
func isolateDeclared(t *testing.T) {
	t.Helper()
	declaredMu.Lock()
	saved := declared
	declared = nil
	declaredMu.Unlock()
	t.Cleanup(func() {
		declaredMu.Lock()
		declared = saved
		declaredMu.Unlock()
	})
}

func ctor(l *log, name string) func(struct{}) *fake {
	return func(struct{}) *fake { return &fake{name: name, l: l} }
}

func TestOrderAndTypedGet(t *testing.T) {
	l := &log{}
	c := New()
	Add(c, fk("db"), ctor(l, "db"), struct{}{})
	Add(c, fk("repo"), ctor(l, "repo"), struct{}{})

	if got := NewKey[Repo]("repo").From(c).Find(); got != "found by repo" {
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
	Add(c, fk("db"), ctor(l, "db"), struct{}{})
	repo := Add(c, fk("repo"), func(struct{}) *fake {
		return &fake{name: "repo", l: l, dep: fk("db").From(c)}
	}, struct{}{})
	if repo.dep == nil || repo.dep.name != "db" {
		t.Fatal("dependency not wired")
	}

	mustPanic(t, `constructing service "cache" (*rockservices.fake): rockservices: service "late" is not in the catalog`, func() {
		Add(c, fk("cache"), func(struct{}) *fake { return fk("late").From(c) }, struct{}{})
	})
}

func TestGetMisuse(t *testing.T) {
	c := New()
	Add(c, fk("db"), ctor(&log{}, "db"), struct{}{})
	mustPanic(t, `service "nope" is not in the catalog`, func() { NewKey[Repo]("nope").From(c) })
	mustPanic(t, `service "db" is *rockservices.fake, not *rockservices.panicky`, func() { NewKey[*panicky]("db").From(c) })
}

func TestAddMisuse(t *testing.T) {
	l := &log{}
	c := New()
	mustPanic(t, "NewKey[*rockservices.fake] with an empty name", func() { fk("") })
	mustPanic(t, "added with a nil key", func() { Add(c, (*Key[*fake])(nil), ctor(l, "x"), struct{}{}) })
	// the name belongs to the registration: one service type, two instances
	Add(c, fk("orders_db"), ctor(l, "x"), struct{}{})
	Add(c, fk("analytics_db"), ctor(l, "x"), struct{}{})
	Add(c, fk("db"), ctor(l, "db"), struct{}{})
	// a second key with the same name is a duplicate too
	mustPanic(t, `"db" added twice`, func() { Add(c, fk("db"), ctor(l, "db"), struct{}{}) })
	mustPanic(t, `constructor of service "nil" (rockservices.Repo) returned nil`, func() {
		Add(c, NewKey[Repo]("nil"), func(struct{}) Repo { return nil }, struct{}{})
	})
	mustPanic(t, `constructing service "bad" (*rockservices.fake): bad config`, func() {
		Add(c, fk("bad"), func(struct{}) *fake { panic("bad config") }, struct{}{})
	})
	if err := c.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	mustPanic(t, `"late" (*rockservices.fake) added after Init`, func() { Add(c, fk("late"), ctor(l, "late"), struct{}{}) })
}

func TestInitFailureRollsBack(t *testing.T) {
	l := &log{}
	c := New()
	Add(c, fk("db"), ctor(l, "db"), struct{}{})
	Add(c, fk("cache"), func(struct{}) *fake { return &fake{name: "cache", l: l, initErr: errors.New("refused")} }, struct{}{})
	Add(c, fk("repo"), ctor(l, "repo"), struct{}{})

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
	Add(c, fk("a"), func(struct{}) *fake { return &fake{name: "a", l: l, stopErr: errors.New("a failed")} }, struct{}{})
	Add(c, fk("b"), func(struct{}) *fake { return &fake{name: "b", l: l, stopErr: errors.New("b failed")} }, struct{}{})
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
	Add(c, fk("pool"), func(struct{}) *fake { return &fake{name: "pool", l: l, block: release} }, struct{}{})
	if err := c.Init(context.Background()); err != nil {
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
	c := New()
	Add(c, NewKey[*panicky]("p"), func(struct{}) *panicky { return &panicky{inInit: true} }, struct{}{})
	if err := c.Init(context.Background()); err == nil || !strings.Contains(err.Error(), "init boom") {
		t.Fatalf("init err = %v", err)
	}

	c = New()
	Add(c, NewKey[*panicky]("p"), func(struct{}) *panicky { return &panicky{inStop: true} }, struct{}{})
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
	Add(Default(), fk("repo"), ctor(l, "repo"), struct{}{})
	if NewKey[Repo]("repo").Get().Find() != "found by repo" {
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
	Add(fakes, fk("repository"), ctor(&log{}, "repository"), struct{}{})
	prev := SetDefault(fakes)
	if Default() != fakes || NewKey[Repo]("repository").Get().Find() != "found by repository" {
		t.Fatal("Get does not see the swapped catalog")
	}
	if SetDefault(prev) != fakes || Default() != prev {
		t.Fatal("restore failed")
	}
	mustPanic(t, "SetDefault(nil)", func() { SetDefault(nil) })
}

// Init of the default catalog fails if a declared key was never added.
func TestInitChecksDeclaredKeys(t *testing.T) {
	isolateDeclared(t)
	l := &log{}
	orders := fk("orders")
	_ = fk("payments") // declared, never added
	c := New()
	c.checkDeclared = true // as the built-in default catalog
	Add(c, orders, ctor(l, "orders"), struct{}{})

	err := c.Init(context.Background())
	if err == nil || !strings.Contains(err.Error(), `["payments"]`) {
		t.Fatalf("err = %v", err)
	}
	if l.String() != "" {
		t.Fatalf("services were initialised before the check: %s", l)
	}

	// catalogs from New() don't check: tests add only the fakes they need
	plain := New()
	Add(plain, orders, ctor(l, "orders"), struct{}{})
	if err := plain.Init(context.Background()); err != nil {
		t.Fatalf("New() catalog checked declared keys: %v", err)
	}
	_ = plain.Stop()
}

// From reads the given catalog, Get the default one.
func TestFromReadsItsCatalog(t *testing.T) {
	orders := fk("from-orders")
	c := New()
	Add(c, orders, ctor(&log{}, "from-orders"), struct{}{})
	if orders.From(c).name != "from-orders" {
		t.Fatal("From(c) did not read c")
	}
	mustPanic(t, `service "from-orders" is not in the catalog (add it in services.Register)`, func() { orders.Get() })
	if orders.Name() != "from-orders" || orders.String() != "from-orders" {
		t.Fatal("Name/String")
	}
}

// Dependencies are wired through the value Add returned, so registering into
// a catalog from New() never touches Default().
func TestWiringByReturnedValue(t *testing.T) {
	before := Default().Names()
	l := &log{}
	c := New()
	db := Add(c, fk("wire-db"), ctor(l, "wire-db"), struct{}{})
	repo := Add(c, fk("wire-repo"), func(struct{}) *fake {
		return &fake{name: "wire-repo", l: l, dep: db}
	}, struct{}{})
	if repo.dep != db {
		t.Fatal("dependency not wired")
	}
	if fmt.Sprint(Default().Names()) != fmt.Sprint(before) {
		t.Fatalf("Default() changed: %v", Default().Names())
	}
}

func TestInitTwice(t *testing.T) {
	l := &log{}
	c := New()
	Add(c, fk("twice"), ctor(l, "twice"), struct{}{})
	if err := c.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.Init(context.Background()); !errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("second Init = %v", err)
	}
	if l.String() != "twice:init" {
		t.Fatalf("services initialised twice: %s", l)
	}
	_ = c.Stop()
}
