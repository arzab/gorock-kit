// Package rockhandlers is a transport-independent shape for handling a
// request: a chain of steps with Next, typed Params and Result, Locals,
// middlewares, panic recovery and error kinds. Transports (HTTP, queues,
// CLI…) fill Params and turn the Result or the error into their protocol.
//
//	var CreateOrder = rockhandlers.Chain("orders.create", auth.Bearer, orders.Create)
//
// See README.md.
package rockhandlers

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"time"
)

// Handler is one step of a chain. P and R are pointer-to-struct types:
// Handler[*CreateParams, *CreateResult].
type Handler[P, R any] func(c *Ctx[P, R]) error

// Context is what a Middleware sees: the request context, the route name
// and Next. It does not expose Params or Result — steps that need request
// data are typed Handlers (see the README: constraints instead of casts).
//
// Context is implemented only by *Ctx.
type Context interface {
	context.Context

	// Route returns the route name.
	Route() string
	// Next runs the rest of the chain and returns its error.
	Next() error
	// Base returns the current underlying context.Context.
	Base() context.Context
	// SetContext replaces the context for the rest of the request (for
	// example to add a trace span or a deadline).
	SetContext(ctx context.Context)

	locals() map[any]any
	setLocal(k, v any)
}

// Middleware is cross-cutting logic that does not look at request data:
// logging, metrics, timeouts, tracing, error handling.
//
//	func Log(c rockhandlers.Context) error {
//	    err := c.Next()
//	    if err != nil { /* log, metric, re-classify */ }
//	    return err
//	}
type Middleware func(c Context) error

// Done ends the chain successfully. A step or middleware returns it to skip
// the rest of the chain — for example on a cache hit:
//
//	if cached, ok := cache.Get(c.Params.ID); ok {
//	    *c.Result = cached
//	    return rockhandlers.Done
//	}
//	return nil // the chain continues
//
// Done never leaves the chain: outer links get nil from their Next, and
// Serve and Run return the Result with a nil error.
var Done = errors.New("rockhandlers: done") //nolint:staticcheck // a control-flow sentinel like io.EOF, not a failure: the name Done reads right at the call site

// Validator is implemented by Params that validate themselves. Validate runs
// after the middlewares and the transport's fill, before the first step; a
// plain error becomes Invalid.
type Validator interface{ Validate() error }

// Ctx is the state of one request. It is a context.Context: pass it to the
// database, clients and channels so they stop when the request is cancelled.
//
// A Ctx is created per request and never reused; it is not safe for
// concurrent use.
type Ctx[P, R any] struct {
	Params P // filled by the transport; never nil
	Result R // allocated before the chain; steps fill it; never nil

	ctx   context.Context
	route string
	lv    map[any]any

	mws   []Middleware
	fill  func(params any) error // the transport's decoding; may be nil
	steps []Handler[P, R]
	cur   int     // index of the running link; -1 before the chain starts
	ran   []bool  // per link: Next already started it
	errs  []error // per link: its result
	done  bool    // a link returned Done: nothing more runs
}

// Context methods.

func (c *Ctx[P, R]) Deadline() (time.Time, bool) { return c.ctx.Deadline() }
func (c *Ctx[P, R]) Done() <-chan struct{}       { return c.ctx.Done() }
func (c *Ctx[P, R]) Err() error                  { return c.ctx.Err() }
func (c *Ctx[P, R]) Value(key any) any           { return c.ctx.Value(key) }

func (c *Ctx[P, R]) Route() string                  { return c.route }
func (c *Ctx[P, R]) Base() context.Context          { return c.ctx }
func (c *Ctx[P, R]) SetContext(ctx context.Context) { c.ctx = ctx }

func (c *Ctx[P, R]) locals() map[any]any { return c.lv }

func (c *Ctx[P, R]) setLocal(k, v any) {
	if c.lv == nil {
		c.lv = map[any]any{}
	}
	c.lv[k] = v
}

// links: middlewares, then fill + validation, then steps.
func (c *Ctx[P, R]) links() int { return len(c.mws) + 1 + len(c.steps) }

// Next runs the next link of the chain and returns its error.
//
// A link does not have to call Next: if it returns nil without calling it,
// the chain continues by itself. Call Next to run code after the rest of the
// chain (a middleware that logs the error), and return Done to end the chain
// early with success.
//
//   - After the last step it does nothing and returns nil.
//   - Called again by the same link, it does not rerun anything and returns
//     the first result.
//   - If the context is already done, it does not run the next link and
//     returns the context error (KindOf: Canceled / Timeout).
//   - A panic in the next link becomes an Internal error with *PanicError.
func (c *Ctx[P, R]) Next() error {
	i := c.cur + 1
	if c.ran[i] {
		return c.errs[i]
	}
	c.ran[i] = true
	if i == c.links() {
		return nil
	}
	if err := c.ctx.Err(); err != nil {
		c.errs[i] = err
		return err
	}
	prev := c.cur
	c.cur = i
	err := c.run(i)
	if err == nil && !c.done && !c.ran[i+1] {
		err = c.Next() // the link returned nil without calling Next: continue
	}
	c.cur = prev
	if errors.Is(err, Done) {
		c.done = true
		err = nil
	}
	c.errs[i] = err
	return err
}

func (c *Ctx[P, R]) run(i int) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = &Error{Kind: KindInternal, Err: &PanicError{Value: v, Stack: debug.Stack()}}
		}
	}()
	switch n := len(c.mws); {
	case i < n:
		return c.mws[i](c)
	case i == n:
		return c.prepare()
	default:
		return c.steps[i-n-1](c)
	}
}

// prepare fills Params (the transport's decoding) and validates them.
func (c *Ctx[P, R]) prepare() error {
	if c.fill != nil {
		if err := c.fill(c.Params); err != nil {
			var e *Error
			if errors.As(err, &e) {
				return err
			}
			// A decoder's text names Go types and fields: keep it for logs only.
			return &Error{Kind: KindInvalid, Message: "invalid request", Err: err}
		}
	}
	if v, ok := any(c.Params).(Validator); ok {
		if err := v.Validate(); err != nil {
			var e *Error
			if errors.As(err, &e) {
				return err
			}
			return &Error{Kind: KindInvalid, Message: err.Error(), Err: err}
		}
	}
	return c.Next()
}

// Timeout bounds the rest of the chain with a context deadline. It is
// cooperative: steps must pass the Ctx to blocking calls; Next refuses to
// start further steps once the deadline has passed.
func Timeout(d time.Duration) Middleware {
	if d <= 0 {
		panic(fmt.Sprintf("rockhandlers: Timeout(%v): duration must be positive", d))
	}
	return func(c Context) error {
		base := c.Base()
		ctx, cancel := context.WithTimeout(base, d)
		defer cancel()
		c.SetContext(ctx)
		defer c.SetContext(base)
		return c.Next()
	}
}
