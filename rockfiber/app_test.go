package rockfiber

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
)

const testAdminPassword = "s3cret"

// newTestApp runs Init and registers routes without listening on a port.
func newTestApp(t *testing.T, cfg Config, endpoints ...FiberEndpoint) *FiberApp {
	t.Helper()
	if cfg.Port == "" {
		cfg.Port = "0"
	}
	a := New(cfg, endpoints...)
	if err := a.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	a.setupEndpoints(a.fiber)
	return a
}

func do(t *testing.T, a *FiberApp, req *http.Request) (*http.Response, string) {
	t.Helper()
	resp, err := a.fiber.Test(req, -1)
	if err != nil {
		t.Fatalf("request %s %s: %v", req.Method, req.URL, err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, string(body)
}

func TestPprofRequiresAdminPassword(t *testing.T) {
	a := newTestApp(t, Config{Pprof: true, AdminPassword: testAdminPassword})

	resp, _ := do(t, a, httptest.NewRequest(http.MethodGet, "/admin/debug/pprof/", nil))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("without password: got %d, want 401", resp.StatusCode)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/debug/pprof/", nil)
	req.Header.Set("X-Admin-Password", testAdminPassword)
	resp, _ = do(t, a, req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("with password: got %d, want 200", resp.StatusCode)
	}
}

func TestPprofDisabledByDefault(t *testing.T) {
	a := newTestApp(t, Config{AdminPassword: testAdminPassword})

	req := httptest.NewRequest(http.MethodGet, "/admin/debug/pprof/", nil)
	req.Header.Set("X-Admin-Password", testAdminPassword)
	resp, _ := do(t, a, req)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("got %d, want 404", resp.StatusCode)
	}
}

func TestPprofCustomAdminPath(t *testing.T) {
	a := newTestApp(t, Config{Pprof: true, AdminPassword: testAdminPassword, AdminEndpointsPath: "/ops/"})

	resp, _ := do(t, a, httptest.NewRequest(http.MethodGet, "/ops/debug/pprof/", nil))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("without password: got %d, want 401", resp.StatusCode)
	}
}

func TestMaskCannotBeBypassedByClient(t *testing.T) {
	const secret = "pq: password authentication failed for user admin"
	var logged string
	a := newTestApp(t,
		Config{
			MaskInternalServerErrorMessage: "internal server error",
			OnError:                        func(_ string, e *ErrorResponse) { logged = e.Message },
		},
		GET("/boom", func(c *fiber.Ctx) error { return errors.New(secret) }),
	)

	for _, url := range []string{"/boom", "/boom?debug=1", "/boom?debug=true"} {
		resp, body := do(t, a, httptest.NewRequest(http.MethodGet, url, nil))
		if resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("%s: got %d, want 500", url, resp.StatusCode)
		}
		if strings.Contains(body, secret) {
			t.Fatalf("%s: response leaks internal error: %s", url, body)
		}
		if !strings.Contains(body, "internal server error") {
			t.Fatalf("%s: response missing mask: %s", url, body)
		}
	}
	if logged != secret {
		t.Fatalf("OnError got %q, want the real message %q", logged, secret)
	}
}

func TestUnmaskedWhenMaskNotSet(t *testing.T) {
	a := newTestApp(t, Config{}, GET("/boom", func(c *fiber.Ctx) error { return errors.New("raw") }))
	_, body := do(t, a, httptest.NewRequest(http.MethodGet, "/boom", nil))
	if !strings.Contains(body, "raw") {
		t.Fatalf("expected raw message, got %s", body)
	}
}

func TestClientErrorsAreNotMasked(t *testing.T) {
	a := newTestApp(t,
		Config{MaskInternalServerErrorMessage: "internal server error"},
		GET("/bad", func(c *fiber.Ctx) error { return NewError(http.StatusBadRequest, "name is required") }),
	)
	_, body := do(t, a, httptest.NewRequest(http.MethodGet, "/bad", nil))
	if !strings.Contains(body, "name is required") {
		t.Fatalf("400 message must not be masked, got %s", body)
	}
}

func TestCorsDisabledByDefault(t *testing.T) {
	a := newTestApp(t, Config{}, GET("/x", func(c *fiber.Ctx) error { return c.SendString("ok") }))
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Origin", "https://evil.example")
	resp, _ := do(t, a, req)
	if v := resp.Header.Get("Access-Control-Allow-Origin"); v != "" {
		t.Fatalf("CORS must be off without CorsConfig, got Access-Control-Allow-Origin=%q", v)
	}
}

func TestCorsExplicitConfig(t *testing.T) {
	a := newTestApp(t,
		Config{CorsConfig: &cors.Config{AllowOrigins: "https://app.example"}},
		GET("/x", func(c *fiber.Ctx) error { return c.SendString("ok") }),
	)
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Origin", "https://app.example")
	resp, _ := do(t, a, req)
	if v := resp.Header.Get("Access-Control-Allow-Origin"); v != "https://app.example" {
		t.Fatalf("got Access-Control-Allow-Origin=%q, want https://app.example", v)
	}
}

func TestTraceIdInUserContext(t *testing.T) {
	a := newTestApp(t, Config{UseTraceId: true}, GET("/t", func(c *fiber.Ctx) error {
		return c.SendString(TraceIdFromContext(c.UserContext()))
	}))

	req := httptest.NewRequest(http.MethodGet, "/t", nil)
	req.Header.Set(TraceIdKey, "abc-123")
	resp, body := do(t, a, req)
	if body != "abc-123" || resp.Header.Get(TraceIdKey) != "abc-123" {
		t.Fatalf("body=%q header=%q", body, resp.Header.Get(TraceIdKey))
	}

	// Survives RequestTimeoutMiddleware, which wraps the user context.
	a = newTestApp(t, Config{UseTraceId: true, RequestTimeout: time.Second}, GET("/t", func(c *fiber.Ctx) error {
		return c.SendString(TraceIdFromContext(c.UserContext()))
	}))
	_, body = do(t, a, httptest.NewRequest(http.MethodGet, "/t", nil))
	if len(body) != 36 { // generated UUID
		t.Fatalf("expected generated trace id, got %q", body)
	}
}
