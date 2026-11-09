package rocktelebot

import "context"

// lifecycle mirrors rockengine.App. It is duplicated here on purpose: modules
// must not import each other, but a signature drift must still fail the build.
type lifecycle interface {
	Init(ctx context.Context) error
	Exec(ctx context.Context) error
	Stop() []error
}

var _ lifecycle = (*App)(nil)
