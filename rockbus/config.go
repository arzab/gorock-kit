package rockbus

// Config configures the event App.
type Config struct {
	// QueueSize is the buffer capacity of each per-topic async queue.
	// PublishAsync drops events when a topic's queue is full and calls OnError.
	// Default: 1024.
	QueueSize int
}
