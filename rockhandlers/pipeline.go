package rockhandlers

import (
	"context"
	"errors"
	"fmt"
	"reflect"
)

// Pipeline is a chain ready to run: what an App stores and calls. It hides
// the concrete Params and Response types, so chains of different types fit
// in one list.
type Pipeline struct {
	newParams   func() Params
	newResponse func() Response
	handlers    func(p Params, r Response) []func(*Ctx) error
	middlewares []Middleware
}

// Chain builds a pipeline from handlers. P and R are pointers to structs,
// usually inferred from the handlers; a handler of another type does not
// compile. A chain may have no handlers yet: Run then returns
// ErrHandlerNotFound.
func Chain[P Params, R Response](handlers ...Handler[P, R]) *Pipeline {
	pt, rt := reflect.TypeFor[P](), reflect.TypeFor[R]()
	if pt.Kind() != reflect.Pointer || rt.Kind() != reflect.Pointer {
		panic(fmt.Sprintf("rockhandlers: Chain[%s, %s]: params and response must be pointers to structs", pt, rt))
	}
	hs := append([]Handler[P, R](nil), handlers...)
	return &Pipeline{
		newParams:   func() Params { return reflect.New(pt.Elem()).Interface().(P) },
		newResponse: func() Response { return reflect.New(rt.Elem()).Interface().(R) },
		handlers: func(p Params, r Response) []func(*Ctx) error {
			params, response := p.(P), r.(R)
			links := make([]func(*Ctx) error, len(hs))
			for i, h := range hs {
				links[i] = func(c *Ctx) error { return h(c, params, response) }
			}
			return links
		},
	}
}

// Use returns a copy of the pipeline with middlewares in front of it. The
// first one runs first, and each Use goes in front of the previous ones, so
// an App adds its middlewares with Use at registration.
func (p *Pipeline) Use(mws ...Middleware) *Pipeline {
	cp := *p
	cp.middlewares = append(append([]Middleware(nil), mws...), p.middlewares...)
	return &cp
}

// Run runs one request. fill is the App's decoding: it gets a new, empty
// Params and fills it from the request. Then, in this order:
//
//  1. middlewares — they see everything below, including decoding errors;
//  2. fill — a plain error becomes Invalid "invalid request" (the cause is
//     kept for logs); return an *Error for a precise message;
//  3. Params.Validate — a plain error becomes Invalid with its text;
//  4. handlers;
//  5. Response.Normalize — its error comes back as *NormalizeError.
//
// Run never panics: a panic anywhere becomes an Internal error. On error the
// response is nil.
func (p *Pipeline) Run(ctx context.Context, fill func(params Params) error) (Response, error) {
	return p.run(ctx, p.newParams(), fill)
}

// Call runs the pipeline with ready params — for tests and for calling a
// chain from code. params must be the chain's Params type.
func (p *Pipeline) Call(ctx context.Context, params Params) (Response, error) {
	if params == nil || reflect.TypeOf(params) != reflect.TypeOf(p.newParams()) {
		return nil, fmt.Errorf("rockhandlers: Call with %T, want %T", params, p.newParams())
	}
	return p.run(ctx, params, nil)
}

func (p *Pipeline) run(ctx context.Context, params Params, fill func(Params) error) (Response, error) {
	response := p.newResponse()
	c := &Ctx{ctx: ctx}
	for _, mw := range p.middlewares {
		c.links = append(c.links, func(c *Ctx) error { return mw(c, params, response) })
	}
	c.links = append(c.links, func(c *Ctx) error {
		if fill != nil {
			if err := fill(params); err != nil {
				return asError(err, "invalid request")
			}
		}
		if err := params.Validate(); err != nil {
			return asError(err, err.Error())
		}
		return c.Next()
	})
	c.links = append(c.links, p.handlers(params, response)...)

	if err := c.Next(); err != nil {
		return nil, err
	}
	if err := normalize(response); err != nil {
		return nil, &NormalizeError{Err: err}
	}
	return response, nil
}

// asError keeps an *Error as is and turns a plain error into Invalid.
func asError(err error, msg string) error {
	var e *Error
	if errors.As(err, &e) {
		return err
	}
	return &Error{Kind: KindInvalid, Message: msg, Err: err}
}

func normalize(r Response) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("panic: %v", v)
		}
	}()
	return r.Normalize()
}
