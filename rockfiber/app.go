package rockfiber

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/helmet"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/gofiber/fiber/v2/middleware/monitor"
	"github.com/gofiber/swagger"
)

// SwaggerConfig configures the Swagger UI endpoint.
type SwaggerConfig struct {
	Path            string
	Config          swagger.Config
	OAuth           *swagger.OAuthConfig
	Filter          swagger.FilterConfig
	SyntaxHighlight *swagger.SyntaxHighlightConfig
}

// Config holds all configuration for a FiberApp.
//
// Compatible with rockconfig.InitFromFile — fields marked config:"-" cannot be
// loaded from a file and must be set in code. All other fields are optional
// (omitempty) except Port which is required.
//
// Example config.yaml:
//
//	port: "8080"
//	endpoints_path_prefix: "/api/v1"
//	admin_password: "secret"
//	use_trace_id: true
//	compress: true
//	helmet: true
//	request_timeout: "5s"
//	mask_internal_server_error_message: "internal server error"
type Config struct {
	// Must be set in code — contains functions and types unsupported by rockconfig.
	App fiber.Config `config:"-"`

	Port string // required

	EndpointsPathPrefix string        `config:",omitempty"` // e.g. "/api/v1"
	AdminEndpointsPath  string        `config:",omitempty"` // defaults to "/admin"
	AdminPassword       string        `config:",omitempty"` // X-Admin-Password header; empty = deny all
	ShutdownTimeout     time.Duration `config:",omitempty"` // defaults to 30s

	// TLS — both must be set together to enable HTTPS.
	TLSCertFile string `config:",omitempty"`
	TLSKeyFile  string `config:",omitempty"`

	// Global middlewares (all routes including /status and admin).
	Compress bool `config:",omitempty"` // gzip/deflate/brotli compression
	Helmet   bool `config:",omitempty"` // security headers: X-Frame-Options, CSP, HSTS, etc.

	// Endpoint middlewares (routes under EndpointsPathPrefix only).
	UseTraceId     bool          `config:",omitempty"`
	RequestTimeout time.Duration `config:",omitempty"` // context deadline; 0 = disabled

	// Must be set in code — complex third-party config types.
	RateLimit        *limiter.Config `config:"-"` // nil = disabled
	Swagger          *SwaggerConfig  `config:"-"` // nil = disabled
	MonitoringConfig *monitor.Config `config:"-"` // nil = default
	CorsConfig       *cors.Config    `config:"-"` // nil = allow all origins

	// MaskInternalServerErrorMessage replaces raw 500 messages in responses.
	// The real message is still visible via ?debug= query param.
	MaskInternalServerErrorMessage string `config:",omitempty"`

	// Must be set in code — function types.
	NotFound  fiber.Handler                            `config:"-"` // catch-all for unmatched routes
	OnRequest func(path, traceId string)               `config:"-"` // called at the start of every request
	OnError   func(traceId string, err *ErrorResponse) `config:"-"` // called on error responses
}

// FiberApp is a fiber-based HTTP server that satisfies the rockengine App interface:
//
//	Init(ctx context.Context) error
//	Exec(ctx context.Context) error
//	Stop() []error
type FiberApp struct {
	cfg       Config
	endpoints []FiberEndpoint
	fiber     *fiber.App
	stopOnce  sync.Once
}

// New creates a FiberApp with the given config and endpoints.
// Pass endpoints via NewEndpoint, GET, POST, etc.
func New(cfg Config, endpoints ...FiberEndpoint) *FiberApp {
	return &FiberApp{cfg: cfg, endpoints: endpoints}
}

// Init sets up the fiber instance and registers global middlewares.
func (a *FiberApp) Init(_ context.Context) error {
	if a.cfg.Port == "" {
		return fmt.Errorf("rockfiber: Port must not be empty")
	}
	if (a.cfg.TLSCertFile == "") != (a.cfg.TLSKeyFile == "") {
		return fmt.Errorf("rockfiber: TLSCertFile and TLSKeyFile must both be set or both empty")
	}

	adminPath := a.cfg.AdminEndpointsPath
	if adminPath == "" {
		adminPath = "/admin"
	} else {
		adminPath = "/" + strings.Trim(adminPath, "/")
	}
	a.cfg.AdminEndpointsPath = adminPath

	fiberCfg := a.cfg.App
	// Only set our error handler if the user has not provided a custom one.
	if fiberCfg.ErrorHandler == nil {
		fiberCfg.ErrorHandler = ErrorHandler(a.cfg.MaskInternalServerErrorMessage, a.cfg.OnError)
	}

	a.fiber = fiber.New(fiberCfg)

	// Global middlewares — applied to every route including admin and /status.
	a.fiber.Use(RecoverHandler())
	a.fiber.Use(PprofHandler(adminPath))
	a.fiber.Use(CorsHandler(a.cfg.CorsConfig))

	if a.cfg.Compress {
		a.fiber.Use(compress.New())
	}
	if a.cfg.Helmet {
		a.fiber.Use(helmet.New())
	}

	return nil
}

// Exec registers endpoints and starts the HTTP server.
// Blocks until ctx is cancelled or the server exits with an error.
func (a *FiberApp) Exec(ctx context.Context) error {
	if a.fiber == nil {
		return fmt.Errorf("rockfiber: Init must be called before Exec")
	}
	a.setupEndpoints()

	listenErr := make(chan error, 1)
	go func() {
		addr := fmt.Sprintf(":%s", a.cfg.Port)
		if a.cfg.TLSCertFile != "" && a.cfg.TLSKeyFile != "" {
			listenErr <- a.fiber.ListenTLS(addr, a.cfg.TLSCertFile, a.cfg.TLSKeyFile)
		} else {
			listenErr <- a.fiber.Listen(addr)
		}
	}()

	select {
	case <-ctx.Done():
		return a.shutdown()
	case err := <-listenErr:
		return err
	}
}

// Stop gracefully shuts down the HTTP server.
// Safe to call multiple times — only the first call takes effect.
func (a *FiberApp) Stop() []error {
	if err := a.shutdown(); err != nil {
		return []error{err}
	}
	return nil
}

func (a *FiberApp) shutdown() error {
	if a.fiber == nil {
		// Init was never called or failed — nothing to shut down.
		return nil
	}
	var err error
	a.stopOnce.Do(func() {
		timeout := a.cfg.ShutdownTimeout
		if timeout == 0 {
			timeout = 30 * time.Second
		}
		if e := a.fiber.ShutdownWithTimeout(timeout); e != nil {
			err = fmt.Errorf("fiber shutdown: %w", e)
		}
	})
	return err
}

func (a *FiberApp) setupEndpoints() {
	if sw := a.cfg.Swagger; sw != nil {
		swPath := sw.Path
		if swPath != "" && swPath[0] != '/' {
			swPath = "/" + swPath
		}
		swCfg := sw.Config
		swCfg.OAuth = sw.OAuth
		swCfg.Filter = sw.Filter
		swCfg.SyntaxHighlight = sw.SyntaxHighlight
		a.fiber.Get(swPath, swagger.HandlerDefault)
		a.fiber.Get(swPath+"/*", swagger.New(swCfg))
	}

	admin := a.fiber.Group(a.cfg.AdminEndpointsPath)
	admin.Use(AdminAuthMiddleware(a.cfg.AdminPassword))
	admin.Get("/metrics", MetricsHandler(a.cfg.MonitoringConfig))

	a.fiber.Get("/status", func(c *fiber.Ctx) error {
		return c.JSON(map[string]interface{}{"status": "ok"})
	})

	prefix := a.cfg.EndpointsPathPrefix
	if prefix == "" {
		prefix = "/"
	}
	router := a.fiber.Group(prefix)

	// Endpoint-scoped middlewares — applied only to routes under EndpointsPathPrefix.
	if a.cfg.UseTraceId {
		router.Use(TraceIdMiddleware())
	}
	if a.cfg.RequestTimeout > 0 {
		router.Use(RequestTimeoutMiddleware(a.cfg.RequestTimeout))
	}
	if a.cfg.RateLimit != nil {
		router.Use(limiter.New(*a.cfg.RateLimit))
	}
	if a.cfg.OnRequest != nil {
		router.Use(RequestLogMiddleware(a.cfg.OnRequest))
	}

	for _, ep := range a.endpoints {
		registerEndpoint(router, ep)
	}

	// Catch-all for unmatched routes — must be registered last.
	if a.cfg.NotFound != nil {
		a.fiber.Use(a.cfg.NotFound)
	}
}

func registerEndpoint(router fiber.Router, ep FiberEndpoint) {
	path := ep.GetPath()
	handlers := ep.GetHandlers()
	switch strings.ToUpper(ep.GetMethod()) {
	case "GET":
		router.Get(path, handlers...)
	case "HEAD":
		router.Head(path, handlers...)
	case "POST":
		router.Post(path, handlers...)
	case "PUT":
		router.Put(path, handlers...)
	case "PATCH":
		router.Patch(path, handlers...)
	case "DELETE":
		router.Delete(path, handlers...)
	case "CONNECT":
		router.Connect(path, handlers...)
	case "OPTIONS":
		router.Options(path, handlers...)
	case "TRACE":
		router.Trace(path, handlers...)
	default:
		router.All(path, handlers...)
	}
}
