package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/adortb/adortb-cdp/internal/metrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// NewServer 注册所有路由并返回 http.Handler。
func NewServer(h *Handler, m *metrics.Metrics) http.Handler {
	mux := http.NewServeMux()

	// Profile 路由
	mux.HandleFunc("POST /v1/profiles", instrument(m, h.CreateOrUpdateProfile))
	mux.HandleFunc("GET /v1/profiles/{canonical_id}", instrument(m, h.GetProfile))
	mux.HandleFunc("POST /v1/profiles/{canonical_id}/events", instrument(m, h.WriteEvent))
	mux.HandleFunc("POST /v1/profiles/identify", instrument(m, h.IdentifyProfile))

	// Audience 路由
	mux.HandleFunc("POST /v1/audiences", instrument(m, h.CreateAudience))
	mux.HandleFunc("GET /v1/audiences/{id}/members", instrument(m, h.GetAudienceMembers))
	mux.HandleFunc("POST /v1/audiences/{id}/export", instrument(m, h.ExportAudience))

	// Journey 路由
	mux.HandleFunc("POST /v1/journeys", instrument(m, h.CreateJourney))
	mux.HandleFunc("POST /v1/journeys/{id}/activate", instrument(m, h.ActivateJourney))
	mux.HandleFunc("GET /v1/journeys/{id}/instances/{canonical_id}", instrument(m, h.GetJourneyInstance))

	// 观测路由
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	return mux
}

// instrument 包装 handler，记录请求延迟。
func instrument(m *metrics.Metrics, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next(rw, r)
		duration := time.Since(start).Seconds()
		m.APIRequestDuration.With(prometheus.Labels{
			"method": r.Method,
			"path":   r.URL.Path,
			"status": strconv.Itoa(rw.status),
		}).Observe(duration)
	}
}

// responseWriter 包装 http.ResponseWriter 捕获状态码。
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(status int) {
	rw.status = status
	rw.ResponseWriter.WriteHeader(status)
}
