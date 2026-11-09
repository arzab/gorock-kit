package rocklog

import "context"

type loggerCtxKey struct{}

// WithContext returns a copy of ctx carrying a logger with fields attached.
// Fields accumulate: calling WithContext on a context that already carries a
// logger adds to that logger's fields.
//
// Typical use is request-scoped fields set once in middleware:
//
//	ctx = rocklog.WithContext(ctx, rocklog.Str("trace_id", id))
//	// ... deeper in the call stack:
//	rocklog.FromContext(ctx).Info("user created") // includes trace_id
func WithContext(ctx context.Context, fields ...Field) context.Context {
	return context.WithValue(ctx, loggerCtxKey{}, FromContext(ctx).With(fields...))
}

// ContextWithLogger returns a copy of ctx carrying l.
func ContextWithLogger(ctx context.Context, l Logger) context.Context {
	return context.WithValue(ctx, loggerCtxKey{}, l)
}

// FromContext returns the logger stored in ctx, or the default logger if
// ctx carries none. It never returns nil.
func FromContext(ctx context.Context) Logger {
	if ctx != nil {
		if l, ok := ctx.Value(loggerCtxKey{}).(Logger); ok && l != nil {
			return l
		}
	}
	return Default()
}
