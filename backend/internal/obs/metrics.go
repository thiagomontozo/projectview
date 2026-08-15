// Package obs provides the operational surface: request metrics and the
// middleware that records them.
//
// Metrics are exposed in Prometheus text format from a hand-rolled registry
// rather than the client library. The set collected here is small and fixed -
// the RED signals (rate, errors, duration) plus a few gauges - and a
// dependency-free implementation keeps the runtime image small and the
// exposition format easy to reason about.
package obs

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Buckets in seconds, covering "fast local query" through "something is wrong".
var latencyBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

type routeKey struct {
	method string
	route  string
	status int
}

type histogram struct {
	counts []uint64
	sum    float64
	total  uint64
}

func (h *histogram) observe(seconds float64) {
	if h.counts == nil {
		h.counts = make([]uint64, len(latencyBuckets))
	}
	for i, upper := range latencyBuckets {
		if seconds <= upper {
			h.counts[i]++
		}
	}
	h.sum += seconds
	h.total++
}

// Metrics collects request statistics.
type Metrics struct {
	mu         sync.Mutex
	requests   map[routeKey]uint64
	latency    map[routeKey]*histogram
	inFlight   int64
	startedAt  time.Time
	panics     uint64
	dbFailures uint64
}

func NewMetrics() *Metrics {
	return &Metrics{
		requests:  map[routeKey]uint64{},
		latency:   map[routeKey]*histogram{},
		startedAt: time.Now(),
	}
}

func (m *Metrics) RecordPanic()     { m.mu.Lock(); m.panics++; m.mu.Unlock() }
func (m *Metrics) RecordDBFailure() { m.mu.Lock(); m.dbFailures++; m.mu.Unlock() }

func (m *Metrics) observe(method, route string, status int, d time.Duration) {
	k := routeKey{method: method, route: route, status: status}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests[k]++
	h, ok := m.latency[k]
	if !ok {
		h = &histogram{}
		m.latency[k] = h
	}
	h.observe(d.Seconds())
}

// Handler serves the Prometheus exposition endpoint.
func (m *Metrics) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()

		var b strings.Builder

		b.WriteString("# HELP projectview_uptime_seconds Seconds since the process started.\n")
		b.WriteString("# TYPE projectview_uptime_seconds gauge\n")
		fmt.Fprintf(&b, "projectview_uptime_seconds %g\n", time.Since(m.startedAt).Seconds())

		b.WriteString("# HELP projectview_requests_in_flight Requests currently being served.\n")
		b.WriteString("# TYPE projectview_requests_in_flight gauge\n")
		fmt.Fprintf(&b, "projectview_requests_in_flight %d\n", m.inFlight)

		b.WriteString("# HELP projectview_panics_total Handler panics recovered.\n")
		b.WriteString("# TYPE projectview_panics_total counter\n")
		fmt.Fprintf(&b, "projectview_panics_total %d\n", m.panics)

		b.WriteString("# HELP projectview_db_failures_total Database operations that returned an error.\n")
		b.WriteString("# TYPE projectview_db_failures_total counter\n")
		fmt.Fprintf(&b, "projectview_db_failures_total %d\n", m.dbFailures)

		keys := make([]routeKey, 0, len(m.requests))
		for k := range m.requests {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i].route != keys[j].route {
				return keys[i].route < keys[j].route
			}
			if keys[i].method != keys[j].method {
				return keys[i].method < keys[j].method
			}
			return keys[i].status < keys[j].status
		})

		b.WriteString("# HELP projectview_http_requests_total Requests handled, by route and status.\n")
		b.WriteString("# TYPE projectview_http_requests_total counter\n")
		for _, k := range keys {
			fmt.Fprintf(&b, "projectview_http_requests_total{method=%q,route=%q,status=%q} %d\n",
				k.method, k.route, strconv.Itoa(k.status), m.requests[k])
		}

		b.WriteString("# HELP projectview_http_request_duration_seconds Request latency.\n")
		b.WriteString("# TYPE projectview_http_request_duration_seconds histogram\n")
		for _, k := range keys {
			h := m.latency[k]
			if h == nil {
				continue
			}
			labels := fmt.Sprintf("method=%q,route=%q,status=%q", k.method, k.route, strconv.Itoa(k.status))
			for i, upper := range latencyBuckets {
				fmt.Fprintf(&b, "projectview_http_request_duration_seconds_bucket{%s,le=\"%g\"} %d\n",
					labels, upper, h.counts[i])
			}
			fmt.Fprintf(&b, "projectview_http_request_duration_seconds_bucket{%s,le=\"+Inf\"} %d\n", labels, h.total)
			fmt.Fprintf(&b, "projectview_http_request_duration_seconds_sum{%s} %g\n", labels, h.sum)
			fmt.Fprintf(&b, "projectview_http_request_duration_seconds_count{%s} %d\n", labels, h.total)
		}

		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(b.String()))
	}
}

// statusRecorder captures the status code and response size, which the
// ResponseWriter interface otherwise discards.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// Hijack lets the WebSocket upgrade through: gorilla needs the underlying
// connection, and a wrapper that hides Hijacker breaks the handshake.
func (s *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := s.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, fmt.Errorf("underlying ResponseWriter does not support hijacking")
}

// Flush forwards to the wrapped writer when it supports flushing.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
