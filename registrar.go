package vmux

import (
	"net/http"

	"lowbit.dev/muxx"
)

// Registrar is scoped to a specific version. It exposes registration,
// prefix grouping, and middleware inheritance without exposing ServeHTTP.
type Registrar struct {
	router *muxx.Router
}

// Handle registers an http.Handler onto this version's router.
func (r Registrar) Handle(pattern string, handler http.Handler, middleware ...muxx.Middleware) {
	r.router.Handle(pattern, handler, middleware...)
}

// HandleFunc registers an http.HandlerFunc onto this version's router.
func (r Registrar) HandleFunc(pattern string, handler http.HandlerFunc, middleware ...muxx.Middleware) {
	r.router.HandleFunc(pattern, handler, middleware...)
}

// Group creates a sub-registrar with an appended path or host prefix.
func (r Registrar) Group(prefix string) Registrar {
	return Registrar{router: r.router.Group(prefix)}
}

// Use appends middleware to this registrar and returns it for chaining.
func (r Registrar) Use(middleware ...muxx.Middleware) Registrar {
	r.router.Use(middleware...)
	return r
}
