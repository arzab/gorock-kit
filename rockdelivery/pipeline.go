package rockdelivery

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"time"
)

// Codec is the only technology-specific part of delivery: how to read the
// input of a request, how to write a result and how to write an error. Req is
// the transport's request (an *http.Request, a queue message, CLI args), Resp
// is where the answer goes (an http.ResponseWriter, an acker, an exit code).
type Codec[Req, Resp any] interface {
	// Decode fills in (a *In from Route.NewInput) from req. An error that is
	// not an *Error becomes Invalid with its text.
	Decode(ctx context.Context, r Route, req Req, in any) error
	// Encode writes a successful result (the handler's *Out, possibly nil).
	Encode(ctx context.Context, r Route, resp Resp, out any) error
	// EncodeError writes a failure. f.Message is safe to show the client.
	EncodeError(ctx context.Context, r Route, resp Resp, f Failure) error
}

// Stage tells where a request failed.
type Stage int

const (
	StageDecode Stage = iota // reading the input
	StageHandle              // validation, middlewares, business logic
	StageEncode              // writing the result or the error
)

func (s Stage) String() string {
	switch s {
	case StageDecode:
		return "decode"
	case StageHandle:
		return "handle"
	case StageEncode:
		return "encode"
	}
	return fmt.Sprintf("stage(%d)", int(s))
}

// Failure is a failed request, already classified.
type Failure struct {
	Route   string
	Stage   Stage
	Kind    Kind
	Message string // safe for the client: the *Error message, or the configured internal message
	Err     error  // the full error, for logs only
}

// Pipeline runs every request the same way, whatever the transport:
//
//	decode → middlewares → Validate → handler → encode   (or encode the error)
//
// It classifies errors, hides Internal messages, turns panics anywhere —
// including in the Codec — into Internal failures, and reports failures and
// observations. Apps call Serve once per request.
type Pipeline[Req, Resp any] struct {
	codec Codec[Req, Resp]
	cfg   pipelineConfig
}

type pipelineConfig struct {
	internalMessage string
	onFailure       func(ctx context.Context, f Failure)
	observe         func(ctx context.Context, r Route, err error, took time.Duration)
}

// PipelineOption configures a Pipeline.
type PipelineOption func(*pipelineConfig)

// InternalMessage sets the text clients get for Internal errors
// (default "internal error").
func InternalMessage(msg string) PipelineOption {
	return func(c *pipelineConfig) { c.internalMessage = msg }
}

// OnFailure is called for every failure, with the full error — the place to
// log. It is also called when writing a response fails.
func OnFailure(fn func(ctx context.Context, f Failure)) PipelineOption {
	return func(c *pipelineConfig) { c.onFailure = fn }
}

// Observe is called once per request with its outcome and duration — the
// place for metrics and access logs. err is nil on success.
func Observe(fn func(ctx context.Context, r Route, err error, took time.Duration)) PipelineOption {
	return func(c *pipelineConfig) { c.observe = fn }
}

// NewPipeline creates a pipeline for one transport.
func NewPipeline[Req, Resp any](codec Codec[Req, Resp], opts ...PipelineOption) *Pipeline[Req, Resp] {
	if codec == nil {
		panic("rockdelivery: NewPipeline with a nil Codec")
	}
	cfg := pipelineConfig{internalMessage: "internal error"}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Pipeline[Req, Resp]{codec: codec, cfg: cfg}
}

// Serve handles one request. It returns the request's error (nil on
// success) so an App can act on it, e.g. a consumer deciding to retry; the
// response has already been written either way.
func (p *Pipeline[Req, Resp]) Serve(ctx context.Context, r Route, req Req, resp Resp) (err error) {
	start := time.Now()
	defer func() { p.observe(ctx, r, err, time.Since(start)) }()

	in := r.NewInput()
	if decErr := guard(func() error { return p.codec.Decode(ctx, r, req, in) }); decErr != nil {
		if !isDeliveryError(decErr) && !errors.Is(decErr, ErrPanic) {
			decErr = &Error{Kind: KindInvalid, Message: decErr.Error(), Err: decErr}
		}
		p.fail(ctx, r, resp, StageDecode, decErr)
		return decErr
	}

	out, callErr := r.Call(ctx, in)
	if callErr != nil {
		p.fail(ctx, r, resp, StageHandle, callErr)
		return callErr
	}

	if encErr := guard(func() error { return p.codec.Encode(ctx, r, resp, out) }); encErr != nil {
		// The answer may be half-written; report it, don't try to write an error over it.
		p.report(ctx, p.failure(r, StageEncode, encErr))
		return encErr
	}
	return nil
}

func (p *Pipeline[Req, Resp]) fail(ctx context.Context, r Route, resp Resp, stage Stage, err error) {
	f := p.failure(r, stage, err)
	p.report(ctx, f)
	if encErr := guard(func() error { return p.codec.EncodeError(ctx, r, resp, f) }); encErr != nil {
		p.report(ctx, p.failure(r, StageEncode, encErr))
	}
}

func (p *Pipeline[Req, Resp]) failure(r Route, stage Stage, err error) Failure {
	f := Failure{Route: r.name, Stage: stage, Kind: KindOf(err), Err: err}
	if f.Kind == KindInternal {
		f.Message = p.cfg.internalMessage
	} else {
		f.Message = MessageOf(err)
	}
	return f
}

func (p *Pipeline[Req, Resp]) report(ctx context.Context, f Failure) {
	if p.cfg.onFailure == nil {
		return
	}
	_ = guard(func() error { p.cfg.onFailure(ctx, f); return nil }) // a broken logger must not break serving
}

func (p *Pipeline[Req, Resp]) observe(ctx context.Context, r Route, err error, took time.Duration) {
	if p.cfg.observe == nil {
		return
	}
	_ = guard(func() error { p.cfg.observe(ctx, r, err, took); return nil })
}

// guard runs fn, turning a panic into an Internal error wrapping ErrPanic.
func guard(fn func() error) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("%w: %v\n%s", ErrPanic, rec, debug.Stack())
		}
	}()
	return fn()
}

func isDeliveryError(err error) bool {
	var e *Error
	return errors.As(err, &e)
}
