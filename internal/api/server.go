// Package api implements the semar-api HTTP surface. Routes and response
// types are generated from api/openapi.yaml into package gen; this package
// implements gen.StrictServerInterface.
package api

import (
	"context"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/sulaimantok/semar/internal/api/gen"
	"github.com/sulaimantok/semar/internal/driver/registry"
	"github.com/sulaimantok/semar/internal/metrics"
	"github.com/sulaimantok/semar/internal/secret"
	"github.com/sulaimantok/semar/internal/version"
)

// ReadinessCheck reports whether a dependency is ready to serve traffic.
type ReadinessCheck func(ctx context.Context) error

// Options configures the handler.
type Options struct {
	Logger *slog.Logger
	// Checks are run by /readyz; any error makes the instance not ready.
	Checks map[string]ReadinessCheck
	// CheckTimeout bounds each readiness check. Defaults to 2s.
	CheckTimeout time.Duration
	// Settings backs the Settings endpoints (sites, projects, providers).
	Settings SettingsStore
	// Auth backs login and session resolution. Without it every
	// authenticated operation answers 401.
	Auth AuthStore
	// Audit receives audit events (login, logout, every Settings mutation).
	Audit AuditLog
	// SessionTTL is the session lifetime. Defaults to 12h.
	SessionTTL time.Duration
	// CookieSecure marks the session cookie Secure (HTTPS only).
	CookieSecure bool
	// Credentials and Secrets back the write-only credential endpoints.
	Credentials CredentialStore
	Secrets     secret.Store
	// MaxBodyBytes limits request bodies. Defaults to 1 MiB.
	MaxBodyBytes int64
	// LoginLimits bounds login attempts and concurrent password hashing.
	LoginLimits LoginLimits
	// Inventory backs the read-only inventory endpoints.
	Inventory InventoryReader
	// Drivers builds provider drivers for connection checks. Defaults to
	// registry.New, the same resolution the worker uses.
	Drivers DriverFactory
	// ValidateTimeout bounds one connection check. Defaults to 15s.
	ValidateTimeout time.Duration
	// ConsoleTokens stores console tokens; without it consoles are off.
	ConsoleTokens ConsoleTokenStore
	// ConsoleDrivers builds console-capable drivers. Defaults to the same
	// registry.Resolver as Drivers.
	ConsoleDrivers ConsoleDriverFactory
	// ConsoleIdleTimeout and ConsoleMaxDuration bound console sessions.
	// Defaults: 15 minutes and 4 hours.
	ConsoleIdleTimeout time.Duration
	ConsoleMaxDuration time.Duration
	// ConsolesPerUser and ConsolesTotal bound open consoles per user and
	// per process. Defaults: 5 and 200.
	ConsolesPerUser int
	ConsolesTotal   int
	// Metrics receives HTTP request metrics; nil disables them.
	Metrics *metrics.Registry
	// TrustedOrigins are the browser origins (scheme://host[:port]) allowed
	// to send state-changing requests. Empty means the request's own Host,
	// which is right unless a reverse proxy rewrites Host.
	TrustedOrigins []string
}

// NewHandler returns the root HTTP handler with middleware applied.
func NewHandler(opts Options) http.Handler {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.CheckTimeout <= 0 {
		opts.CheckTimeout = 2 * time.Second
	}
	if opts.SessionTTL <= 0 {
		opts.SessionTTL = 12 * time.Hour
	}
	if opts.MaxBodyBytes <= 0 {
		opts.MaxBodyBytes = 1 << 20
	}
	resolver := &registry.Resolver{Secrets: opts.Secrets}
	if c, ok := opts.Credentials.(registry.CredentialOpener); ok {
		resolver.Credentials = c
	}
	if opts.Drivers == nil {
		opts.Drivers = resolver.Driver
	}
	if opts.ConsoleDrivers == nil {
		opts.ConsoleDrivers = resolver.ConsoleDriver
	}
	if opts.ConsoleIdleTimeout <= 0 {
		opts.ConsoleIdleTimeout = 15 * time.Minute
	}
	if opts.ConsoleMaxDuration <= 0 {
		opts.ConsoleMaxDuration = 4 * time.Hour
	}
	if opts.ConsolesPerUser <= 0 {
		opts.ConsolesPerUser = 5
	}
	if opts.ConsolesTotal <= 0 {
		opts.ConsolesTotal = 200
	}
	if opts.ValidateTimeout <= 0 {
		opts.ValidateTimeout = 15 * time.Second
	}
	log := opts.Logger
	srv := &server{opts: opts, logins: newLoginLimiter(opts.LoginLimits), originOK: originChecker(opts.TrustedOrigins),
		consoles: newConsoleSlots(opts.ConsolesPerUser, opts.ConsolesTotal)}

	strict := gen.NewStrictHandlerWithOptions(srv, []gen.StrictMiddlewareFunc{srv.authorize}, gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			writeProblem(w, r, http.StatusBadRequest, err.Error())
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			log.ErrorContext(r.Context(), "handler error",
				slog.Any("error", err), slog.String("request_id", RequestIDFrom(r.Context())))
			writeProblem(w, r, http.StatusInternalServerError, "")
		},
	})

	mux := http.NewServeMux()
	gen.HandlerWithOptions(strict, gen.StdHTTPServerOptions{
		BaseRouter: mux,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			writeProblem(w, r, http.StatusBadRequest, err.Error())
		},
	})
	// Console WebSockets cannot go through the generated JSON handlers.
	mux.HandleFunc("GET "+ConsoleWebSocketPath, srv.serveConsole)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, r, http.StatusNotFound, "no route for "+r.Method+" "+r.URL.Path)
	})

	h := withMetrics(opts.Metrics, mux)
	h = withRecover(log, h)
	h = withAuthentication(opts.Auth, log, h)
	h = withCSRFProtection(opts.TrustedOrigins, h)
	h = withBodyLimit(opts.MaxBodyBytes, h)
	h = withAccessLog(log, h)
	h = withClientIP(h)
	h = withRequestID(h)
	return h
}

// server implements gen.StrictServerInterface.
type server struct {
	opts     Options
	logins   *loginLimiter
	originOK func(origin, host string) bool
	consoles *consoleSlots
}

var _ gen.StrictServerInterface = (*server)(nil)

func (s *server) GetHealthz(context.Context, gen.GetHealthzRequestObject) (gen.GetHealthzResponseObject, error) {
	return gen.GetHealthz200JSONResponse{Status: "ok"}, nil
}

func (s *server) GetReadyz(ctx context.Context, _ gen.GetReadyzRequestObject) (gen.GetReadyzResponseObject, error) {
	failed := make(map[string]struct{})
	for name, check := range s.opts.Checks {
		checkCtx, cancel := context.WithTimeout(ctx, s.opts.CheckTimeout)
		err := check(checkCtx)
		cancel()
		if err != nil {
			failed[name] = struct{}{}
			s.opts.Logger.WarnContext(ctx, "readiness check failed",
				slog.String("check", name), slog.Any("error", err))
		}
	}
	if len(failed) > 0 {
		// Error details stay in logs; only check names are exposed.
		detail := "not ready: " + strings.Join(slices.Sorted(maps.Keys(failed)), ", ")
		p := newProblem(ctx, "/readyz", http.StatusServiceUnavailable, detail)
		return gen.GetReadyz503ApplicationProblemPlusJSONResponse{
			ProblemApplicationProblemPlusJSONResponse: gen.ProblemApplicationProblemPlusJSONResponse(p),
		}, nil
	}
	return gen.GetReadyz200JSONResponse{Status: "ready"}, nil
}

func (s *server) GetVersion(context.Context, gen.GetVersionRequestObject) (gen.GetVersionResponseObject, error) {
	info := version.Get()
	return gen.GetVersion200JSONResponse{
		Version:   info.Version,
		Commit:    info.Commit,
		GoVersion: info.GoVersion,
		Platform:  info.Platform,
	}, nil
}
