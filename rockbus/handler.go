package rockbus

import "context"

// Handler processes an event. The payload and topic are available via
// Payload[T] and CurrentTopic helpers on the context.
// Returning an error marks this handler as failed; other registered handlers
// for the same topic still execute.
type Handler func(ctx context.Context) error
