package rockdelivery

import (
	"errors"
	"fmt"
)

// Kind classifies an error independently of the transport. Each App maps
// kinds to its protocol: HTTP to a status code, a queue to ack/retry, a CLI
// to an exit code.
type Kind int

const (
	// KindInternal is an unexpected failure. Any error that is not an
	// *Error is Internal. Apps hide its message from clients.
	KindInternal     Kind = iota
	KindInvalid           // bad input
	KindUnauthorized      // caller is not authenticated
	KindForbidden         // caller may not do this
	KindNotFound          // the thing does not exist
	KindConflict          // state conflict: duplicate, version mismatch
	KindUnavailable       // temporarily unavailable; retrying may help
)

var kindNames = [...]string{"internal", "invalid", "unauthorized", "forbidden", "not_found", "conflict", "unavailable"}

func (k Kind) String() string {
	if k >= 0 && int(k) < len(kindNames) {
		return kindNames[k]
	}
	return fmt.Sprintf("kind(%d)", int(k))
}

// Error is a business error with a kind and a client-safe message.
type Error struct {
	Kind    Kind
	Message string // safe to show to the client
	Err     error  // optional cause, for logs; never shown to clients
}

func (e *Error) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

func (e *Error) Unwrap() error { return e.Err }

// Wrap attaches a cause for logging and returns e.
//
//	return rockdelivery.Unavailable("payments are down").Wrap(err)
func (e *Error) Wrap(err error) *Error {
	e.Err = err
	return e
}

// New creates an error of the given kind. msg is formatted with args when
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

// KindOf returns the kind of err: the kind of the first *Error in its chain,
// otherwise KindInternal. err must not be nil.
func KindOf(err error) Kind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return KindInternal
}

// MessageOf returns the client-safe message of err: the message of the first
// *Error in its chain, or "" for an Internal error, whose text must not leak.
func MessageOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Message
	}
	return ""
}
