package rockservices

import "context"

// Service is what every service in pkg/services implements.
//
//	// pkg/services/repository/interface.go
//	const Name = "repository"
//
//	type Service interface {
//	    rockservices.Service
//	    Insert(ctx context.Context, model any) error
//	}
//
//	// pkg/services/repository/service.go
//	func (s *service) Name() string { return Name }
type Service interface {
	// Name is the key the service is stored and looked up under. By
	// convention it returns the package's Name constant.
	Name() string
	// Init opens connections. ctx bounds startup (e.g. a connect timeout).
	Init(ctx context.Context) error
	// Stop releases everything Init opened.
	Stop() error
}
