package rockhandlers

import "fmt"

// Local is a typed key for request-scoped values passed between steps and
// middlewares. Declare it once at package level:
//
//	var UserKey = rockhandlers.NewLocal[*User]("user")
//
//	UserKey.Set(c, u)
//	u, ok := UserKey.Get(c)
//	u := UserKey.MustGet(c)
//
// Keys are compared by identity, so two keys with the same name never
// collide.
type Local[T any] struct {
	name string
}

// NewLocal creates a key; name is used in error messages.
func NewLocal[T any](name string) *Local[T] { return &Local[T]{name: name} }

// Name returns the key name.
func (k *Local[T]) Name() string { return k.name }

// Set stores v for the current request.
func (k *Local[T]) Set(c Context, v T) { c.setLocal(k, v) }

// Get returns the value and whether it was set.
func (k *Local[T]) Get(c Context) (T, bool) {
	v, ok := c.locals()[k]
	if !ok {
		var zero T
		return zero, false
	}
	return v.(T), true
}

// MustGet returns the value or panics if it was not set — a chain wiring
// bug (a step runs without the one that sets it). The panic becomes an
// Internal error.
func (k *Local[T]) MustGet(c Context) T {
	v, ok := k.Get(c)
	if !ok {
		panic(fmt.Sprintf("rockhandlers: local %q is not set (route %q)", k.name, c.Route()))
	}
	return v
}
