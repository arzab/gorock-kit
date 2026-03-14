package rockfiber

import (
	"net/http"

	"github.com/gofiber/fiber/v2"
)

// Endpoint is a generic route descriptor.
type Endpoint[HandlerFunc any] interface {
	GetPath() string
	GetMethod() string
	GetHandlers() []HandlerFunc
}

// FiberEndpoint is an Endpoint for fiber handlers.
type FiberEndpoint Endpoint[fiber.Handler]

type fiberEndpoint struct {
	method   string
	path     string
	handlers []fiber.Handler
}

func (e *fiberEndpoint) GetPath() string             { return e.path }
func (e *fiberEndpoint) GetMethod() string            { return e.method }
func (e *fiberEndpoint) GetHandlers() []fiber.Handler { return e.handlers }

// NewEndpoint creates a FiberEndpoint with the given method, path and handlers.
func NewEndpoint(method, path string, handlers ...fiber.Handler) FiberEndpoint {
	return &fiberEndpoint{method: method, path: path, handlers: handlers}
}

// HTTP method shorthands — pass middleware handlers before the final handler.

func GET(path string, handlers ...fiber.Handler) FiberEndpoint {
	return NewEndpoint(http.MethodGet, path, handlers...)
}

func POST(path string, handlers ...fiber.Handler) FiberEndpoint {
	return NewEndpoint(http.MethodPost, path, handlers...)
}

func PUT(path string, handlers ...fiber.Handler) FiberEndpoint {
	return NewEndpoint(http.MethodPut, path, handlers...)
}

func PATCH(path string, handlers ...fiber.Handler) FiberEndpoint {
	return NewEndpoint(http.MethodPatch, path, handlers...)
}

func DELETE(path string, handlers ...fiber.Handler) FiberEndpoint {
	return NewEndpoint(http.MethodDelete, path, handlers...)
}

func HEAD(path string, handlers ...fiber.Handler) FiberEndpoint {
	return NewEndpoint(http.MethodHead, path, handlers...)
}

func OPTIONS(path string, handlers ...fiber.Handler) FiberEndpoint {
	return NewEndpoint(http.MethodOptions, path, handlers...)
}
