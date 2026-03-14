# rockfiber

A fiber-based HTTP server wrapper that integrates with [rockengine](../rockengine) and provides structured routing, request parsing, error handling, and production middlewares.

## Overview

`rockfiber` wraps [gofiber/fiber v2](https://github.com/gofiber/fiber) with a clean API for:
- Registering endpoints with typed request parsing and validation
- Lifecycle management compatible with the rockengine `App` interface
- Built-in middlewares: CORS, compression, security headers, rate limiting, tracing, pprof, metrics
- Structured error responses
- TLS / HTTPS support

## Quick Start

```go
app := rockfiber.New(
    rockfiber.Config{
        Port:       "8080",
        UseTraceId: true,
        Compress:   true,
    },
    rockfiber.GET("/hello", helloHandler),
    rockfiber.POST("/users", createUserHandler),
)

// Use standalone
ctx := context.Background()
if err := app.Init(ctx); err != nil {
    log.Fatal(err)
}
if err := app.Exec(ctx); err != nil {
    log.Fatal(err)
}

// Or register with rockengine
engine.MustRegister("http", app, rockengine.RestartPolicy{})
engine.Run()
```

## Loading Config from File

`rockfiber.Config` is compatible with `rockconfig.InitFromFile`. Fields that contain functions or third-party complex types are tagged `config:"-"` and must be set in code — everything else can come from a YAML or JSON file.

```yaml
# config.yaml
port: "8080"
endpoints_path_prefix: "/api/v1"
admin_password: "secret"
shutdown_timeout: "30s"
use_trace_id: true
compress: true
helmet: true
request_timeout: "5s"
tls_cert_file: "/etc/ssl/cert.pem"  # optional
tls_key_file:  "/etc/ssl/key.pem"   # optional
mask_internal_server_error_message: "internal server error"
```

```go
cfg, err := rockconfig.InitFromFile[rockfiber.Config]("config.yaml")
if err != nil {
    log.Fatal(err)
}

// Fields that must be set in code
cfg.OnRequest = func(path, traceId string) { ... }
cfg.OnError   = func(traceId string, err *rockfiber.ErrorResponse) { ... }
cfg.CorsConfig = &cors.Config{AllowOrigins: "https://myapp.com"}
cfg.RateLimit  = &limiter.Config{Max: 100, Expiration: time.Minute}

app := rockfiber.New(*cfg,
    rockfiber.GET("/hello", helloHandler),
    rockfiber.POST("/users", createUserHandler),
)
```

| From file | Code only (`config:"-"`) |
|-----------|--------------------------|
| `port`, `admin_password` | `App fiber.Config` |
| `endpoints_path_prefix`, `admin_endpoints_path` | `RateLimit`, `CorsConfig` |
| `use_trace_id`, `compress`, `helmet` | `Swagger`, `MonitoringConfig` |
| `request_timeout`, `shutdown_timeout` | `NotFound`, `OnRequest`, `OnError` |
| `tls_cert_file`, `tls_key_file` | |
| `mask_internal_server_error_message` | |

## Config

```go
type Config struct {
    App  fiber.Config  // underlying fiber config (body limit, prefork, etc.)
    Port string        // listening port, e.g. "8080"

    EndpointsPathPrefix string        // common prefix for all endpoints, e.g. "/api/v1"
    AdminEndpointsPath  string        // admin route prefix; defaults to "/admin"
    AdminPassword       string        // X-Admin-Password header value; empty = deny all
    ShutdownTimeout     time.Duration // graceful shutdown timeout; defaults to 30s

    // TLS — both must be set together.
    TLSCertFile string
    TLSKeyFile  string

    // Global middlewares (all routes including /status and admin).
    Compress bool // gzip/deflate/brotli response compression
    Helmet   bool // security headers: X-Frame-Options, X-Content-Type-Options, HSTS, etc.

    // Endpoint middlewares (routes under EndpointsPathPrefix only).
    UseTraceId     bool
    RequestTimeout time.Duration   // sets context deadline; 0 = disabled
    RateLimit      *limiter.Config // nil = disabled

    Swagger          *SwaggerConfig
    MonitoringConfig *monitor.Config
    CorsConfig       *cors.Config   // nil = allow all origins (see CORS section)

    MaskInternalServerErrorMessage string       // masks 500 messages; see Error Handling
    NotFound                       fiber.Handler // catch-all for unmatched routes

    OnRequest func(path, traceId string)
    OnError   func(traceId string, err *ErrorResponse)
}
```

## Endpoints

### Method helpers

```go
rockfiber.GET(path, handlers...)
rockfiber.POST(path, handlers...)
rockfiber.PUT(path, handlers...)
rockfiber.PATCH(path, handlers...)
rockfiber.DELETE(path, handlers...)
rockfiber.HEAD(path, handlers...)
rockfiber.OPTIONS(path, handlers...)
```

Pass middleware handlers before the final handler:

```go
rockfiber.GET("/profile", authMiddleware, getProfileHandler)
```

### Custom method

```go
rockfiber.NewEndpoint("PURGE", "/cache", purgeHandler)
```

### Custom endpoint type

Implement the `FiberEndpoint` interface to build your own endpoint descriptors:

```go
type FiberEndpoint interface {
    GetPath() string
    GetMethod() string
    GetHandlers() []fiber.Handler
}
```

## Request Parsing

### DefaultHandler

`DefaultHandler` is a generic middleware that parses the request, validates it, and stores the result in `ctx.Locals`:

```go
type CreateUserParams struct {
    Name  string `json:"name"`
    Email string `json:"email" query:"email"`
}

func (p *CreateUserParams) Validate(ctx *fiber.Ctx) error {
    if p.Name == "" {
        return rockfiber.NewError(http.StatusBadRequest, "name is required")
    }
    return nil
}

rockfiber.POST("/users",
    rockfiber.DefaultHandler[CreateUserParams](),
    createUserHandler,
)

func createUserHandler(ctx *fiber.Ctx) error {
    params, err := rockfiber.GetFromContext[CreateUserParams](ctx, "params")
    // ...
}
```

Use a custom key when multiple `DefaultHandler` calls are chained:

```go
rockfiber.DefaultHandler[CreateUserParams]("user")
rockfiber.GetFromContext[CreateUserParams](ctx, "user")
```

### ParseRequest

`ParseRequest` populates any struct from the incoming request. Supported tags:

| Tag              | Source                          |
|------------------|---------------------------------|
| `query:"name"`   | Query parameter                 |
| `json:"name"`    | JSON / XML / form body          |
| `reqHeader:"name"` | Request header                |
| `params:"name"`  | URL path parameter              |
| `form:"name"`    | Multipart file field            |

Parsing order: **query → body → headers → path params**. Path params are applied last and overwrite earlier values for the same field.

### Multipart file upload

```go
type UploadParams struct {
    Title    string                    `json:"title"`
    File     *multipart.FileHeader     `form:"file"`    // single file
    Gallery  []*multipart.FileHeader   `form:"gallery"` // multiple files
}
```

### Params constraint

`Params[T]` is the interface your param struct must satisfy to be used with `DefaultHandler`:

```go
type Params[T any] interface {
    *T
    Validate(ctx *fiber.Ctx) error
}
```

## Error Handling

### ErrorResponse

All errors are returned as JSON:

```json
{
  "code": 404,
  "status": "Not Found",
  "message": "user not found",
  "source": "user-service",
  "action": "get"
}
```

`source` and `action` are omitted from JSON when empty.

```go
// Simple error
return rockfiber.NewError(http.StatusNotFound, "user not found")

// With context
return rockfiber.NewError(http.StatusInternalServerError, "query failed").
    WithSource("database").
    WithAction("get-user")

// Return from handler — fiber routes it through ErrorHandler automatically
return rockfiber.NewError(http.StatusUnauthorized, "token expired")
```

### Masking internal errors

Set `MaskInternalServerErrorMessage` to hide raw 500 error details from clients:

```go
Config{
    MaskInternalServerErrorMessage: "internal server error",
}
```

The real error message is visible when `?debug=` is added to the request URL — useful for investigating issues in any environment without redeployment.

### Custom error handler

If `cfg.App.ErrorHandler` is set, it takes full priority and our handler is not registered:

```go
Config{
    App: fiber.Config{
        ErrorHandler: myCustomHandler,
    },
}
```

### Error logging hook

```go
Config{
    OnError: func(traceId string, err *rockfiber.ErrorResponse) {
        log.Error("request failed",
            rocklog.Str("trace_id", traceId),
            rocklog.Int("code", err.Code),
            rocklog.Str("message", err.Message),
        )
    },
}
```

## Tracing

When `UseTraceId: true`, every request under `EndpointsPathPrefix` gets an `X-Trace-Id` header:
- If the client sends `X-Trace-Id`, it is propagated (sanitised: max 64 chars, no control characters).
- Otherwise a new UUID v4 is generated.

The trace ID is available in handlers via `ctx.Locals`:

```go
traceId, _ := ctx.Locals(rockfiber.TraceIdKey).(string)
```

And in response headers automatically.

## Request Logging Hook

```go
Config{
    UseTraceId: true,
    OnRequest: func(path, traceId string) {
        log.Info("request received",
            rocklog.Str("path", path),
            rocklog.Str("trace_id", traceId),
        )
    },
}
```

`OnRequest` runs after `TraceIdMiddleware` so the trace ID is always available.

## Rate Limiting

```go
Config{
    RateLimit: &limiter.Config{
        Max:        100,
        Expiration: 1 * time.Minute,
    },
}
```

Rate limiting is applied only to routes under `EndpointsPathPrefix`. Admin routes and `/status` are not affected.

## Request Timeout

```go
Config{
    RequestTimeout: 5 * time.Second,
}
```

Sets a `context.WithTimeout` deadline on every request. Any context-aware operation (database queries, HTTP calls) will respect this deadline automatically. The handler itself is not forcefully terminated — code that ignores the context will still run to completion.

## TLS / HTTPS

```go
Config{
    Port:        "443",
    TLSCertFile: "/etc/ssl/cert.pem",
    TLSKeyFile:  "/etc/ssl/key.pem",
}
```

Both fields must be set together. Omitting one returns an error from `Init`.

## Compression

```go
Config{Compress: true}
```

Enables gzip / deflate / brotli based on the `Accept-Encoding` request header. Applied globally to all routes. The middleware auto-detects content type and skips already-compressed formats.

## Security Headers (Helmet)

```go
Config{Helmet: true}
```

Adds: `X-XSS-Protection`, `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, `Content-Security-Policy`, `Permissions-Policy`, `Strict-Transport-Security`.

> **Note:** The default CSP may conflict with the Swagger UI. If you use both `Helmet: true` and `Swagger`, configure `cfg.App` with a custom Helmet config instead.

## Admin Routes

Registered under `AdminEndpointsPath` (default `/admin`), protected by `X-Admin-Password` header:

| Route             | Description                  |
|-------------------|------------------------------|
| `GET /admin/metrics` | fiber monitor dashboard   |
| `GET /admin/debug/pprof/*` | Go pprof endpoints  |

If `AdminPassword` is empty, all admin requests return 401.

## Built-in Routes

| Route      | Description               |
|------------|---------------------------|
| `GET /status` | Returns `{"status":"ok"}` |

## CORS

```go
// Allow all origins (default — safe for public APIs without credentials)
Config{CorsConfig: nil}

// Restrict origins
Config{
    CorsConfig: &cors.Config{
        AllowOrigins: "https://app.example.com",
        AllowHeaders: "Content-Type, Authorization",
    },
}
```

> **Warning:** The default CORS policy (`nil`) allows all origins (`*`). If your API uses cookies or `Authorization` headers with browser clients, always set `CorsConfig` explicitly.

## Context Helpers

```go
// Store any value in request locals
rockfiber.HandlerInitInCtx[MyStruct]("my-key")

// Retrieve it downstream (type-safe)
val, err := rockfiber.GetFromContext[MyStruct](ctx, "my-key")
```

## Panic Recovery

`RecoverHandler` is registered globally. On panic it:
1. Writes the stack trace to `stderr`
2. Returns a 500 `ErrorResponse` to the client (routed through `ErrorHandler`)

## Limitations

- `RequestTimeout` sets a context deadline but does not forcefully kill goroutines. Handlers that ignore `ctx.Done()` complete normally.
- `Helmet: true` with `Swagger` may conflict on `Content-Security-Policy`.
- `EndpointsPathPrefix` should not overlap with `/status` or `AdminEndpointsPath`.
- `?debug=` reveals the original error message when `MaskInternalServerErrorMessage` is set. Restrict this at the gateway level if needed in production.
