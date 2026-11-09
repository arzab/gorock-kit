// Package rockhttp is an HTTP transport for rockhandlers on net/http.
//
// EXPERIMENTAL: it lives in gorock-kit to validate the rockhandlers contract
// and may move to the catalog; its API may change.
//
//	server := rockhttp.New(cfg,
//	    rockhttp.Endpoint{Method: "POST", Path: "/orders", Handler: orders.Create, Status: http.StatusCreated},
//	    rockhttp.Endpoint{Method: "GET", Path: "/orders/{id}", Handler: orders.Get},
//	).Use(Log)
//	engine.MustRegister("http", server)
package rockhttp

import "github.com/arzab/gorock-kit/rockhandlers"

// Endpoint binds a rockhandlers route to an HTTP method and path.
type Endpoint struct {
	Method  string             // GET, POST, ...
	Path    string             // net/http pattern: "/orders/{id}"
	Handler rockhandlers.Route // the chain
	Status  int                // success status; default 200, or 204 for an empty result struct
}
