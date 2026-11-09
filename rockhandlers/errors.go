package rockhandlers

import (
	"context"
	"errors"
	"fmt"
	"maps"
)

// Kind classifies an error independently of the transport. Each transport
// maps kinds to its protocol: HTTP to a status code, a queue to ack/requeue,
// a CLI to an exit code.
type Kind int

const (
	// KindInternal is an unexpected failure. Any error that is not an *Error
	// (and not a context error) is Internal. Transports hide its message.
	KindInternal     Kind = iota
	KindInvalid           // bad input
	KindUnauthorized      // caller is not authenticated
	KindForbidden         // caller may not do this
	KindNotFound          // the thing does not exist
	KindConflict          // state conflict: duplicate, version mismatch
	KindUnavailable       // temporarily unavailable; retrying may help
	KindCanceled          // the context was cancelled: caller gone or shutting down
	KindTimeout           // the context deadline passed
)

var kindNames = [...]string{"internal", "invalid", "unauthorized", "forbidden", "not_found", "conflict", "unavailable", "canceled", "timeout"}

func (k Kind) String() string {
	if k >= 0 && int(k) < len(kindNames) {
		return kindNames[k]
	}
	return fmt.Sprintf("kind(%d)", int(k))
}

// Error is an error with a kind and a client-safe message.
type Error struct {
	Kind    Kind
	Message string            // safe to show to the client
	Fields  map[string]string // per-field problems (validation); may be nil
	Err     error             // cause, for logs only
}

func (e *Error) Error() string {
	s := e.Kind.String()
	if e.Message != "" {
		s += ": " + e.Message
	}
	if e.Err != nil {
		s += ": " + e.Err.Error()
	}
	return s
}

func (e *Error) Unwrap() error { return e.Err }

// Wrap returns a copy of e with cause err (for logs).
//
//	return rockhandlers.Unavailable("payments are down").Wrap(err)
func (e *Error) Wrap(err error) *Error {
	cp := *e
	cp.Err = err
	return &cp
}

// WithFields returns a copy of e with per-field problems.
//
//	return rockhandlers.Invalid("invalid order").WithFields(map[string]string{"qty": "must be > 0"})
func (e *Error) WithFields(fields map[string]string) *Error {
	cp := *e
	cp.Fields = maps.Clone(fields)
	return &cp
}

// New creates an error of the given kind; msg is formatted with args when
// args are given.
func New(kind Kind, msg string, args ...any) *Error {
	if len(args) > 0 {
		msg = fmt.Sprintf(msg, args...)
	}
	return &Error{Kind: kind, Message: msg}
}

func Invalid(msg string, args ...any) *Error      { return New(KindInvalid, msg, args...) }
func Unauthorized(msg string, args ...any) *Error { return New(KindUnauthorized, msg, args...) }
func Forbidden(msg string, args ...any) *Error    { return New(KindForbidden, msg, args...) }
func NotFound(msg string, args ...any) *Error     { return New(KindNotFound, msg, args...) }
func Conflict(msg string, args ...any) *Error     { return New(KindConflict, msg, args...) }
func Unavailable(msg string, args ...any) *Error  { return New(KindUnavailable, msg, args...) }

// KindOf classifies err: the kind of the first *Error in its chain;
// otherwise context.Canceled → KindCanceled, context.DeadlineExceeded →
// KindTimeout, anything else → KindInternal.
func KindOf(err error) Kind {
	var e *Error
	switch {
	case errors.As(err, &e):
		return e.Kind
	case errors.Is(err, context.Canceled):
		return KindCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return KindTimeout
	}
	return KindInternal
}

// MessageOf returns the client-safe message of err: the message of the first
// *Error in its chain, or "" for Internal errors and errors without one.
func MessageOf(err error) string {
	var e *Error
	if errors.As(err, &e) && e.Kind != KindInternal {
		return e.Message
	}
	return ""
}

// FieldsOf returns the per-field problems of the first *Error in err's chain.
func FieldsOf(err error) map[string]string {
	var e *Error
	if errors.As(err, &e) {
		return e.Fields
	}
	return nil
}

// PanicError is the cause of the Internal error a recovered panic becomes.
type PanicError struct {
	Value any
	Stack []byte
}

func (p *PanicError) Error() string { return fmt.Sprintf("panic: %v", p.Value) }
