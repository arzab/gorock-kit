// Package rockdelivery is the contract between Realm (internal/delivery) and
// the Apps that serve it: business logic is a typed function that knows
// nothing about the transport, and any App — HTTP, queue consumer, scheduler,
// CLI — can serve the same function.
//
//	func Create(ctx context.Context, p *CreateParams) (*CreateResult, error)
//
// An App decodes its input into the params, calls the function through an
// Action, and translates the result and the error Kind into its protocol.
package rockdelivery

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime/debug"
	"time"
)

// Handler is a piece of business logic: typed input, typed output, an error.
// In and Out are structs; a handler without input uses struct{}.
type Handler[In, Out any] func(ctx context.Context, in *In) (*Out, error)

// Validator is implemented by input types that check themselves. Action.Call
// runs Validate before the handler; a plain error from it becomes Invalid.
type Validator interface {
	Validate() error
}

// Next is the type-erased form of a handler that middlewares wrap.
type Next func(ctx context.Context, in any) (any, error)

// Middleware wraps a call with transport-independent behaviour (auth from
// ctx, logging, metrics, timeouts). The first middleware passed to Bind is
// the outermost.
type Middleware func(next Next) Next

// ErrPanic is wrapped by the Internal error Call returns when a handler or a
// middleware panics.
var ErrPanic = errors.New("rockdelivery: panic")

// Action is a bound handler in the form Apps work with, without generics.
type Action struct {
	newInput func() any
	call     Next
	in, out  reflect.Type
}

// Bind turns a handler into an Action. Middlewares run in the given order
// around validation and the handler.
func Bind[In, Out any](h Handler[In, Out], mws ...Middleware) Action {
	if h == nil {
		panic("rockdelivery: Bind with a nil handler")
	}
	var next Next = func(ctx context.Context, in any) (any, error) {
		p, ok := in.(*In)
		if !ok || p == nil {
			var want *In
			return nil, fmt.Errorf("rockdelivery: input is %T, want %T", in, want)
		}
		if err := validate(p); err != nil {
			return nil, err
		}
		out, err := h(ctx, p)
		if err != nil {
			return nil, err
		}
		return out, nil
	}
	for i := len(mws) - 1; i >= 0; i-- {
		next = mws[i](next)
	}
	return Action{
		newInput: func() any { return new(In) },
		call:     next,
		in:       reflect.TypeFor[In](),
		out:      reflect.TypeFor[Out](),
	}
}

// NewInput returns a fresh *In for the App to decode the request into.
func (a Action) NewInput() any { return a.newInput() }

// Call runs middlewares, validation and the handler. The result is the
// handler's *Out (possibly nil). A panic anywhere in the chain is returned as
// an Internal error wrapping ErrPanic, so an App never crashes on one.
func (a Action) Call(ctx context.Context, in any) (out any, err error) {
	defer func() {
		if r := recover(); r != nil {
			out, err = nil, fmt.Errorf("%w: %v\n%s", ErrPanic, r, debug.Stack())
		}
	}()
	return a.call(ctx, in)
}

// InputType and OutputType describe the handler, e.g. for generated docs.
func (a Action) InputType() reflect.Type  { return a.in }
func (a Action) OutputType() reflect.Type { return a.out }

func validate(in any) error {
	v, ok := in.(Validator)
	if !ok {
		return nil
	}
	err := v.Validate()
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		return err
	}
	return &Error{Kind: KindInvalid, Message: err.Error()}
}

// Timeout bounds the call with a context deadline. Handlers that pass ctx to
// services stop waiting when it expires.
func Timeout(d time.Duration) Middleware {
	return func(next Next) Next {
		return func(ctx context.Context, in any) (any, error) {
			ctx, cancel := context.WithTimeout(ctx, d)
			defer cancel()
			return next(ctx, in)
		}
	}
}
