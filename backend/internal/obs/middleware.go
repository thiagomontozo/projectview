package obs

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"projectview/internal/logger"
)

// Observe records metrics and emits one structured log line per request.
//
// The route label comes from chi's pattern ("/api/tasks/{id}") rather than the
// raw path, so metrics stay bounded: labelling by path would create a new time
// series per task id and blow up the metrics store.
func Observe(m *Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// The metrics endpoint scraping itself is noise.
			if r.URL.Path == "/metrics" {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}

			m.mu.Lock()
			m.inFlight++
			m.mu.Unlock()

			defer func() {
				m.mu.Lock()
				m.inFlight--
				m.mu.Unlock()
			}()

			next.ServeHTTP(rec, r)

			status := rec.status
			if status == 0 {
				status = http.StatusOK
			}
			elapsed := time.Since(start)

			route := chi.RouteContext(r.Context()).RoutePattern()
			if route == "" {
				route = "unmatched"
			}
			m.observe(r.Method, route, status, elapsed)

			logger.Request(logger.RequestLog{
				RequestID: middleware.GetReqID(r.Context()),
				Method:    r.Method,
				Route:     route,
				Path:      r.URL.Path,
				Status:    status,
				Bytes:     rec.bytes,
				Duration:  elapsed,
				RemoteIP:  r.RemoteAddr,
				UserAgent: r.UserAgent(),
			})
		})
	}
}
