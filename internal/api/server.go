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
	log := opts.Logger
	srv := &server{opts: opts, logins: newLoginLimiter(opts.LoginLimits)}

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
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, r, http.StatusNotFound, "no route for "+r.Method+" "+r.URL.Path)
	})

	var h http.Handler = mux
	h = withRecover(log, h)
	h = withAuthentication(opts.Auth, log, h)
	h = withCSRFProtection(h)
	h = withBodyLimit(opts.MaxBodyBytes, h)
	h = withAccessLog(log, h)
	h = withClientIP(h)
	h = withRequestID(h)
	return h
}

// server implements gen.StrictServerInterface.
type server struct {
	opts   Options
	logins *loginLimiter
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
