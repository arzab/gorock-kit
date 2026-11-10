// Package rockhandlers is the handlers layer of a GOROCK project: business
// logic as a chain of handlers, like Fiber, but independent of the transport.
//
//	type CreateParams struct{ Title string `json:"title"` }
//	func (p *CreateParams) Validate() error { ... }
//
//	type CreateResponse struct{ Task *models.Task `json:"task"` }
//	func (r *CreateResponse) Normalize() error { return nil }
//
//	func create(c *rockhandlers.Ctx, params *CreateParams, response *CreateResponse) error { ... }
//
//	var Create = rockhandlers.Chain(checkAuth, create)
//
// An App (HTTP, queue, cron…) runs it: response, err := Create.Run(ctx, fill).
package rockhandlers

import (
	"context"
	"errors"
	"runtime/debug"
	"time"
)

// Params is what a handler gets: filled by the App, checked by Validate
// before any handler runs.
type Params interface{ Validate() error }

// Response is what a handler fills. Normalize runs after the chain succeeded.
type Response interface{ Normalize() error }

// Handler is one step of a chain, like a Fiber handler: it gets the request
// context, the params and the response.
//
//   - return nil: the chain is done, without errors;
//   - return c.Next(): run the next handler and return its result;
//   - return an error: the chain stops, the App gets the error.
type Handler[P Params, R Response] func(c *Ctx, params P, response R) error

// Middleware is a handler for any chain: the same signature with the
// general Params and Response. Logging, metrics, timeouts.
//
//	func Log(c *rockhandlers.Ctx, _ rockhandlers.Params, _ rockhandlers.Response) error {
//	    err := c.Next()
//	    if err != nil { /* log */ }
//	    return err
//	}
type Middleware = Handler[Params, Response]

// ErrHandlerNotFound is returned by Next when there is no next handler: the
// last handler called Next, or the chain has no handlers yet.
var ErrHandlerNotFound = errors.New("rockhandlers: handler not found")

// NormalizeError wraps an error returned by Response.Normalize, so the App
// can tell it apart with errors.As.
type NormalizeError struct{ Err error }

func (e *NormalizeError) Error() string { return "rockhandlers: normalize: " + e.Err.Error() }
func (e *NormalizeError) Unwrap() error { return e.Err }

// Ctx is the state of one request. It is a context.Context: pass it to the
// database, clients and channels so they stop when the request is cancelled.
// A Ctx is created per request; it is not safe for concurrent use.
type Ctx struct {
	ctx    context.Context
	links  []func(c *Ctx) error
	next   int
	locals map[any]any
}

func (c *Ctx) Deadline() (time.Time, bool) { return c.ctx.Deadline() }
func (c *Ctx) Done() <-chan struct{}       { return c.ctx.Done() }
func (c *Ctx) Err() error                  { return c.ctx.Err() }
func (c *Ctx) Value(key any) any           { return c.ctx.Value(key) }

// Context returns the current underlying context.
func (c *Ctx) Context() context.Context { return c.ctx }

// SetContext replaces the context for the rest of the request — for a
// deadline or a tracing span. Derive the new one from c.Context().
func (c *Ctx) SetContext(ctx context.Context) { c.ctx = ctx }

// Next runs the next handler and returns its result. It returns
// ErrHandlerNotFound if there is none, and the context error if the request
// is already cancelled. A panic in the handler becomes an Internal error.
func (c *Ctx) Next() error {
	if c.next >= len(c.links) {
		return ErrHandlerNotFound
	}
	if err := c.ctx.Err(); err != nil {
		return err
	}
	link := c.links[c.next]
	c.next++
	return run(link, c)
}

// Set stores a value for the next handlers of this request.
func (c *Ctx) Set(key, value any) {
	if c.locals == nil {
		c.locals = map[any]any{}
	}
	c.locals[key] = value
}

// Get returns a value stored with Set.
func (c *Ctx) Get(key any) (any, bool) {
	v, ok := c.locals[key]
	return v, ok
}

func run(link func(*Ctx) error, c *Ctx) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = &Error{Kind: KindInternal, Err: &PanicError{Value: v, Stack: debug.Stack()}}
		}
	}()
	return link(c)
}

// Timeout bounds the rest of the chain with a deadline. Handlers must pass
// the Ctx to blocking calls for it to take effect.
func Timeout(d time.Duration) Middleware {
	return func(c *Ctx, _ Params, _ Response) error {
		parent := c.Context()
		ctx, cancel := context.WithTimeout(parent, d)
		defer cancel()
		c.SetContext(ctx)
		defer c.SetContext(parent)
		return c.Next()
	}
}
