package rockbus

import (
	"context"
	"fmt"
)

// ctxKey is an unexported type for context keys to avoid collisions with
// keys from other packages.
type ctxKey string

// WithValue stores value under key in ctx and returns the derived context.
// Use GetValue to retrieve it downstream in handlers or middleware.
//
// Example:
//
//	ctx = rockbus.WithValue(ctx, "requestId", id)
func WithValue(ctx context.Context, key string, value any) context.Context {
	return context.WithValue(ctx, ctxKey(key), value)
}

// GetValue retrieves and type-asserts the value stored under key.
// Returns an error if the key is missing or the type does not match.
//
// Example:
//
//	id, err := rockbus.GetValue[string](ctx, "requestId")
func GetValue[T any](ctx context.Context, key string) (T, error) {
	val := ctx.Value(ctxKey(key))
	if val == nil {
		var zero T
		return zero, fmt.Errorf("rockbus: key %q not found in context", key)
	}
	typed, ok := val.(T)
	if !ok {
		var zero T
		return zero, fmt.Errorf("rockbus: key %q has type %T, want %T", key, val, zero)
	}
	return typed, nil
}
