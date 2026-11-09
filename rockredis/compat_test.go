package rockredis

import "context"

// resource mirrors rockengine.Resource. It is duplicated here on purpose:
// modules must not import each other, but a signature drift must still fail
// the build — Service is meant to be passed to rockengine.RegisterResource.
type resource interface {
	Init(ctx context.Context) error
	Stop() error
}

var _ resource = Service(nil)
