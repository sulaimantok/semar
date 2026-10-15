// Package metrics is a small Prometheus text-format exposition (version
// 0.0.4) for SEMAR's internal metrics (roadmap §3.10): counters, gauges and
// histograms with labels. It avoids pulling a metrics client library into
// both binaries for a handful of series.
package metrics

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Registry holds metric families and serves them.
type Registry struct {
	mu       sync.Mutex
	families []family
}

type family interface {
	write(b *strings.Builder)
}

// NewRegistry returns a registry with process metrics (goroutines, heap,
// start time) and semar_build_info.
func NewRegistry(version, commit string) *Registry {
	r := &Registry{}
	start := float64(time.Now().Unix())
	r.GaugeFunc("process_start_time_seconds", "Start time of the process since the Unix epoch.", func() float64 { return start })
	r.GaugeFunc("go_goroutines", "Number of goroutines.", func() float64 { return float64(runtime.NumGoroutine()) })
	r.GaugeFunc("go_memstats_heap_alloc_bytes", "Heap bytes allocated and in use.", func() float64 {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return float64(m.HeapAlloc)
	})
	r.Gauge("semar_build_info", "SEMAR build information.", "version", "commit").Set(1, version, commit)
	return r
}

func (r *Registry) add(f family) {
	r.mu.Lock()
	r.families = append(r.families, f)
	r.mu.Unlock()
}

// Handler serves the registry in the Prometheus text format.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		var b strings.Builder
		r.mu.Lock()
		fams := append([]family(nil), r.families...)
		r.mu.Unlock()
		for _, f := range fams {
			f.write(&b)
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte(b.String()))
	})
}

// --- Labels ---

func key(values []string) string { return strings.Join(values, "\xff") }

var labelEscaper = strings.NewReplacer(`\`, `\\`, "\n", `\n`, `"`, `\"`)

func labelString(names, values []string, extra ...string) string {
	if len(names) == 0 && len(extra) == 0 {
		return ""
	}
	parts := make([]string, 0, len(names)+1)
	for i, n := range names {
		parts = append(parts, n+`="`+labelEscaper.Replace(values[i])+`"`)
	}
	for i := 0; i+1 < len(extra); i += 2 {
		parts = append(parts, extra[i]+`="`+extra[i+1]+`"`)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func header(b *strings.Builder, name, help, typ string) {
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
}

func formatFloat(v float64) string {
	switch {
	case math.IsInf(v, 1):
		return "+Inf"
	case math.IsInf(v, -1):
		return "-Inf"
	default:
		return strconv.FormatFloat(v, 'g', -1, 64)
	}
}

func checkValues(name string, labels, values []string) {
	if len(labels) != len(values) {
		panic(fmt.Sprintf("metrics: %s takes %d label values, got %d", name, len(labels), len(values)))
	}
}

// --- Counter and gauge ---

// Vec is a counter or gauge with labels.
type Vec struct {
	name, help, typ string
	labels          []string

	mu     sync.Mutex
	values map[string]float64
	keys   map[string][]string
}

// Counter registers a monotonically increasing counter.
func (r *Registry) Counter(name, help string, labels ...string) *Vec {
	v := &Vec{name: name, help: help, typ: "counter", labels: labels, values: map[string]float64{}, keys: map[string][]string{}}
	r.add(v)
	return v
}

// Gauge registers a value that can go up and down.
func (r *Registry) Gauge(name, help string, labels ...string) *Vec {
	v := r.Counter(name, help, labels...)
	v.typ = "gauge"
	return v
}

// Add adds d (which must be >= 0 for counters).
func (v *Vec) Add(d float64, values ...string) {
	checkValues(v.name, v.labels, values)
	k := key(values)
	v.mu.Lock()
	v.values[k] += d
	v.keys[k] = values
	v.mu.Unlock()
}

// Inc adds one.
func (v *Vec) Inc(values ...string) { v.Add(1, values...) }

// Set sets a gauge.
func (v *Vec) Set(x float64, values ...string) {
	checkValues(v.name, v.labels, values)
	k := key(values)
	v.mu.Lock()
	v.values[k] = x
	v.keys[k] = values
	v.mu.Unlock()
}

// Value returns the current value (tests).
func (v *Vec) Value(values ...string) float64 {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.values[key(values)]
}

func (v *Vec) write(b *strings.Builder) {
	v.mu.Lock()
	defer v.mu.Unlock()
	header(b, v.name, v.help, v.typ)
	ks := make([]string, 0, len(v.values))
	for k := range v.values {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		fmt.Fprintf(b, "%s%s %s\n", v.name, labelString(v.labels, v.keys[k]), formatFloat(v.values[k]))
	}
}

type gaugeFunc struct {
	name, help string
	fn         func() float64
}

// GaugeFunc registers a gauge read at scrape time.
func (r *Registry) GaugeFunc(name, help string, fn func() float64) {
	r.add(gaugeFunc{name: name, help: help, fn: fn})
}

func (g gaugeFunc) write(b *strings.Builder) {
	header(b, g.name, g.help, "gauge")
	fmt.Fprintf(b, "%s %s\n", g.name, formatFloat(g.fn()))
}

// --- Histogram ---

// DefBuckets suit request latencies in seconds.
var DefBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// SyncBuckets suit inventory syncs, which take seconds to minutes.
var SyncBuckets = []float64{0.5, 1, 2.5, 5, 10, 30, 60, 120, 300}

// Histogram is a histogram with labels.
type Histogram struct {
	name, help string
	labels     []string
	buckets    []float64

	mu     sync.Mutex
	series map[string]*hist
}

type hist struct {
	values []string
	counts []uint64
	sum    float64
	count  uint64
}

// Histogram registers a histogram with the given upper bounds.
func (r *Registry) Histogram(name, help string, buckets []float64, labels ...string) *Histogram {
	h := &Histogram{name: name, help: help, labels: labels, buckets: buckets, series: map[string]*hist{}}
	r.add(h)
	return h
}

// Observe records one value.
func (h *Histogram) Observe(x float64, values ...string) {
	checkValues(h.name, h.labels, values)
	k := key(values)
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.series[k]
	if !ok {
		s = &hist{values: values, counts: make([]uint64, len(h.buckets))}
		h.series[k] = s
	}
	for i, ub := range h.buckets {
		if x <= ub {
			s.counts[i]++
		}
	}
	s.sum += x
	s.count++
}

func (h *Histogram) write(b *strings.Builder) {
	h.mu.Lock()
	defer h.mu.Unlock()
	header(b, h.name, h.help, "histogram")
	ks := make([]string, 0, len(h.series))
	for k := range h.series {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		s := h.series[k]
		for i, ub := range h.buckets {
			fmt.Fprintf(b, "%s_bucket%s %d\n", h.name, labelString(h.labels, s.values, "le", formatFloat(ub)), s.counts[i])
		}
		fmt.Fprintf(b, "%s_bucket%s %d\n", h.name, labelString(h.labels, s.values, "le", "+Inf"), s.count)
		fmt.Fprintf(b, "%s_sum%s %s\n", h.name, labelString(h.labels, s.values), formatFloat(s.sum))
		fmt.Fprintf(b, "%s_count%s %d\n", h.name, labelString(h.labels, s.values), s.count)
	}
}

// Serve runs a metrics listener on addr until the returned shutdown is
// called. The listener is separate from the API so that metrics can be
// bound to an internal interface; extra handlers (e.g. /healthz) may be
// added.
func Serve(addr string, r *Registry, extra map[string]http.Handler) (*http.Server, <-chan error) {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", r.Handler())
	for p, h := range extra {
		mux.Handle(p, h)
	}
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	errc := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
		close(errc)
	}()
	return srv, errc
}
