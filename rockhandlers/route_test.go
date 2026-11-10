package rockhandlers_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	rh "github.com/arzab/gorock-kit/rockhandlers"
)

// --- a reusable step with a constraint, and a model that satisfies it by embedding ---

type TokenParams interface{ BearerToken() string }

type BearerAuth struct {
	Authorization string `header:"Authorization"`
}

func (a *BearerAuth) BearerToken() string { return strings.TrimPrefix(a.Authorization, "Bearer ") }

type User struct{ Name string }

var UserKey = rh.NewLocal[*User]("user")

func Bearer[P TokenParams, R any](c *rh.Ctx[P, R]) error {
	tok := c.Params.BearerToken()
	if tok == "" {
		return rh.Unauthorized("token required")
	}
	UserKey.Set(c, &User{Name: tok})
	return c.Next()
}

// --- models and a business step ---

type CreateParams struct {
	BearerAuth
	Item string
	Qty  int
}

func (p *CreateParams) Validate() error {
	if p.Qty <= 0 {
		return errors.New("qty must be > 0")
	}
	return nil
}

type CreateResult struct {
	ID    string
	Owner string
}

func create(c *rh.Ctx[*CreateParams, *CreateResult]) error {
	c.Result.ID = c.Params.Item + "-1"
	c.Result.Owner = UserKey.MustGet(c).Name
	return nil
}

// --- a group as a plain generic function ---

func Secured[P TokenParams, R any](name string, steps ...rh.Handler[P, R]) rh.Route {
	return rh.Chain(name, rh.Prepend(steps, Bearer[P, R])...)
}

func params(token string, qty int) *CreateParams {
	return &CreateParams{BearerAuth: BearerAuth{Authorization: "Bearer " + token}, Item: "tea", Qty: qty}
}

func TestChainInfersGenericSteps(t *testing.T) {
	route := rh.Chain("orders.create", Bearer, create) // Bearer instantiated from create's types
	res, err := rh.Run[*CreateResult](context.Background(), route, params("ann", 1))
	if err != nil {
		t.Fatal(err)
	}
	if res.ID != "tea-1" || res.Owner != "ann" {
		t.Fatalf("got %+v", res)
	}
}

func TestGroupFunction(t *testing.T) {
	route := Secured("orders.create", create)
	if _, err := rh.Run[*CreateResult](context.Background(), route, params("", 1)); rh.KindOf(err) != rh.KindUnauthorized {
		t.Fatalf("err = %v", err)
	}
	res, err := rh.Run[*CreateResult](context.Background(), route, params("bob", 1))
	if err != nil || res.Owner != "bob" {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

func TestValidation(t *testing.T) {
	route := rh.Chain("orders.create", Bearer, create)
	_, err := rh.Run[*CreateResult](context.Background(), route, params("ann", 0))
	if rh.KindOf(err) != rh.KindInvalid || rh.MessageOf(err) != "qty must be > 0" {
		t.Fatalf("err = %v", err)
	}
}

// --- order, early exit, repeated Next ---

type P struct{ N int }
type R struct{ Log []string }

func rec(name string) rh.Handler[*P, *R] {
	return func(c *rh.Ctx[*P, *R]) error {
		c.Result.Log = append(c.Result.Log, name)
		return c.Next()
	}
}

func mw(log *[]string, name string) rh.Middleware {
	return func(c rh.Context) error {
		*log = append(*log, name+">")
		err := c.Next()
		*log = append(*log, "<"+name)
		return err
	}
}

func TestOrder(t *testing.T) {
	var log []string
	route := rh.Chain("r", rec("s1"), rec("s2")).With(mw(&log, "a"), mw(&log, "b")).With(mw(&log, "transport"))
	res, err := rh.Run[*R](context.Background(), route, &P{})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(log, " "); got != "transport> a> b> <b <a <transport" {
		t.Fatalf("middlewares: %s", got)
	}
	if got := strings.Join(res.Log, " "); got != "s1 s2" {
		t.Fatalf("steps: %s", got)
	}
}

func TestEarlyExitAndRepeatedNext(t *testing.T) {
	calls := 0
	cache := func(c *rh.Ctx[*P, *R]) error {
		if c.Params.N == 1 {
			c.Result.Log = []string{"cached"}
			return rh.Done // early success: the rest of the chain is skipped
		}
		err1 := c.Next()
		err2 := c.Next() // no-op, same result
		if !errors.Is(err2, err1) && err1 != err2 {
			t.Errorf("repeated Next: %v vs %v", err1, err2)
		}
		return err1
	}
	count := func(c *rh.Ctx[*P, *R]) error { calls++; return c.Next() }
	route := rh.Chain("r", cache, count)

	res, err := rh.Run[*R](context.Background(), route, &P{N: 1})
	if err != nil || res.Log[0] != "cached" || calls != 0 {
		t.Fatalf("early exit: res %+v, err %v, calls %d", res, err, calls)
	}
	if _, err := rh.Run[*R](context.Background(), route, &P{N: 2}); err != nil || calls != 1 {
		t.Fatalf("repeated Next: err %v, calls %d", err, calls)
	}
}

// A step that returns nil without calling Next does not end the chain.
func TestStepWithoutNextContinues(t *testing.T) {
	quiet := func(name string) rh.Handler[*P, *R] {
		return func(c *rh.Ctx[*P, *R]) error {
			c.Result.Log = append(c.Result.Log, name)
			return nil // no Next
		}
	}
	res, err := rh.Run[*R](context.Background(), rh.Chain("r", quiet("a"), quiet("b"), quiet("c")), &P{})
	if err != nil || strings.Join(res.Log, " ") != "a b c" {
		t.Fatalf("res %+v, err %v", res, err)
	}

	// explicit Next: every step still runs exactly once
	res, err = rh.Run[*R](context.Background(), rh.Chain("r", rec("a"), quiet("b"), rec("c")), &P{})
	if err != nil || strings.Join(res.Log, " ") != "a b c" {
		t.Fatalf("mixed: res %+v, err %v", res, err)
	}
}

func TestDoneInTheMiddle(t *testing.T) {
	var seen []string
	mw := func(c rh.Context) error {
		err := c.Next()
		seen = append(seen, fmt.Sprint(err))
		return err
	}
	stop := func(c *rh.Ctx[*P, *R]) error {
		c.Result.Log = append(c.Result.Log, "stop")
		return rh.Done
	}
	route := rh.Chain("r", rec("a"), stop, rec("never")).With(mw)
	res, err := rh.Run[*R](context.Background(), route, &P{})
	if err != nil || strings.Join(res.Log, " ") != "a stop" {
		t.Fatalf("res %+v, err %v", res, err)
	}
	if fmt.Sprint(seen) != "[<nil>]" {
		t.Fatalf("middleware saw %v", seen)
	}
	// Done never leaves Serve
	if _, err := route.Serve(context.Background(), nil); err != nil {
		t.Fatalf("Serve = %v", err)
	}
}

func TestDoneFromMiddleware(t *testing.T) {
	validated, stepped := false, false
	route := rh.Chain("r", func(*rh.Ctx[*validating, *R]) error { stepped = true; return nil }).
		With(func(rh.Context) error { return rh.Done })
	res, err := rh.Run[*R](context.Background(), route, &validating{check: func() { validated = true }})
	if err != nil || res == nil || validated || stepped {
		t.Fatalf("res %v, err %v, validated %v, stepped %v", res, err, validated, stepped)
	}
}

type validating struct{ check func() }

func (v *validating) Validate() error {
	if v.check != nil {
		v.check()
	}
	return nil
}

// --- errors, panics, no leak ---

func TestErrorDropsResult(t *testing.T) {
	route := rh.Chain("r", func(c *rh.Ctx[*P, *R]) error {
		c.Result.Log = []string{"partial"}
		return rh.Conflict("duplicate")
	})
	res, err := route.Serve(context.Background(), nil)
	if res != nil || rh.KindOf(err) != rh.KindConflict {
		t.Fatalf("res %v, err %v", res, err)
	}
}

func TestPanicBecomesInternalAndMiddlewareSeesIt(t *testing.T) {
	var seen error
	catch := func(c rh.Context) error { seen = c.Next(); return seen }
	route := rh.Chain("r", func(*rh.Ctx[*P, *R]) error { panic("boom") }).With(catch)

	_, err := route.Serve(context.Background(), nil)
	var pe *rh.PanicError
	if rh.KindOf(err) != rh.KindInternal || !errors.As(err, &pe) || pe.Value != "boom" || len(pe.Stack) == 0 {
		t.Fatalf("err = %v", err)
	}
	if seen == nil {
		t.Fatal("middleware did not see the panic as an error")
	}
	if rh.MessageOf(err) != "" {
		t.Fatalf("Internal message leaked: %q", rh.MessageOf(err))
	}

	// panic in a middleware too
	route = rh.Chain("r", rec("s")).With(func(rh.Context) error { panic("mw") })
	if _, err := route.Serve(context.Background(), nil); rh.KindOf(err) != rh.KindInternal {
		t.Fatalf("mw panic: %v", err)
	}
}

func TestMustGetMissing(t *testing.T) {
	route := rh.Chain("r", func(c *rh.Ctx[*P, *R]) error { _ = UserKey.MustGet(c); return nil })
	_, err := route.Serve(context.Background(), nil)
	var pe *rh.PanicError
	if !errors.As(err, &pe) || !strings.Contains(pe.Error(), `local "user" is not set`) {
		t.Fatalf("err = %v", err)
	}
	if _, ok := UserKey.Get(&rh.Ctx[*P, *R]{}); ok {
		t.Fatal("Get on empty ctx: ok")
	}
}

func TestErrorHelpers(t *testing.T) {
	err := rh.Invalid("bad %s", "order").WithFields(map[string]string{"qty": "must be > 0"}).Wrap(errors.New("cause"))
	if rh.KindOf(err) != rh.KindInvalid || rh.MessageOf(err) != "bad order" || rh.FieldsOf(err)["qty"] != "must be > 0" {
		t.Fatalf("%v", err)
	}
	if err.Error() != "invalid: bad order: cause" {
		t.Fatalf("Error() = %q", err.Error())
	}
	if rh.KindOf(errors.New("x")) != rh.KindInternal || rh.KindOf(context.Canceled) != rh.KindCanceled ||
		rh.KindOf(context.DeadlineExceeded) != rh.KindTimeout {
		t.Fatal("KindOf classification")
	}
}

// --- cancellation and timeouts ---

func TestCanceledStopsBetweenSteps(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ran := false
	route := rh.Chain("r",
		func(c *rh.Ctx[*P, *R]) error { cancel(); return c.Next() },
		func(c *rh.Ctx[*P, *R]) error { ran = true; return nil },
	)
	_, err := route.Serve(ctx, nil)
	if ran || rh.KindOf(err) != rh.KindCanceled {
		t.Fatalf("ran %v, err %v", ran, err)
	}

	// already cancelled: nothing runs
	if _, err := route.Serve(ctx, nil); rh.KindOf(err) != rh.KindCanceled {
		t.Fatalf("err %v", err)
	}
}

func TestTimeoutIsCooperativeAndRestored(t *testing.T) {
	var inner, after context.Context
	outer := func(c rh.Context) error { err := c.Next(); after = c.Base(); return err }
	route := rh.Chain("r", func(c *rh.Ctx[*P, *R]) error {
		inner = c.Base()
		select {
		case <-c.Done(): // the step reacts to the signal immediately
			return c.Err()
		case <-time.After(5 * time.Second):
			return nil
		}
	}).With(outer, rh.Timeout(20*time.Millisecond))

	start := time.Now()
	_, err := route.Serve(context.Background(), nil)
	if rh.KindOf(err) != rh.KindTimeout || time.Since(start) > 2*time.Second {
		t.Fatalf("err %v after %v", err, time.Since(start))
	}
	if _, ok := inner.Deadline(); !ok {
		t.Fatal("step did not get a deadline")
	}
	if _, ok := after.Deadline(); ok {
		t.Fatal("Timeout did not restore the outer context")
	}
}

func TestCtxIsContext(t *testing.T) {
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "v")
	route := rh.Chain("r", func(c *rh.Ctx[*P, *R]) error {
		var std context.Context = c // usable anywhere a context.Context is
		if std.Value(key{}) != "v" || c.Route() != "r" {
			return errors.New("context not propagated")
		}
		return nil
	})
	if _, err := route.Serve(ctx, nil); err != nil {
		t.Fatal(err)
	}
}

// --- transport contract ---

func TestTransportContract(t *testing.T) {
	var log []string
	route := rh.Chain("r", rec("s")).With(func(c rh.Context) error {
		err := c.Next()
		log = append(log, rh.KindOf(err).String())
		return err
	})
	if route.Name() != "r" || route.ParamsType() != reflect.TypeFor[*P]() || route.ResultType() != reflect.TypeFor[*R]() {
		t.Fatal("accessors")
	}

	// fill gets a fresh *P inside the chain
	res, err := route.Serve(context.Background(), func(p any) error { p.(*P).N = 7; return nil })
	if _, ok := res.(*R); !ok || err != nil {
		t.Fatalf("Serve = %T, %v", res, err)
	}

	// a plain fill error becomes Invalid; an *Error keeps its kind; middlewares see both
	decodeErr := errors.New("json: cannot unmarshal string into Go struct field P.N of type int")
	_, err = route.Serve(context.Background(), func(any) error { return decodeErr })
	if rh.KindOf(err) != rh.KindInvalid || rh.MessageOf(err) != "invalid request" || !errors.Is(err, decodeErr) {
		t.Fatalf("plain fill error: %v", err)
	}
	_, err = route.Serve(context.Background(), func(any) error { return rh.Unauthorized("no") })
	if rh.KindOf(err) != rh.KindUnauthorized {
		t.Fatalf("*Error from fill: %v", err)
	}
	_, err = route.Serve(context.Background(), func(any) error { panic("decoder") })
	if rh.KindOf(err) != rh.KindInternal {
		t.Fatalf("panic in fill: %v", err)
	}
	if got := strings.Join(log, " "); got != "none invalid unauthorized internal" {
		t.Fatalf("middleware saw: %s", got)
	}

	if _, err := rh.Run[*R](context.Background(), route, &R{}); err == nil {
		t.Fatal("Run with wrong params type: no error")
	}
	if _, err := rh.Run[*R](context.Background(), route, (*P)(nil)); err == nil {
		t.Fatal("Run with nil params: no error")
	}
	if _, err := (rh.Route{}).Serve(context.Background(), nil); err == nil {
		t.Fatal("zero Route Serve: no error")
	}
	if rh.KindOf(nil) != rh.KindNone {
		t.Fatal("KindOf(nil)")
	}
}

func TestChainPanicsOnWiringBugs(t *testing.T) {
	type S struct{}
	cases := map[string]func(){
		"empty name": func() { rh.Chain[*P, *R]("", rec("s")) },
		"no steps":   func() { rh.Chain[*P, *R]("r") },
		"nil step":   func() { rh.Chain[*P, *R]("r", nil) },
		"non-ptr P":  func() { rh.Chain("r", func(*rh.Ctx[S, *R]) error { return nil }) },
		"non-ptr R":  func() { rh.Chain("r", func(*rh.Ctx[*P, int]) error { return nil }) },
		"nil mw":     func() { rh.Chain("r", rec("s")).With(nil) },
		"zero route": func() { (rh.Route{}).With() },
		"timeout 0":  func() { rh.Timeout(0) },
	}
	for name, f := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("no panic")
				}
			}()
			f()
		})
	}
}
