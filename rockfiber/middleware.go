package rockfiber

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/monitor"
	"github.com/gofiber/fiber/v2/middleware/pprof"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/google/uuid"
)

const TraceIdKey = "X-Trace-Id"

var defaultMonitorConfig = monitor.Config{
	Title:   "Monitoring",
	Refresh: 1,
	APIOnly: false,
}

// RecoverHandler recovers from panics and writes the stack trace to stderr.
func RecoverHandler() fiber.Handler {
	return recover.New(recover.Config{
		EnableStackTrace: true,
		StackTraceHandler: func(c *fiber.Ctx, e interface{}) {
			_, _ = os.Stderr.WriteString(fmt.Sprintf("panic: %v\n%s\n", e, debug.Stack()))
		},
	})
}

// PprofHandler registers pprof routes under the given path prefix.
func PprofHandler(prefix string) fiber.Handler {
	return pprof.New(pprof.Config{Prefix: prefix})
}

// CorsHandler returns a CORS middleware. Passing nil uses the fiber default policy.
func CorsHandler(cfg *cors.Config) fiber.Handler {
	if cfg != nil {
		return cors.New(*cfg)
	}
	return cors.New()
}

// MetricsHandler returns a monitor middleware. Passing nil uses a sensible default.
func MetricsHandler(cfg *monitor.Config) fiber.Handler {
	if cfg != nil {
		return monitor.New(*cfg)
	}
	return monitor.New(defaultMonitorConfig)
}

// TraceIdMiddleware generates a new X-Trace-Id UUID for each request, or propagates
// an existing one if already present in the incoming request headers.
// Incoming values are sanitised: values longer than 64 chars or containing
// control characters (e.g. newlines) are rejected and a fresh UUID is generated instead.
func TraceIdMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		traceId := c.Get(TraceIdKey)
		if !isValidTraceId(traceId) {
			traceId = uuid.NewString()
		}
		c.Set(TraceIdKey, traceId)
		c.Locals(TraceIdKey, traceId)
		return c.Next()
	}
}

// isValidTraceId checks that a client-supplied trace ID is safe to log and propagate.
// Rejects empty values, values over 64 chars, and values containing control characters.
func isValidTraceId(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	return !strings.ContainsAny(s, "\r\n\t")
}

// RequestLogMiddleware calls fn with the request URI and trace ID before each handler.
// Use the OnRequest hook in Config instead of wiring this manually.
func RequestLogMiddleware(fn func(path, traceId string)) fiber.Handler {
	return func(c *fiber.Ctx) error {
		traceId, _ := c.Locals(TraceIdKey).(string)
		fn(string(c.Request().RequestURI()), traceId)
		return c.Next()
	}
}

// AdminAuthMiddleware checks the X-Admin-Password request header.
// If password is empty, all requests are denied.
// Uses constant-time comparison to prevent timing attacks.
func AdminAuthMiddleware(password string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		provided := c.Get("X-Admin-Password")
		if password == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(password)) != 1 {
			return NewError(http.StatusUnauthorized, "unauthorized")
		}
		return c.Next()
	}
}

// RequestTimeoutMiddleware sets a context deadline on every request.
// Any context-aware operation (DB queries, HTTP calls, etc.) will respect this deadline.
// d must be greater than zero; use Config.RequestTimeout instead of wiring this manually.
func RequestTimeoutMiddleware(d time.Duration) fiber.Handler {
	return func(c *fiber.Ctx) error {
		ctx, cancel := context.WithTimeout(c.UserContext(), d)
		defer cancel()
		c.SetUserContext(ctx)
		return c.Next()
	}
}

// ErrorHandler returns a fiber error handler that serialises errors as ErrorResponse JSON.
//
// maskMessage, when non-empty, replaces raw internal server error messages.
// The original message is still shown when the ?debug= query param is present.
//
// onError is an optional callback invoked for every error response — use it to integrate
// rocklog or any other logger without coupling rockfiber to a specific library.
func ErrorHandler(
	maskMessage string,
	onError func(traceId string, err *ErrorResponse),
) func(*fiber.Ctx, error) error {
	return func(c *fiber.Ctx, err error) error {
		var errResp *ErrorResponse
		var fiberErr *fiber.Error

		switch {
		case errors.As(err, &errResp):
			// already an ErrorResponse — use as-is
		case errors.As(err, &fiberErr):
			errResp = NewError(fiberErr.Code, fiberErr.Message)
		default:
			msg := err.Error()
			if maskMessage != "" && c.Query("debug") == "" {
				msg = maskMessage
			}
			errResp = NewError(http.StatusInternalServerError, msg).WithSource("internal")
		}

		if onError != nil {
			traceId, _ := c.Locals(TraceIdKey).(string)
			onError(traceId, errResp)
		}

		return c.Status(errResp.Code).JSON(errResp)
	}
}
