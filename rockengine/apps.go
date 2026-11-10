package rockengine

import "context"

// App defines the lifecycle of an application managed by Engine.
//
// Call order: Init → Exec → Stop. With Engine.WithRestarts a failed App is
// restarted: Stop → Init → Exec.
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
