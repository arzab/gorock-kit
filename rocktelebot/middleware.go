package rocktelebot

import (
	"fmt"
	"runtime/debug"

	tele "gopkg.in/telebot.v3"
)

// Recovery returns a middleware that recovers panics in handlers.
// onError is called with the context and the recovered value.
// If onError is nil, the panic is written to stderr.
//
// Example:
//
//	rocktelebot.Recovery(func(c tele.Context, err interface{}) {
//	    log.Error("handler panic", rocklog.Any("err", err))
//	})
func Recovery(onError func(c tele.Context, err interface{})) tele.MiddlewareFunc {
	return func(next tele.HandlerFunc) tele.HandlerFunc {
		return func(c tele.Context) (err error) {
			defer func() {
				if r := recover(); r != nil {
					if onError != nil {
						onError(c, r)
					} else {
						_, _ = fmt.Fprintf(errWriter, "rocktelebot: handler panic: %v\n%s\n", r, debug.Stack())
					}
				}
			}()
			return next(c)
		}
	}
}

// Logger returns a middleware that calls fn for each incoming update
// before passing it to the handler. Use it for logging or tracing.
//
// Example:
//
//	rocktelebot.Logger(func(c tele.Context) {
//	    log.Info("update",
//	        rocklog.Int64("user_id", c.Sender().ID),
//	        rocklog.Str("text", c.Text()),
//	    )
//	})
func Logger(fn func(c tele.Context)) tele.MiddlewareFunc {
	return func(next tele.HandlerFunc) tele.HandlerFunc {
		return func(c tele.Context) error {
			fn(c)
			return next(c)
		}
	}
}
