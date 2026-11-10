package rockservices

import "context"

// Service is what every service in pkg/services implements: a wrapper over a
// technology or a piece of logic that has a lifecycle. The service has no
// name of its own — the name is its key in the catalog map.
type Service interface {
	// Init opens connections. ctx bounds startup (e.g. a connect timeout).
	Init(ctx context.Context) error
	// Stop releases everything Init opened.
	Stop() error
}
