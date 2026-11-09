package rockengine

import "time"

// State represents the current runtime state of an app.
type State string

const (
	StateIdle       State = "idle"
	StateRunning    State = "running"
	StateStopped    State = "stopped"
	StateFailed     State = "failed"
	StateRestarting State = "restarting"
)

// AppInfo holds the current runtime status of a registered app.
type AppInfo struct {
	Name    string
	State   State
	Err     error // last error, nil if none
	Retries int
}

// RestartPolicy defines how a failed app should be restarted.
//
//   - MaxRetries = 0: no restart (default)
//   - MaxRetries = -1: unlimited restarts
//   - MaxRetries > 0: restart up to N times, then mark as failed
//
// A panic in Exec counts as a failure and follows the same policy.
type RestartPolicy struct {
	MaxRetries    int
	Delay         time.Duration // wait between restart attempts
	RestartOnExit bool          // restart even if Exec returns nil (unexpected clean exit); not counted as a retry

	// ResetAfter resets the retry counter when a run lasted at least this long
	// before failing, so MaxRetries limits crash loops rather than lifetime
	// failures. 0 = never reset.
	ResetAfter time.Duration

	// OnFatal is called when the app is marked as failed; overrides
	// Engine.WithFatalHandler for this app.
	OnFatal func(err error)
}
