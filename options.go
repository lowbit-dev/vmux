package vmux

import (
	"net/http"
	"time"

	"lowbit.dev/muxx"
)

type Option func(*Mux)

// WithPrefix sets the path prefix preceding version numbers. Defaults to "v" (e.g. "/v1/...").
func WithPrefix(prefix string) Option {
	return func(m *Mux) {
		m.prefix = prefix
	}
}

// WithVersion registers an API version and its optional lifecycle metadata.
func WithVersion(version uint, lifecycle ...Lifecycle) Option {
	if version == 0 {
		panic("vmux: version 0 is reserved and cannot be configured")
	}

	return func(m *Mux) {
		var lc Lifecycle
		if len(lifecycle) > 0 {
			lc = lifecycle[0]
		}

		serveMux := http.NewServeMux()
		m.versions[version] = &versionEntry{
			version:               version,
			serveMux:              serveMux,
			router:                muxx.NewFromMux(serveMux),
			lifecycle:             lc,
			deprecationHeader:     precomputeDeprecation(lc.DeprecatedAt),
			sunsetHeader:          precomputeSunset(lc.SunsetAt),
			deprecationLinkHeader: precomputeLink(lc.DeprecationLink, "deprecation"),
			sunsetLinkHeader:      precomputeLink(lc.SunsetLink, "sunset"),
		}
	}
}

// WithNotfoundHandler sets the http handler which is responsible to write the
// http 404 response to the client
func WithNotFoundHandler(h http.Handler) Option {
	if h == nil {
		panic("vmux: attempting to set nil http.Handler as not found handler")
	}

	return func(m *Mux) {
		m.notfoundHandler = h
	}
}

func WithDeprecatedHandler(h http.Handler) Option {
	if h == nil {
		panic("vmux: attempting to set nil http.Handler as deprecated handler")
	}

	return func(m *Mux) {
		m.deprecatedHandler = h
	}
}

func WithInheritance(enable bool) Option {
	return func(m *Mux) {
		m.inheritance = enable
	}
}

// WithClock overrides the clock provider for deterministic testing.
func WithClock(now func() time.Time) Option {
	return func(m *Mux) {
		m.now = now
	}
}
