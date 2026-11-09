package rockengine

import "context"

// App defines the lifecycle of an application managed by Engine.
//
// Call order: Init → Exec → Stop. On restart: Stop → Init → Exec.
//
// Shutdown contract:
//   - Exec must stop accepting new work when ctx is cancelled and return
//     after finishing any in-flight operations. Whatever it returns after
//     cancellation is treated as a clean stop.
//   - Stop is called right after ctx cancellation, possibly while Exec is
//     still running. For blocking Exec (e.g. HTTP server), Stop must unblock
//     it gracefully — waiting for in-flight work before returning.
//   - The engine calls Stop at most once per successful Init.
//   - Stop must not forcefully terminate ongoing work; the Engine enforces
//     a shutdown timeout as a last resort.
type App interface {
	Init(ctx context.Context) error
	Exec(ctx context.Context) error
	Stop() []error
}

// Resource is a dependency without its own run loop — a database pool, a
// cache client, a message-broker connection. Register it with
// RegisterResource.
//
// Resources are initialised before any App, in registration order, and
// stopped after every App has stopped, in reverse order. So an HTTP server
// finishes its in-flight requests before the database pool it uses is closed.
//
// A GOROCK service in pkg/services (Init(ctx) error / Stop() error) satisfies it.
type Resource interface {
	Init(ctx context.Context) error
	Stop() error
}
