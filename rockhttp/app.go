package rockhttp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/arzab/gorock-kit/rockdelivery"
)

// Config is file-loadable with rockconfig.
type Config struct {
	Port            string        // required, e.g. "8080"
	Prefix          string        `config:",omitempty"`              // e.g. "/api/v1"
	ShutdownTimeout time.Duration `config:",default=30s"`            // graceful shutdown bound
	ReadTimeout     time.Duration `config:",default=15s"`            // whole request read
	WriteTimeout    time.Duration `config:",omitempty"`              // 0 = none (streaming downloads)
	MaxBodyBytes    int64         `config:",default=10485760"`       // 10 MiB
	InternalMessage string        `config:",default=internal error"` // client text for Internal errors

	// Code only.
	OnFailure  func(ctx context.Context, f rockdelivery.Failure)                              `config:"-"`
	Observe    func(ctx context.Context, r rockdelivery.Route, err error, took time.Duration) `config:"-"`
	Middleware []func(http.Handler) http.Handler                                              `config:"-"` // net/http level: CORS, compression...
}

// App is an HTTP server serving rockdelivery endpoints. It implements
// rockengine.App.
type App struct {
	cfg       Config
	endpoints []Endpoint

	mu      sync.Mutex
	server  *http.Server
	handler http.Handler
}

// New creates the app. Register it with the engine; it starts on Exec.
func New(cfg Config, endpoints ...Endpoint) *App {
	return &App{cfg: cfg, endpoints: endpoints}
}

// Init builds the router. Configuration errors (duplicate routes, bad
// patterns) fail here, not at Exec.
func (a *App) Init(context.Context) (err error) {
	if a.cfg.Port == "" {
		return errors.New("rockhttp: Port must not be empty")
	}
	maxBody := a.cfg.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = 10 << 20
	}
	opts := []rockdelivery.PipelineOption{}
	if a.cfg.InternalMessage != "" {
		opts = append(opts, rockdelivery.InternalMessage(a.cfg.InternalMessage))
	}
	if a.cfg.OnFailure != nil {
		opts = append(opts, rockdelivery.OnFailure(a.cfg.OnFailure))
	}
	if a.cfg.Observe != nil {
		opts = append(opts, rockdelivery.Observe(a.cfg.Observe))
	}
	pipe := rockdelivery.NewPipeline[*http.Request, http.ResponseWriter](codec{maxBody: maxBody}, opts...)

	mux := http.NewServeMux()
	defer func() { // ServeMux panics on conflicting patterns
		if r := recover(); r != nil {
			err = fmt.Errorf("rockhttp: %v", r)
		}
	}()
	prefix := "/" + strings.Trim(a.cfg.Prefix, "/")
	if prefix == "/" {
		prefix = ""
	}
	for _, ep := range a.endpoints {
		route := ep.route
		method, _ := rockdelivery.Meta(route, metaMethod)
		pattern, _ := rockdelivery.Meta(route, metaPattern)
		mux.HandleFunc(method+" "+prefix+pattern, func(w http.ResponseWriter, r *http.Request) {
			ctx := withExchange(r.Context(), r)
			_ = pipe.Serve(ctx, route, r, w) // the answer is written; failures go to OnFailure
		})
	}

	var h http.Handler = mux
	for i := len(a.cfg.Middleware) - 1; i >= 0; i-- {
		h = a.cfg.Middleware[i](h)
	}

	a.mu.Lock()
	a.handler = h
	a.server = &http.Server{
		Addr:         ":" + a.cfg.Port,
		Handler:      h,
		ReadTimeout:  a.cfg.ReadTimeout,
		WriteTimeout: a.cfg.WriteTimeout,
	}
	a.mu.Unlock()
	return nil
}

// Handler returns the router built by Init — handy for httptest.
func (a *App) Handler() http.Handler {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.handler
}

// Exec listens until ctx is cancelled, then shuts down gracefully.
func (a *App) Exec(ctx context.Context) error {
	a.mu.Lock()
	srv := a.server
	a.mu.Unlock()
	if srv == nil {
		return errors.New("rockhttp: Init must be called before Exec")
	}

	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return fmt.Errorf("rockhttp: listen %s: %w", srv.Addr, err)
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()

	select {
	case <-ctx.Done():
		return a.shutdown(srv)
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// Stop shuts the server down gracefully; Exec also does it on ctx
// cancellation, and Shutdown is safe to call twice.
func (a *App) Stop() []error {
	a.mu.Lock()
	srv := a.server
	a.mu.Unlock()
	if srv == nil {
		return nil
	}
	if err := a.shutdown(srv); err != nil {
		return []error{err}
	}
	return nil
}

func (a *App) shutdown(srv *http.Server) error {
	timeout := a.cfg.ShutdownTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("rockhttp: shutdown: %w", err)
	}
	return nil
}
