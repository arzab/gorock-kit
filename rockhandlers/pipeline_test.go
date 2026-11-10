package rockhandlers_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	rh "github.com/arzab/gorock-kit/rockhandlers"
)

type P struct {
	N        int
	Bad      bool
	validate func() error
}

func (p *P) Validate() error {
	if p.validate != nil {
		return p.validate()
	}
	if p.Bad {
		return errors.New("n must be > 0")
	}
	return nil
}

type R struct {
	Log       []string
	normalize func() error
}

func (r *R) Normalize() error {
	if r.normalize != nil {
		return r.normalize()
	}
	return nil
}

// step appends its name and continues the chain.
func step(name string) rh.Handler[*P, *R] {
	return func(c *rh.Ctx, _ *P, r *R) error {
		r.Log = append(r.Log, name)
		return c.Next()
	}
}

// last appends its name and ends the chain.
func last(name string) rh.Handler[*P, *R] {
	return func(_ *rh.Ctx, _ *P, r *R) error {
		r.Log = append(r.Log, name)
		return nil
	}
}

func call(t *testing.T, pl *rh.Pipeline, p *P) (*R, error) {
	t.Helper()
	res, err := pl.Call(context.Background(), p)
	if err != nil {
		return nil, err
	}
	return res.(*R), nil
}

func TestChainLikeFiber(t *testing.T) {
	res, err := call(t, rh.Chain(step("a"), step("b"), last("c")), &P{})
	if err != nil || strings.Join(res.Log, " ") != "a b c" {
		t.Fatalf("res %+v, err %v", res, err)
	}

	// return nil without Next ends the chain
	res, err = call(t, rh.Chain(step("a"), last("b"), last("never")), &P{})
	if err != nil || strings.Join(res.Log, " ") != "a b" {
		t.Fatalf("early end: res %+v, err %v", res, err)
	}
}

func TestHandlerNotFound(t *testing.T) {
	// the last handler calls Next
	if _, err := call(t, rh.Chain(step("a")), &P{}); !errors.Is(err, rh.ErrHandlerNotFound) {
		t.Fatalf("err = %v", err)
	}
	// a skeleton chain without handlers
	if _, err := call(t, rh.Chain[*P, *R](), &P{}); !errors.Is(err, rh.ErrHandlerNotFound) {
		t.Fatalf("empty chain: err = %v", err)
	}
}

func TestErrorStopsTheChain(t *testing.T) {
	failing := func(*rh.Ctx, *P, *R) error { return rh.Conflict("duplicate") }
	res, err := call(t, rh.Chain(step("a"), failing, last("never")), &P{})
	if res != nil || rh.KindOf(err) != rh.KindConflict {
		t.Fatalf("res %v, err %v", res, err)
	}
}

func TestValidate(t *testing.T) {
	ran := false
	pl := rh.Chain(func(*rh.Ctx, *P, *R) error { ran = true; return nil })

	_, err := call(t, pl, &P{Bad: true})
	if rh.KindOf(err) != rh.KindInvalid || rh.MessageOf(err) != "n must be > 0" || ran {
		t.Fatalf("plain error: err %v, handler ran %v", err, ran)
	}

	fields := rh.Invalid("invalid").WithFields(map[string]string{"n": "required"})
	_, err = call(t, pl, &P{validate: func() error { return fields }})
	if rh.FieldsOf(err)["n"] != "required" {
		t.Fatalf("*Error: %v", err)
	}
}

func TestFill(t *testing.T) {
	pl := rh.Chain(func(_ *rh.Ctx, p *P, r *R) error { r.Log = append(r.Log, "n", string(rune('0'+p.N))); return nil })

	res, err := pl.Run(context.Background(), func(p rh.Params) error { p.(*P).N = 7; return nil })
	if err != nil || strings.Join(res.(*R).Log, "") != "n7" {
		t.Fatalf("res %v, err %v", res, err)
	}

	decodeErr := errors.New("json: cannot unmarshal string into Go struct field P.N of type int")
	_, err = pl.Run(context.Background(), func(rh.Params) error { return decodeErr })
	if rh.KindOf(err) != rh.KindInvalid || rh.MessageOf(err) != "invalid request" || !errors.Is(err, decodeErr) {
		t.Fatalf("plain fill error: %v", err)
	}

	_, err = pl.Run(context.Background(), func(rh.Params) error { return rh.Unauthorized("bad signature") })
	if rh.KindOf(err) != rh.KindUnauthorized {
		t.Fatalf("*Error from fill: %v", err)
	}
}

func TestMiddlewares(t *testing.T) {
	var log []string
	mw := func(name string) rh.Middleware {
		return func(c *rh.Ctx, _ rh.Params, _ rh.Response) error {
			log = append(log, name+">")
			err := c.Next()
			log = append(log, "<"+name+":"+rh.KindOf(err).String())
			return err
		}
	}
	pl := rh.Chain(last("h")).Use(mw("route")).Use(mw("app"))
	if _, err := call(t, pl, &P{}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(log, " "); got != "app> route> <route:none <app:none" {
		t.Fatalf("order: %s", got)
	}

	// middlewares see decoding and validation errors too
	log = nil
	_, _ = pl.Run(context.Background(), func(rh.Params) error { return errors.New("bad json") })
	if got := strings.Join(log, " "); got != "app> route> <route:invalid <app:invalid" {
		t.Fatalf("decoding error: %s", got)
	}
}

func TestPanics(t *testing.T) {
	boom := func(*rh.Ctx, *P, *R) error { panic("boom") }
	_, err := call(t, rh.Chain(boom), &P{})
	var pe *rh.PanicError
	if rh.KindOf(err) != rh.KindInternal || !errors.As(err, &pe) || pe.Value != "boom" {
		t.Fatalf("handler panic: %v", err)
	}
	if rh.MessageOf(err) != "" {
		t.Fatal("Internal message leaked")
	}

	mw := func(*rh.Ctx, rh.Params, rh.Response) error { panic("mw") }
	if _, err := call(t, rh.Chain(last("h")).Use(mw), &P{}); rh.KindOf(err) != rh.KindInternal {
		t.Fatalf("middleware panic: %v", err)
	}
}

func TestNormalize(t *testing.T) {
	normalized := false
	ok := func(_ *rh.Ctx, _ *P, r *R) error {
		r.normalize = func() error { normalized = true; return nil }
		return nil
	}
	if _, err := call(t, rh.Chain(ok), &P{}); err != nil || !normalized {
		t.Fatalf("err %v, normalized %v", err, normalized)
	}

	bad := func(_ *rh.Ctx, _ *P, r *R) error {
		r.normalize = func() error { return errors.New("empty task") }
		return nil
	}
	res, err := call(t, rh.Chain(bad), &P{})
	var ne *rh.NormalizeError
	if res != nil || !errors.As(err, &ne) || ne.Err.Error() != "empty task" {
		t.Fatalf("res %v, err %v", res, err)
	}

	// not called when the chain failed
	normalized = false
	failing := func(_ *rh.Ctx, _ *P, r *R) error {
		r.normalize = func() error { normalized = true; return nil }
		return rh.NotFound("no")
	}
	if _, err := call(t, rh.Chain(failing), &P{}); rh.KindOf(err) != rh.KindNotFound || normalized {
		t.Fatalf("err %v, normalized %v", err, normalized)
	}
}

func TestCancellationAndTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := rh.Chain(last("h")).Call(ctx, &P{}); rh.KindOf(err) != rh.KindCanceled {
		t.Fatalf("canceled: %v", err)
	}

	slow := func(c *rh.Ctx, _ *P, _ *R) error {
		select {
		case <-c.Done():
			return c.Err()
		case <-time.After(5 * time.Second):
			return nil
		}
	}
	start := time.Now()
	_, err := call(t, rh.Chain(slow).Use(rh.Timeout(20*time.Millisecond)), &P{})
	if rh.KindOf(err) != rh.KindTimeout || time.Since(start) > 2*time.Second {
		t.Fatalf("timeout: %v after %v", err, time.Since(start))
	}
}

func TestLocals(t *testing.T) {
	type userKey struct{}
	auth := func(c *rh.Ctx, _ *P, _ *R) error { c.Set(userKey{}, "ann"); return c.Next() }
	use := func(c *rh.Ctx, _ *P, r *R) error {
		user, _ := c.Get(userKey{})
		r.Log = append(r.Log, user.(string))
		return nil
	}
	res, err := call(t, rh.Chain(auth, use), &P{})
	if err != nil || res.Log[0] != "ann" {
		t.Fatalf("res %+v, err %v", res, err)
	}
}

func TestCallChecksType(t *testing.T) {
	type other struct{ P }
	if _, err := rh.Chain(last("h")).Call(context.Background(), &other{}); err == nil {
		t.Fatal("Call with another params type: no error")
	}
	if _, err := rh.Chain(last("h")).Call(context.Background(), nil); err == nil {
		t.Fatal("Call with nil: no error")
	}
}
