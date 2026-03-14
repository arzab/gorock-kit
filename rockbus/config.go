package rockbus

import "context"

// Config configures the event App.
type Config struct {
	// QueueSize is the buffer capacity of each per-topic async queue.
	// PublishAsync drops events when a topic's queue is full and calls OnError.
	// Default: 1024.
	QueueSize int

	// OnError is called when an async handler returns an error or panics,
	// or when a queue is full, the app is stopped, or a topic has no worker.
	// If nil, errors are silently dropped.
	OnError func(ctx context.Context, event Event, err error)
}
