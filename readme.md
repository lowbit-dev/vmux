# vmux

A versioned HTTP multiplexer for Go.

`vmux` is a routing primitive for managing API versions, route inheritance, and lifecycle transitions (deprecation and sunsetting). It is built strictly on top of Go 1.22's native `http.ServeMux`. No external dependencies, no request-path allocations, and no hidden state.

Write the routes that changed. Inherit the ones that didn't.

## Features

* **Zero-Allocation Hot Path:** Prefix matching and inheritance cascading happen via pre-computed map lookups and zero-allocation string slicing.

* **Automatic Route Inheritance:** If `v2` does not define an endpoint, `vmux` automatically cascades down to `v1`. You only write code for endpoints that actually change. (Can be disabled via `WithInheritance(false)`).

* **RFC-Compliant Lifecycle Management:** Enforces RFC 9745 (`Deprecation`) and RFC 8594 (`Sunset`) headers automatically based on version milestones.

* **Composability:** Implements `http.Handler`. Mount it anywhere, or use it as your root router.

* **Middleware & Grouping:** Powered internally by `lowbit.dev/muxx` for zero-overhead route grouping and middleware chaining.

## Installation

```bash
go get lowbit.dev/vmux

```

## Quick Start

Declare your supported versions and their lifecycles at startup, then register your handlers.

```go
package main

import (
	"fmt"
	"net/http"
	"time"

	"lowbit.dev/vmux"
)

func main() {
	router := vmux.New(
		// v1 is deprecated and will be removed in 2027
		vmux.WithVersion(1, vmux.Lifecycle{
			DeprecatedAt:    time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			SunsetAt:        time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
			DeprecationLink: "[https://api.example.com/docs/v2](https://api.example.com/docs/v2)",
		}),

		// v2 is the current active version
		vmux.WithVersion(2),
		
		// Optional: Disable automatic route cascading (default: true)
		vmux.WithInheritance(true),

		// Optional: Define a custom 404 Not Found handler
		vmux.WithNotFoundHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"error": "route or version not found"}`, http.StatusNotFound)
		})),

		// Optional: Custom handler for sunset/deprecated endpoints (e.g., returning JSON)
		vmux.WithDeprecatedHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusGone)
			w.Write([]byte(`{"type": "api_sunset", "title": "API version is no longer supported"}`))
		})),
	)

	// --- v1 Routes ---
	v1 := router.Version(1)
	v1.HandleFunc("GET /users", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Legacy users list")
	})
	
	// Go 1.22 wildcard path
	v1.HandleFunc("GET /users/{id}", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "User %s", r.PathValue("id"))
	})

	// --- v2 Routes ---
	v2 := router.Version(2)
	
	// Override the list endpoint for v2
	v2.HandleFunc("GET /users", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Modern users list")
	})
	// GET /users/{id} is not declared here. It automatically inherits from v1.

	// Shorthand
	router.V(2).HandleFunc("POST /users", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	http.ListenAndServe(":8080", router)
}

```

## Route Inheritance

The defining feature of `vmux` is how it handles missing routes. A full API version bump rarely means every single endpoint changed.

When a client requests `GET /v2/users/42`, `vmux` will:

1. Check if `v2` has an explicit handler for `GET /users/{id}`.
2. If not, cascade down to `v1`.
3. Execute the `v1` handler, but **retain the `v2` lifecycle context**.

If `v1` is deprecated but `v2` is active, requesting the inherited route via `/v2/` will *not* emit deprecation headers, and will continue to work even after `v1` reaches its Sunset (EOL) date.

*Note: If you prefer strict, explicit routing where every version must define all of its endpoints, you can disable this behavior globally using `vmux.WithInheritance(false)`.*

You can inspect exactly how a request was resolved using context helpers:

```go
func handleUser(w http.ResponseWriter, r *http.Request) {
	reqVer := vmux.Version(r.Context())       // e.g., 2
	execVer := vmux.TargetVersion(r.Context()) // e.g., 1 (because it inherited)
}

```

## Lifecycle Management

`vmux` prevents you from managing HTTP dates and deprecation states inside your handler logic. Lifecycles are declared once during configuration.

| Lifecycle Milestone | Condition                          | Router Behavior                                                                 |
| ------------------- | ---------------------------------- | ------------------------------------------------------------------------------- |
| `AvailableFrom`     | Request arrives *before* this date | Returns `404 Not Found`. (Feature flag / embargoed API).                        |
| `DeprecatedAt`      | Request arrives *after* this date  | Executes target handler, injecting RFC 9745 `Deprecation: @<timestamp>` header. |
| `SunsetAt`          | Request arrives *after* this date  | Short-circuits. Standard handler is **never executed**.                         |

**Customizing Lifecycle Responses:** By default, hitting a sunset (EOL) route short-circuits the request and returns a standard plain-text `410 Gone`. Using `WithDeprecatedHandler`, you can set a custom response body (like RFC 9457 Problem JSON). `vmux` will still automatically populate the correct `Sunset` and `Link` headers before handing control over to your custom handler.

## Grouping and Middleware

Because `vmux.Registrar` is backed by `lowbit.dev/muxx`, you can safely group routes and chain middleware without exposing the underlying `http.Handler`.

```go
v1 := router.Version(1)

// Apply middleware to a group
admin := v1.Group("/admin").Use(requireAuth, requireAdmin)
admin.HandleFunc("POST /ban", banUser)

// Sub-groups
billing := admin.Group("/billing")
billing.HandleFunc("GET /invoices", getInvoices)

```

## License

MIT - Vendor it, read it, own it.
