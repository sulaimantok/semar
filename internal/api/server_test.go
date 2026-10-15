package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sulaimantok/semar/internal/api/gen"
	"github.com/sulaimantok/semar/internal/metrics"
	"github.com/sulaimantok/semar/internal/version"
)

func newTestHandler(checks map[string]ReadinessCheck) http.Handler {
	return NewHandler(Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Checks: checks,
	})
}

func do(t *testing.T, h http.Handler, method, path string, header http.Header) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, path, nil)
	for k, vs := range header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHealthz(t *testing.T) {
	rec := do(t, newTestHandler(nil), http.MethodGet, "/healthz", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q", ct)
	}
}

func TestReadyz(t *testing.T) {
	ok := func(context.Context) error { return nil }
	bad := func(context.Context) error { return errors.New("dial tcp 10.0.0.1:5432: secret detail") }

	rec := do(t, newTestHandler(map[string]ReadinessCheck{"db": ok}), http.MethodGet, "/readyz", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("all ok: status = %d, want 200", rec.Code)
	}

	rec = do(t, newTestHandler(map[string]ReadinessCheck{"db": bad, "temporal": ok}), http.MethodGet, "/readyz", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("failing check: status = %d, want 503", rec.Code)
	}
	p := decodeProblem(t, rec)
	if p.Detail == nil || !strings.Contains(*p.Detail, "db") || strings.Contains(*p.Detail, "temporal") {
		t.Fatalf("detail = %v, want only the failing check name", p.Detail)
	}
	if strings.Contains(rec.Body.String(), "secret detail") {
		t.Fatal("readiness error detail leaked to client")
	}
}

func TestVersion(t *testing.T) {
	rec := do(t, newTestHandler(nil), http.MethodGet, "/api/v1/version", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got version.Info
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got != version.Get() {
		t.Fatalf("version = %+v, want %+v", got, version.Get())
	}
}

func TestReadyzProblemCarriesRequestID(t *testing.T) {
	bad := func(context.Context) error { return errors.New("down") }
	h := newTestHandler(map[string]ReadinessCheck{"db": bad})
	rec := do(t, h, http.MethodGet, "/readyz", http.Header{RequestIDHeader: {"ready-check-1"}})
	p := decodeProblem(t, rec)
	if p.RequestId == nil || *p.RequestId != "ready-check-1" || p.Instance == nil || *p.Instance != "/readyz" {
		t.Fatalf("problem = %+v", p)
	}
}

func TestWrongMethodIsProblemJSON(t *testing.T) {
	rec := do(t, newTestHandler(nil), http.MethodPost, "/api/v1/version", http.Header{CSRFHeader: {"test"}})
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 404 or 405", rec.Code)
	}
	decodeProblem(t, rec)
}

func TestUnknownRouteIsProblemJSON(t *testing.T) {
	rec := do(t, newTestHandler(nil), http.MethodGet, "/api/v1/nope", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	p := decodeProblem(t, rec)
	if p.Status != http.StatusNotFound || p.Instance == nil || *p.Instance != "/api/v1/nope" || p.RequestId == nil || *p.RequestId == "" {
		t.Fatalf("problem = %+v", p)
	}
}

func TestRequestID(t *testing.T) {
	h := newTestHandler(nil)

	rec := do(t, h, http.MethodGet, "/healthz", http.Header{RequestIDHeader: {"req-abc.123"}})
	if got := rec.Header().Get(RequestIDHeader); got != "req-abc.123" {
		t.Fatalf("valid incoming ID not echoed: %q", got)
	}

	rec = do(t, h, http.MethodGet, "/healthz", http.Header{RequestIDHeader: {"bad id\nwith newline"}})
	if got := rec.Header().Get(RequestIDHeader); got == "" || strings.ContainsAny(got, " \n") {
		t.Fatalf("invalid incoming ID not replaced: %q", got)
	}

	rec = do(t, h, http.MethodGet, "/healthz", nil)
	if got := rec.Header().Get(RequestIDHeader); len(got) != 32 {
		t.Fatalf("generated ID = %q, want 32 hex chars", got)
	}
}

func TestPanicBecomesProblem(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := withRequestID(withRecover(log, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})))
	rec := do(t, h, http.MethodGet, "/x", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if p := decodeProblem(t, rec); p.Detail != nil {
		t.Fatalf("panic detail leaked: %q", *p.Detail)
	}
}

func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) gen.Problem {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("content-type = %q, want application/problem+json", ct)
	}
	var p gen.Problem
	if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestHTTPMetricsUseRoutePatterns(t *testing.T) {
	reg := metrics.NewRegistry("test", "test")
	a := newAnonymousAPI(t, func(o *Options) { o.Metrics = reg })
	a.expect(a.call("GET", "/api/v1/providers/pve-lab", ""), http.StatusUnauthorized, nil)
	a.expect(a.call("GET", "/api/v1/providers/os-a", ""), http.StatusUnauthorized, nil)
	a.expect(a.call("GET", "/no/such/path", ""), http.StatusNotFound, nil)

	rec := httptest.NewRecorder()
	reg.Handler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), "GET", "/metrics", nil))
	out := rec.Body.String()
	for _, want := range []string{
		`semar_http_requests_total{method="GET",route="GET /api/v1/providers/{providerId}",status="401"} 2`,
		`semar_http_requests_total{method="GET",route="unmatched",status="404"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "pve-lab") {
		t.Error("raw path values leaked into metric labels")
	}
}
