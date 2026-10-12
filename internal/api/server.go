// Package api implements the semar-api HTTP surface. The contract lives in
// api/openapi.yaml at the repository root.
package api

import (
	"context"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

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
}

// NewHandler returns the root HTTP handler with middleware applied.
func NewHandler(opts Options) http.Handler {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.CheckTimeout <= 0 {
		opts.CheckTimeout = 2 * time.Second
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", readyz(opts))
	mux.HandleFunc("GET /api/v1/version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, version.Get())
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, r, http.StatusNotFound, "no route for "+r.Method+" "+r.URL.Path)
	})

	var h http.Handler = mux
	h = withRecover(opts.Logger, h)
	h = withAccessLog(opts.Logger, h)
	h = withRequestID(h)
	return h
}

func readyz(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		failed := make(map[string]string)
		for name, check := range opts.Checks {
			ctx, cancel := context.WithTimeout(r.Context(), opts.CheckTimeout)
			err := check(ctx)
			cancel()
			if err != nil {
				failed[name] = err.Error()
				opts.Logger.WarnContext(r.Context(), "readiness check failed",
					slog.String("check", name), slog.Any("error", err))
			}
		}
		if len(failed) > 0 {
			// Error details stay in logs; only check names are exposed.
			names := strings.Join(slices.Sorted(maps.Keys(failed)), ", ")
			writeProblem(w, r, http.StatusServiceUnavailable, "not ready: "+names)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	}
}
