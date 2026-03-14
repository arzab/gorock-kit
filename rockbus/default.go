package rockbus

import (
	"context"
	"sync"
)

var (
	defaultMu  sync.RWMutex
	defaultApp *App
)

// SetDefault sets the global App used by the package-level functions.
// Call this once during application startup before publishing any events.
func SetDefault(app *App) {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultApp = app
}

// Default returns the global App.
// Panics with a descriptive message if SetDefault was never called.
func Default() *App {
	defaultMu.RLock()
	defer defaultMu.RUnlock()
	if defaultApp == nil {
		panic("rockbus: SetDefault must be called before using package-level functions")
	}
	return defaultApp
}

// Subscribe registers a raw Handler on the default App.
// Prefer the generic Subscribe[T] helper for type-safe subscriptions.
func Subscribe(topic Topic, handler Handler) {
	Default().Subscribe(topic, handler)
}

// Publish delivers event synchronously via the default App.
func Publish(ctx context.Context, event Event) error {
	return Default().Publish(ctx, event)
}

// PublishAsync enqueues event for async delivery via the default App.
func PublishAsync(ctx context.Context, event Event) {
	Default().PublishAsync(ctx, event)
}
