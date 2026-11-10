package vmux

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"lowbit.dev/muxx"
)

// Lifecycle defines temporal boundaries and documentation links for an API version.
type Lifecycle struct {
	AvailableFrom   time.Time // Callable start date (zero = immediate)
	DeprecatedAt    time.Time // Deprecation milestone (RFC 9745)
	SunsetAt        time.Time // Hard cutoff / EOL (RFC 8594)
	DeprecationLink string    // Migration guide URI (rel="deprecation")
	SunsetLink      string    // Sunset policy URI (rel="sunset")
}

type versionEntry struct {
	version               uint
	serveMux              *http.ServeMux
	router                *muxx.Router
	lifecycle             Lifecycle
	deprecationHeader     string
	sunsetHeader          string
	deprecationLinkHeader string
	sunsetLinkHeader      string
}

// Mux routes versioned requests to internal routers and cascades
// missing endpoints down to earlier registered versions.
type Mux struct {
	prefix          string
	versions        map[uint]*versionEntry
	byPrefix        map[string]*versionEntry
	orderedVersions []*versionEntry

	notfoundHandler   http.Handler
	deprecatedHandler http.Handler
	inheritance       bool
	now               func() time.Time
}

// New creates a versioned multiplexer with the declared versions.
func New(opts ...Option) *Mux {
	m := &Mux{
		prefix:            "v",
		versions:          make(map[uint]*versionEntry),
		byPrefix:          make(map[string]*versionEntry),
		notfoundHandler:   http.HandlerFunc(http.NotFound),
		deprecatedHandler: http.HandlerFunc(deprecatedHandler),
		inheritance:       true,
		now:               time.Now,
	}

	for _, opt := range opts {
		opt(m)
	}

	if len(m.versions) == 0 {
		panic("vmux: at least one version must be configured using WithVersion")
	}

	// Post-options index build: order-independent prefix mapping and sort
	for version, entry := range m.versions {
		prefixKey := m.prefix + strconv.FormatUint(uint64(version), 10)
		m.byPrefix[prefixKey] = entry
		m.orderedVersions = append(m.orderedVersions, entry)
	}

	slices.SortFunc(m.orderedVersions, func(a, b *versionEntry) int {
		if a.version < b.version {
			return 1
		}
		if a.version > b.version {
			return -1
		}
		return 0
	})

	return m
}

// Version returns a Registrar scoped strictly to the specified version.
// Panics immediately if the version was not configured during New().
func (m *Mux) Version(version uint) Registrar {
	entry, exists := m.versions[version]
	if !exists {
		panic(fmt.Sprintf("vmux: version %d is not configured", version))
	}

	return Registrar{router: entry.router}
}

// V is an alias for Version, providing a concise syntax for inline route chaining.
func (m *Mux) V(version uint) Registrar {
	return m.Version(version)
}

func (m *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	segment, remainder, ok := splitFirstSegment(r.URL.Path)
	if !ok {
		m.notfoundHandler.ServeHTTP(w, r)
		return
	}

	entry, exists := m.byPrefix[segment]
	if !exists {
		m.notfoundHandler.ServeHTTP(w, r)
		return
	}

	now := m.now()
	lc := entry.lifecycle

	// EOL / Sunset Check (RFC 8594) -> 410 Gone
	if !lc.SunsetAt.IsZero() && !now.Before(lc.SunsetAt) {
		w.Header().Set("Sunset", entry.sunsetHeader)
		if entry.sunsetLinkHeader != "" {
			w.Header().Set("Link", entry.sunsetLinkHeader)
		}

		m.deprecatedHandler.ServeHTTP(w, r)
		return
	}

	// Pending Activation Check -> 404 Not Found
	if !lc.AvailableFrom.IsZero() && now.Before(lc.AvailableFrom) {
		m.notfoundHandler.ServeHTTP(w, r)
		return
	}

	// RFC 9745 Deprecation Header
	if !lc.DeprecatedAt.IsZero() && !now.Before(lc.DeprecatedAt) {
		w.Header().Set("Deprecation", entry.deprecationHeader)
		if entry.deprecationLinkHeader != "" {
			w.Header().Add("Link", entry.deprecationLinkHeader)
		}
	}

	// RFC 8594 Sunset Header
	if !lc.SunsetAt.IsZero() {
		w.Header().Set("Sunset", entry.sunsetHeader)
		if entry.sunsetLinkHeader != "" {
			w.Header().Add("Link", entry.sunsetLinkHeader)
		}
	}

	// Build sub-request targeting remainder path
	ctx := context.WithValue(r.Context(), contextKeyVersion, entry.version)
	subReq := r.Clone(ctx)
	subReq.URL.Path = remainder
	if subReq.URL.RawPath != "" {
		subReq.URL.RawPath = remainder
	}

	if !m.inheritance {
		m.notfoundHandler.ServeHTTP(w, r)
		return
	}

	// Inheritance Walk: Search descending from requested version down to lowest
	for _, candidate := range m.orderedVersions {
		if candidate.version > entry.version {
			continue
		}

		_, pattern := candidate.serveMux.Handler(subReq)
		if pattern == "" {
			continue
		}

		targetCtx := context.WithValue(subReq.Context(), contextKeyTargetVersion, candidate.version)
		candidate.serveMux.ServeHTTP(w, subReq.WithContext(targetCtx))
		return
	}

	m.notfoundHandler.ServeHTTP(w, r)
}

func splitFirstSegment(path string) (segment, remainder string, ok bool) {
	if len(path) == 0 || path[0] != '/' {
		return "", "", false
	}

	p := path[1:]
	slash := strings.IndexByte(p, '/')
	if slash == -1 {
		return p, "/", true
	}

	return p[:slash], p[slash:], true
}

func precomputeDeprecation(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return "@" + strconv.FormatInt(t.Unix(), 10)
}

func precomputeSunset(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(http.TimeFormat)
}

func precomputeLink(uri, rel string) string {
	if uri == "" {
		return ""
	}
	return fmt.Sprintf("<%s>; rel=\"%s\"", uri, rel)
}

func deprecatedHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusGone)
	w.Write([]byte("API version has been sunset and is no longer available\n"))
}

// ---------------------------------------------------------------------
//
// ---------------------------------------------------------------------

type contextKey struct{ name string }

var (
	contextKeyVersion       = &contextKey{"vmux.version"}
	contextKeyTargetVersion = &contextKey{"vmux.target_version"}
)

// Version returns the API version requested by the client (0 if unversioned).
func Version(ctx context.Context) uint {
	if v, ok := ctx.Value(contextKeyVersion).(uint); ok {
		return v
	}
	return 0
}

// TargetVersion returns the version whose handler resolved the route.
func TargetVersion(ctx context.Context) uint {
	if v, ok := ctx.Value(contextKeyTargetVersion).(uint); ok {
		return v
	}
	return 0
}
