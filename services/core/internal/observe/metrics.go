package observe

import (
	"context"
	"crypto/subtle"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Metrics is a small Prometheus text-format registry: request counters and
// latency histograms recorded by the middleware, plus gauges computed at
// scrape time (for example queue depths read from the database).
type Metrics struct {
	mu       sync.Mutex
	requests map[string]float64 // key: route|method|class
	buckets  []float64
	hist     map[string]*histogram // key: route
	gauges   []GaugeFunc
}

// Gauge is one sample of a gauge family.
type Gauge struct {
	Name   string
	Help   string
	Labels map[string]string
	Value  float64
}

// GaugeFunc produces gauge samples when /metrics is scraped.
type GaugeFunc func(ctx context.Context) ([]Gauge, error)

type histogram struct {
	counts []float64
	sum    float64
	count  float64
}

func NewMetrics() *Metrics {
	return &Metrics{
		requests: map[string]float64{},
		buckets:  []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		hist:     map[string]*histogram{},
	}
}

// AddGauges registers a scrape-time gauge source.
func (m *Metrics) AddGauges(f GaugeFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gauges = append(m.gauges, f)
}

func (m *Metrics) observe(route, method string, status int, elapsed time.Duration) {
	if m == nil {
		return
	}
	class := strconv.Itoa(status/100) + "xx"
	seconds := elapsed.Seconds()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests[route+"|"+method+"|"+class]++
	h, ok := m.hist[route]
	if !ok {
		h = &histogram{counts: make([]float64, len(m.buckets))}
		m.hist[route] = h
	}
	for i, b := range m.buckets {
		if seconds <= b {
			h.counts[i]++
		}
	}
	h.sum += seconds
	h.count++
}

func escapeLabel(v string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(v)
}

func labelString(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf(`%s="%s"`, k, escapeLabel(labels[k])))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func formatFloat(v float64) string {
	if math.IsInf(v, 1) {
		return "+Inf"
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// Write writes every metric in Prometheus text exposition format.
func (m *Metrics) Write(ctx context.Context, w io.Writer) error {
	m.mu.Lock()
	requestKeys := make([]string, 0, len(m.requests))
	for k := range m.requests {
		requestKeys = append(requestKeys, k)
	}
	sort.Strings(requestKeys)
	fmt.Fprintln(w, "# HELP aside_http_requests_total HTTP requests by route pattern, method and status class.")
	fmt.Fprintln(w, "# TYPE aside_http_requests_total counter")
	for _, k := range requestKeys {
		p := strings.SplitN(k, "|", 3)
		fmt.Fprintf(w, "aside_http_requests_total%s %s\n", labelString(map[string]string{"route": p[0], "method": p[1], "status": p[2]}), formatFloat(m.requests[k]))
	}
	routes := make([]string, 0, len(m.hist))
	for k := range m.hist {
		routes = append(routes, k)
	}
	sort.Strings(routes)
	fmt.Fprintln(w, "# HELP aside_http_request_duration_seconds HTTP request latency by route pattern.")
	fmt.Fprintln(w, "# TYPE aside_http_request_duration_seconds histogram")
	for _, route := range routes {
		h := m.hist[route]
		for i, b := range m.buckets {
			fmt.Fprintf(w, "aside_http_request_duration_seconds_bucket%s %s\n", labelString(map[string]string{"route": route, "le": formatFloat(b)}), formatFloat(h.counts[i]))
		}
		fmt.Fprintf(w, "aside_http_request_duration_seconds_bucket%s %s\n", labelString(map[string]string{"route": route, "le": "+Inf"}), formatFloat(h.count))
		fmt.Fprintf(w, "aside_http_request_duration_seconds_sum%s %s\n", labelString(map[string]string{"route": route}), formatFloat(h.sum))
		fmt.Fprintf(w, "aside_http_request_duration_seconds_count%s %s\n", labelString(map[string]string{"route": route}), formatFloat(h.count))
	}
	gauges := append([]GaugeFunc(nil), m.gauges...)
	m.mu.Unlock()

	seen := map[string]bool{}
	for _, f := range gauges {
		samples, err := f(ctx)
		if err != nil {
			fmt.Fprintf(w, "# gauge source failed: %s\n", strings.ReplaceAll(err.Error(), "\n", " "))
			continue
		}
		for _, g := range samples {
			if !seen[g.Name] {
				fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n", g.Name, g.Help, g.Name)
				seen[g.Name] = true
			}
			fmt.Fprintf(w, "%s%s %s\n", g.Name, labelString(g.Labels), formatFloat(g.Value))
		}
	}
	return nil
}

// Handler serves /metrics. When token is empty the endpoint is disabled (404);
// otherwise callers must send "Authorization: Bearer <token>".
func (m *Metrics) Handler(token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if token == "" {
			http.NotFound(w, r)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = m.Write(r.Context(), w)
	}
}
