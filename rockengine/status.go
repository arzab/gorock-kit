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
type RestartPolicy struct {
	MaxRetries    int
	Delay         time.Duration  // wait between restart attempts
	RestartOnExit bool           // restart even if Exec returns nil (unexpected clean exit)
	OnFatal       func(err error) // called when the app is marked as failed; overrides Engine.WithFatalHandler for this app
}
