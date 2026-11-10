package rockservices

import "context"

// Service is what every service in pkg/services implements: a wrapper over a
// technology or a piece of logic that has a lifecycle. Its name is not part
// of the service — it is the Key it is added under, so the same service can
// be added twice under two keys.
//
//	// pkg/services/orders/interface.go
//	type Service interface {
//	    rockservices.Service
//	    Insert(ctx context.Context, order any) error
//	}
type Service interface {
	// Init opens connections. ctx bounds startup (e.g. a connect timeout).
	Init(ctx context.Context) error
	// Stop releases everything Init opened.
	Stop() error
}
