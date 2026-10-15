package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/sulaimantok/semar/internal/metrics"
	"github.com/sulaimantok/semar/internal/requestid"
)

// RequestIDHeader carries the correlation ID end to end; it is later
// forwarded to OpenStack as X-OpenStack-Request-ID (roadmap §3.4).
const RequestIDHeader = "X-Request-ID"

// validRequestID bounds client-supplied IDs so they are safe to log and echo.
var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// RequestIDFrom returns the request ID stored in ctx, or "". The ID lives
// in package requestid so that drivers can forward it to providers.
func RequestIDFrom(ctx context.Context) string { return requestid.From(ctx) }

func newRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// withRequestID accepts a well-formed incoming X-Request-ID or generates one,
// stores it in the context and echoes it in the response.
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if !validRequestID.MatchString(id) {
			id = newRequestID()
		}
		w.Header().Set(RequestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(requestid.With(r.Context(), id)))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Unwrap exposes the underlying writer to http.ResponseController (needed
// later for hijacking console WebSocket connections).
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// withAccessLog logs one structured line per request.
func withAccessLog(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.LogAttrs(r.Context(), slog.LevelInfo, "http request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rec.status),
			slog.Duration("duration", time.Since(start)),
			slog.String("request_id", RequestIDFrom(r.Context())),
		)
	})
}

// withMetrics counts requests and their latency per route. It must wrap
// the router directly: the router records the matched pattern on the
// request it is given, which keeps the route label low-cardinality.
func withMetrics(reg *metrics.Registry, next http.Handler) http.Handler {
	if reg == nil {
		return next
	}
	requests := reg.Counter("semar_http_requests_total", "HTTP requests by route and status.", "method", "route", "status")
	latency := reg.Histogram("semar_http_request_duration_seconds", "HTTP request latency by route.", metrics.DefBuckets, "route")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		route := r.Pattern
		if route == "" || route == "/" {
			route = "unmatched"
		}
		requests.Inc(r.Method, route, strconv.Itoa(rec.status))
		latency.Observe(time.Since(start).Seconds(), route)
	})
}

// withRecover turns a handler panic into a 500 problem response.
func withRecover(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if err, ok := v.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(v)
				}
				log.ErrorContext(r.Context(), "handler panic",
					slog.Any("panic", v),
					slog.String("request_id", RequestIDFrom(r.Context())),
				)
				writeProblem(w, r, http.StatusInternalServerError, "")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// withBodyLimit caps request bodies; oversized bodies fail to decode and are
// answered with 400.
func withBodyLimit(limit int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
		}
		next.ServeHTTP(w, r)
	})
}
