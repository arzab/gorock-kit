// Package rockhttp is an HTTP transport for rockdelivery on net/http.
//
// EXPERIMENTAL: it lives in gorock-kit to validate the rockdelivery contract
// and may move to the catalog; its API may change.
//
//	server := rockhttp.New(cfg,
//	    rockhttp.POST("/orders", orders.Create, rockhttp.Status(http.StatusCreated)),
//	    rockhttp.GET("/orders/{id}", orders.Get),
//	)
//	engine.MustRegister("http", server)
package rockhttp

import (
	"net/http"

	"github.com/arzab/gorock-kit/rockdelivery"
)

// Route metadata keys understood by this transport.
var (
	metaMethod  = rockdelivery.NewMetaKey[string]("http.method")
	metaPattern = rockdelivery.NewMetaKey[string]("http.pattern")
	metaStatus  = rockdelivery.NewMetaKey[int]("http.status")
)

// Endpoint is an HTTP route: a rockdelivery.Route with method and pattern.
type Endpoint struct {
	route rockdelivery.Route
}

// Route returns the underlying rockdelivery route.
func (e Endpoint) Route() rockdelivery.Route { return e.route }

// Option configures an endpoint.
type Option func(*options)

type options struct {
	status int
	mws    []rockdelivery.Middleware
	name   string
}

// Status sets the success status (default 200, or 204 when the handler
// returns a nil result).
func Status(code int) Option { return func(o *options) { o.status = code } }

// Use adds transport-independent middlewares to this endpoint.
func Use(mws ...rockdelivery.Middleware) Option {
	return func(o *options) { o.mws = append(o.mws, mws...) }
}

// Name overrides the route name used in failures and observations
// (default "METHOD pattern").
func Name(name string) Option { return func(o *options) { o.name = name } }

// Handle declares an endpoint. pattern uses net/http patterns: "/orders/{id}".
func Handle[In, Out any](method, pattern string, h rockdelivery.Handler[In, Out], opts ...Option) Endpoint {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	if o.name == "" {
		o.name = method + " " + pattern
	}
	ropts := []rockdelivery.RouteOption{
		rockdelivery.WithMeta(metaMethod, method),
		rockdelivery.WithMeta(metaPattern, pattern),
	}
	if o.status != 0 {
		ropts = append(ropts, rockdelivery.WithMeta(metaStatus, o.status))
	}
	return Endpoint{route: rockdelivery.NewRoute(o.name, rockdelivery.Bind(h, o.mws...), ropts...)}
}

func GET[In, Out any](pattern string, h rockdelivery.Handler[In, Out], opts ...Option) Endpoint {
	return Handle(http.MethodGet, pattern, h, opts...)
}

func POST[In, Out any](pattern string, h rockdelivery.Handler[In, Out], opts ...Option) Endpoint {
	return Handle(http.MethodPost, pattern, h, opts...)
}

func PUT[In, Out any](pattern string, h rockdelivery.Handler[In, Out], opts ...Option) Endpoint {
	return Handle(http.MethodPut, pattern, h, opts...)
}

func PATCH[In, Out any](pattern string, h rockdelivery.Handler[In, Out], opts ...Option) Endpoint {
	return Handle(http.MethodPatch, pattern, h, opts...)
}

func DELETE[In, Out any](pattern string, h rockdelivery.Handler[In, Out], opts ...Option) Endpoint {
	return Handle(http.MethodDelete, pattern, h, opts...)
}
