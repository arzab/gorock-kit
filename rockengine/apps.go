package rockengine

import "context"

// App defines the lifecycle of an application managed by Engine.
//
// Call order: Init → Exec → Stop
//
// Shutdown contract:
//   - Exec must stop accepting new work when ctx is cancelled and return
//     after finishing any in-flight operations.
//   - Stop is called concurrently with ctx cancellation. For blocking Exec
//     (e.g. HTTP server), Stop must unblock it gracefully — waiting for
//     in-flight work before returning.
//   - Stop must not forcefully terminate ongoing work; the Engine enforces
//     a shutdown timeout as a last resort.
type App interface {
	Init(ctx context.Context) error
	Exec(ctx context.Context) error
	Stop() []error
}
