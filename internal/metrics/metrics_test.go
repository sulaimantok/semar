package metrics

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExposition(t *testing.T) {
	r := NewRegistry("v0.1.0", "abc1234")
	c := r.Counter("semar_test_total", "Test counter.", "route", "status")
	c.Inc("GET /x", "200")
	c.Inc("GET /x", "200")
	c.Add(3, `quote"me`, "500")
	h := r.Histogram("semar_test_seconds", "Test histogram.", []float64{0.1, 1}, "route")
	h.Observe(0.05, "GET /x")
	h.Observe(0.5, "GET /x")
	h.Observe(5, "GET /x")

	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), "GET", "/metrics", nil))
	out := rec.Body.String()
	for _, want := range []string{
		"# TYPE semar_test_total counter",
		`semar_test_total{route="GET /x",status="200"} 2`,
		`semar_test_total{route="quote\"me",status="500"} 3`,
		`semar_test_seconds_bucket{route="GET /x",le="0.1"} 1`,
		`semar_test_seconds_bucket{route="GET /x",le="1"} 2`,
		`semar_test_seconds_bucket{route="GET /x",le="+Inf"} 3`,
		`semar_test_seconds_sum{route="GET /x"} 5.55`,
		`semar_test_seconds_count{route="GET /x"} 3`,
		`semar_build_info{version="v0.1.0",commit="abc1234"} 1`,
		"# TYPE go_goroutines gauge",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain; version=0.0.4") {
		t.Errorf("content type %q", ct)
	}
}

func TestWrongLabelCountPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("no panic for a missing label value")
		}
	}()
	NewRegistry("", "").Counter("x_total", "x", "a").Inc()
}
