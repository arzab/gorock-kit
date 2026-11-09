package rockdelivery

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type createParams struct {
	Item string
	Qty  int
}

func (p *createParams) Validate() error {
	if p.Item == "" {
		return errors.New("item is required") // plain error → Invalid
	}
	if p.Qty <= 0 {
		return Invalid("qty must be > 0, got %d", p.Qty)
	}
	return nil
}

type createResult struct{ ID string }

func create(_ context.Context, p *createParams) (*createResult, error) {
	switch p.Item {
	case "missing":
		return nil, NotFound("item %q not found", p.Item)
	case "db":
		return nil, fmt.Errorf("insert: %w", errors.New("connection reset"))
	case "boom":
		panic("handler exploded")
	}
	return &createResult{ID: "order-" + p.Item}, nil
}

func TestCallSuccessAndTypes(t *testing.T) {
	a := Bind(create)
	in := a.NewInput().(*createParams)
	in.Item, in.Qty = "book", 2
	out, err := a.Call(context.Background(), in)
	if err != nil || out.(*createResult).ID != "order-book" {
		t.Fatalf("out=%v err=%v", out, err)
	}
	if a.InputType().Name() != "createParams" || a.OutputType().Name() != "createResult" {
		t.Fatalf("types: %v %v", a.InputType(), a.OutputType())
	}
}

func TestValidation(t *testing.T) {
	a := Bind(create)
	for _, tc := range []struct {
		in   createParams
		msg  string
		kind Kind
	}{
		{createParams{Qty: 1}, "item is required", KindInvalid},
		{createParams{Item: "x", Qty: 0}, "qty must be > 0, got 0", KindInvalid},
	} {
		in := tc.in
		_, err := a.Call(context.Background(), &in)
		if KindOf(err) != tc.kind || MessageOf(err) != tc.msg {
			t.Fatalf("in=%+v: kind=%v msg=%q", tc.in, KindOf(err), MessageOf(err))
		}
	}
}

func TestErrorKinds(t *testing.T) {
	a := Bind(create)
	_, err := a.Call(context.Background(), &createParams{Item: "missing", Qty: 1})
	if KindOf(err) != KindNotFound || MessageOf(err) != `item "missing" not found` {
		t.Fatalf("not found: kind=%v msg=%q", KindOf(err), MessageOf(err))
	}

	_, err = a.Call(context.Background(), &createParams{Item: "db", Qty: 1})
	if KindOf(err) != KindInternal || MessageOf(err) != "" || !strings.Contains(err.Error(), "connection reset") {
		t.Fatalf("internal: kind=%v msg=%q err=%v", KindOf(err), MessageOf(err), err)
	}

	// A kind survives wrapping; the cause is kept for logs.
	cause := errors.New("timeout talking to bank")
	wrapped := fmt.Errorf("charge: %w", Unavailable("payments are down").Wrap(cause))
	if KindOf(wrapped) != KindUnavailable || MessageOf(wrapped) != "payments are down" || !errors.Is(wrapped, cause) {
		t.Fatalf("wrapped: kind=%v msg=%q", KindOf(wrapped), MessageOf(wrapped))
	}
}

func TestPanicBecomesInternal(t *testing.T) {
	a := Bind(create)
	out, err := a.Call(context.Background(), &createParams{Item: "boom", Qty: 1})
	if out != nil || !errors.Is(err, ErrPanic) || KindOf(err) != KindInternal || !strings.Contains(err.Error(), "handler exploded") {
		t.Fatalf("out=%v err=%v", out, err)
	}
}

func TestWrongInputType(t *testing.T) {
	a := Bind(create)
	if _, err := a.Call(context.Background(), &createResult{}); err == nil || !strings.Contains(err.Error(), "want *rockdelivery.createParams") {
		t.Fatalf("err = %v", err)
	}
	if _, err := a.Call(context.Background(), nil); err == nil {
		t.Fatal("nil input accepted")
	}
}

func TestMiddlewareOrder(t *testing.T) {
	var trace []string
	mw := func(name string) Middleware {
		return func(next Next) Next {
			return func(ctx context.Context, in any) (any, error) {
				trace = append(trace, name+">")
				out, err := next(ctx, in)
				trace = append(trace, "<"+name)
				return out, err
			}
		}
	}
	a := Bind(create, mw("auth"), mw("log"))
	if _, err := a.Call(context.Background(), &createParams{Item: "x", Qty: 1}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(trace, " "); got != "auth> log> <log <auth" {
		t.Fatalf("order = %s", got)
	}
}

// A middleware can reject before validation and the handler run.
func TestMiddlewareShortCircuit(t *testing.T) {
	called := false
	deny := func(Next) Next {
		return func(context.Context, any) (any, error) { return nil, Unauthorized("token required") }
	}
	a := Bind(func(context.Context, *struct{}) (*struct{}, error) { called = true; return nil, nil }, deny)
	_, err := a.Call(context.Background(), a.NewInput())
	if called || KindOf(err) != KindUnauthorized {
		t.Fatalf("called=%v err=%v", called, err)
	}
}

func TestTimeout(t *testing.T) {
	a := Bind(func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}, Timeout(20*time.Millisecond))
	start := time.Now()
	_, err := a.Call(context.Background(), a.NewInput())
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Fatalf("err=%v after %s", err, time.Since(start))
	}
}

func TestKindString(t *testing.T) {
	if KindNotFound.String() != "not_found" || Kind(42).String() != "kind(42)" {
		t.Fatal(KindNotFound.String(), Kind(42).String())
	}
	if (&Error{Message: "m", Err: errors.New("c")}).Error() != "m: c" {
		t.Fatal("Error() format")
	}
}

func TestBindNil(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	Bind[struct{}, struct{}](nil)
}
