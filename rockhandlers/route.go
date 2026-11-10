package rockhandlers

import (
	"context"
	"fmt"
	"reflect"
)

// Route is a named chain with its generics erased, so routes of different
// types fit in one slice or struct field. Build it with Chain.
type Route struct {
	name   string
	params reflect.Type
	result reflect.Type
	mws    []Middleware // outermost first
	serve  func(ctx context.Context, name string, mws []Middleware, params any, fill func(any) error) (any, error)
}

// Chain builds a route from steps. P and R must be pointer-to-struct types;
// they are usually inferred from a concrete step, and generic steps are
// instantiated to match:
//
//	rockhandlers.Chain("orders.create", auth.Bearer, orders.Create)
//
// Chain panics on an empty name, no steps, a nil step or non-pointer types —
// wiring bugs surface at startup.
func Chain[P, R any](name string, steps ...Handler[P, R]) Route {
	pt, rt := reflect.TypeFor[P](), reflect.TypeFor[R]()
	switch {
	case name == "":
		panic("rockhandlers: Chain: empty route name")
	case len(steps) == 0:
		panic(fmt.Sprintf("rockhandlers: Chain %q: no steps", name))
	case !isStructPtr(pt):
		panic(fmt.Sprintf("rockhandlers: Chain %q: params type %s must be a pointer to a struct", name, pt))
	case !isStructPtr(rt):
		panic(fmt.Sprintf("rockhandlers: Chain %q: result type %s must be a pointer to a struct", name, rt))
	}
	for i, s := range steps {
		if s == nil {
			panic(fmt.Sprintf("rockhandlers: Chain %q: step %d is nil", name, i))
		}
	}
	steps = append([]Handler[P, R](nil), steps...)

	serve := func(ctx context.Context, name string, mws []Middleware, params any, fill func(any) error) (any, error) {
		if params == nil {
			params = reflect.New(pt.Elem()).Interface()
		}
		p, ok := params.(P)
		if !ok || reflect.ValueOf(p).IsNil() {
			return nil, &Error{Kind: KindInternal, Err: fmt.Errorf("rockhandlers: route %q: params must be a non-nil %s, got %T", name, pt, params)}
		}
		c := &Ctx[P, R]{
			Params: p,
			Result: reflect.New(rt.Elem()).Interface().(R),
			ctx:    ctx,
			route:  name,
			mws:    mws,
			fill:   fill,
			steps:  steps,
			cur:    -1,
		}
		n := c.links() + 1
		c.ran, c.errs = make([]bool, n), make([]error, n)
		if err := c.Next(); err != nil {
			return nil, err
		}
		return c.Result, nil
	}
	return Route{name: name, params: pt, result: rt, serve: serve}
}

func isStructPtr(t reflect.Type) bool {
	return t.Kind() == reflect.Pointer && t.Elem().Kind() == reflect.Struct
}

// Prepend returns a new slice: pre, then steps. For groups:
//
//	func Admin[P auth.Params, R any](name string, steps ...rockhandlers.Handler[P, R]) rockhandlers.Route {
//	    return rockhandlers.Chain(name, rockhandlers.Prepend(steps, auth.Bearer[P, R])...)
//	}
func Prepend[P, R any](steps []Handler[P, R], pre ...Handler[P, R]) []Handler[P, R] {
	out := make([]Handler[P, R], 0, len(pre)+len(steps))
	return append(append(out, pre...), steps...)
}

// Name returns the route name.
func (r Route) Name() string { return r.name }

// ParamsType returns P.
func (r Route) ParamsType() reflect.Type { return r.params }

// ResultType returns R.
func (r Route) ResultType() reflect.Type { return r.result }

// With returns a copy of r with extra middlewares around it. The first one
// is the outermost, and each With call wraps everything added before:
// route.With(A).With(B) runs B → A → steps.
func (r Route) With(mws ...Middleware) Route {
	r.mustBuilt()
	for i, mw := range mws {
		if mw == nil {
			panic(fmt.Sprintf("rockhandlers: route %q: With: middleware %d is nil", r.name, i))
		}
	}
	all := make([]Middleware, 0, len(mws)+len(r.mws))
	r.mws = append(append(all, mws...), r.mws...)
	return r
}

// Serve runs the chain for one request. fill is the transport's decoding:
// it gets a new P (a pointer to a zero struct) and fills it from the request.
// It runs inside the chain — after the middlewares, before validation — so
// middlewares see decoding errors and timeouts cover decoding. A plain error
// from fill becomes Invalid with the generic message "invalid request" (the
// error itself is kept as the cause, for logs) — a decoder's text names Go
// types and fields and must not reach the client. Return an *Error to set the
// kind and a safe, precise message yourself. fill may be nil.
//
// Serve never panics. On error the result is nil — a partly filled Result
// does not leak; classify the error with KindOf. On success the result is
// the R.
func (r Route) Serve(ctx context.Context, fill func(params any) error) (any, error) {
	if r.serve == nil {
		return nil, &Error{Kind: KindInternal, Err: fmt.Errorf("rockhandlers: Serve on a Route not built by Chain")}
	}
	return r.serve(ctx, r.name, r.mws, nil, fill)
}

func (r Route) mustBuilt() {
	if r.serve == nil {
		panic("rockhandlers: Route not built by Chain")
	}
}

// Run serves the route with ready params — for tests and for calling a route
// from code. P is inferred from params:
//
//	res, err := rockhandlers.Run[*CreateResult](ctx, CreateOrder, &CreateParams{Qty: 1})
func Run[R, P any](ctx context.Context, r Route, params P) (R, error) {
	var zero R
	if r.serve == nil {
		return zero, fmt.Errorf("rockhandlers: Run on a Route not built by Chain")
	}
	if pt, rt := reflect.TypeFor[P](), reflect.TypeFor[R](); pt != r.params || rt != r.result {
		return zero, fmt.Errorf("rockhandlers: Run[%s, %s] on route %q of [%s, %s]", pt, rt, r.name, r.params, r.result)
	}
	if reflect.ValueOf(params).IsNil() {
		return zero, fmt.Errorf("rockhandlers: Run on route %q: nil params", r.name)
	}
	res, err := r.serve(ctx, r.name, r.mws, params, nil)
	if err != nil {
		return zero, err
	}
	return res.(R), nil
}
