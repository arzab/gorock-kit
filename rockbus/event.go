package rockbus

import (
	"context"
	"errors"
	"fmt"
)

// Topic identifies an event type, e.g. "user.created", "order.placed".
type Topic string

// Event is the envelope delivered to handlers.
type Event struct {
	Topic   Topic
	Payload any
}

// ErrPayload is returned by Payload[T] when the context has no payload
// or the payload type does not match T.
var ErrPayload = errors.New("rockbus: payload error")

// unexported context key types — prevent collisions with other packages.
type (
	payloadCtxKey struct{}
	topicCtxKey   struct{}
)

// injectEvent stores the event payload and topic in ctx before calling a handler.
func injectEvent(ctx context.Context, event Event) context.Context {
	ctx = context.WithValue(ctx, payloadCtxKey{}, event.Payload)
	ctx = context.WithValue(ctx, topicCtxKey{}, event.Topic)
	return ctx
}

// Payload retrieves and type-asserts the event payload from ctx.
// Returns ErrPayload if the context has no payload or the type does not match T.
//
// Example:
//
//	func onUserCreated(ctx context.Context) error {
//	    p, err := rockbus.Payload[UserCreated](ctx)
//	    if err != nil { return err }
//	    // use p.UserID, p.Email ...
//	}
func Payload[T any](ctx context.Context) (T, error) {
	val := ctx.Value(payloadCtxKey{})
	if val == nil {
		var zero T
		return zero, fmt.Errorf("%w: no payload in context", ErrPayload)
	}
	typed, ok := val.(T)
	if !ok {
		var zero T
		return zero, fmt.Errorf("%w: got %T, want %T", ErrPayload, val, zero)
	}
	return typed, nil
}

// CurrentTopic returns the topic of the event currently being handled.
// Returns an empty string if called outside a handler.
func CurrentTopic(ctx context.Context) Topic {
	t, _ := ctx.Value(topicCtxKey{}).(Topic)
	return t
}
