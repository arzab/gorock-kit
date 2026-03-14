package rockcron

import "context"

// Handler is a single step in a job's execution chain.
// If a handler returns an error the remaining handlers are not called.
type Handler func(ctx context.Context) error
