package rockengine

import (
	"context"
	"time"
)

// defaultEngine is the engine used by package-level functions.
var defaultEngine = NewEngine()

// SetShutdownTimeout sets the shutdown timeout on the default engine.
func SetShutdownTimeout(d time.Duration) {
	defaultEngine.WithShutdownTimeout(d)
}

// SetFatalHandler sets the fatal handler on the default engine.
func SetFatalHandler(fn func(name string, err error)) {
	defaultEngine.WithFatalHandler(fn)
}

// Register adds a named app to the default engine.
// Panics if the name is already registered or the engine is running.
func Register(name string, app App, policy ...RestartPolicy) {
	defaultEngine.MustRegister(name, app, policy...)
}

// Run starts the default engine.
func Run() error {
	return defaultEngine.Run()
}

// RunContext starts the default engine with the given context.
func RunContext(ctx context.Context) error {
	return defaultEngine.RunContext(ctx)
}

// StopApp stops a single app in the default engine.
func StopApp(name string) error {
	return defaultEngine.StopApp(name)
}

// RestartApp restarts a single app in the default engine.
func RestartApp(name string) error {
	return defaultEngine.RestartApp(name)
}

// AppStatus returns the status of a single app in the default engine.
func AppStatus(name string) (AppInfo, error) {
	return defaultEngine.AppStatus(name)
}

// AppStatuses returns the status of all apps in the default engine.
func AppStatuses() []AppInfo {
	return defaultEngine.AppStatuses()
}

// Shutdown stops all apps in the default engine.
func Shutdown() []error {
	return defaultEngine.Shutdown()
}
