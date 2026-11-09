package rockhttp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/arzab/gorock-kit/rockhandlers"
)

// Config is file-loadable with rockconfig.
type Config struct {
	Port            string        // required, e.g. "8080"
	Prefix          string        `config:",omitempty"`              // e.g. "/api/v1"
	ShutdownTimeout time.Duration `config:",default=30s"`            // graceful shutdown bound
	ReadTimeout     time.Duration `config:",default=15s"`            // whole request read
	WriteTimeout    time.Duration `config:",omitempty"`              // 0 = none (long downloads)
	MaxBodyBytes    int64         `config:",default=10485760"`       // 10 MiB
	InternalMessage string        `config:",default=internal error"` // client text for Internal errors

	// Code only.

	// OnError is called when writing a response fails after the chain
	// succeeded (headers may be sent already). Chain errors, including
	// decoding, go through rockhandlers middlewares — log them there.
	OnError func(r *http.Request, route string, err error) `config:"-"`
	// Middleware wraps the router at the net/http level: CORS, gzip, ...
	Middleware []func(http.Handler) http.Handler `config:"-"`
}

// App is an HTTP server serving rockhandlers routes. It implements
// rockengine.App.
type App struct {
	cfg       Config
	endpoints []Endpoint
	mws       []rockhandlers.Middleware

	mu      sync.Mutex
	server  *http.Server
	handler http.Handler
}

// New creates the app. Register it with the engine; it listens on Exec.
func New(cfg Config, endpoints ...Endpoint) *App {
	return &App{cfg: cfg, endpoints: endpoints}
}

// Use adds rockhandlers middlewares around every endpoint (outermost first).
// Call it before Init.
func (a *App) Use(mws ...rockhandlers.Middleware) *App {
	a.mws = append(a.mws, mws...)
	return a
}

// Init builds the router. Configuration errors (bad or duplicate endpoints)
// fail here, not at Exec.
func (a *App) Init(context.Context) (err error) {
	if a.cfg.Port == "" {
		return errors.New("rockhttp: Port must not be empty")
	}
	mux := http.NewServeMux()
	defer func() { // ServeMux and Route.With panic on bad input
		if r := recover(); r != nil {
			err = fmt.Errorf("rockhttp: %v", r)
		}
	}()
	prefix := "/" + strings.Trim(a.cfg.Prefix, "/")
	if prefix == "/" {
		prefix = ""
	}
	for i, ep := range a.endpoints {
		if ep.Method == "" || ep.Path == "" || ep.Handler.ParamsType() == nil {
			return fmt.Errorf("rockhttp: endpoint %d (%s %s): Method, Path and Handler are required", i, ep.Method, ep.Path)
		}
		route := ep.Handler.With(a.mws...)
		status := ep.Status
		mux.HandleFunc(ep.Method+" "+prefix+ep.Path, func(w http.ResponseWriter, r *http.Request) {
			a.serve(w, r, route, status)
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

// serve: run the chain (binding is its fill link) → write the result or the error.
func (a *App) serve(w http.ResponseWriter, r *http.Request, route rockhandlers.Route, status int) {
	ctx := withExchange(r.Context(), r)
	defer func() { // the chain never panics; this guards writing the response
		if v := recover(); v != nil {
			err := &rockhandlers.Error{Kind: rockhandlers.KindInternal, Err: &rockhandlers.PanicError{Value: v, Stack: debug.Stack()}}
			a.report(r, route.Name(), err)
		}
	}()

	maxBody := a.maxBody()
	res, err := route.Serve(ctx, func(params any) error { return bind(r, params, maxBody) })
	if err != nil {
		a.writeError(ctx, w, err)
		return
	}
	if err := writeResult(ctx, w, res, status); err != nil {
		a.report(r, route.Name(), err) // headers may be sent already: only report
	}
}

func (a *App) report(r *http.Request, route string, err error) {
	if a.cfg.OnError != nil {
		a.cfg.OnError(r, route, err)
	}
}

func (a *App) maxBody() int64 {
	if a.cfg.MaxBodyBytes > 0 {
		return a.cfg.MaxBodyBytes
	}
	return 10 << 20
}

func (a *App) internalMessage() string {
	if a.cfg.InternalMessage != "" {
		return a.cfg.InternalMessage
	}
	return "internal error"
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
