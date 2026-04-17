// Package metrics 提供 Prometheus 指标注册与暴露。
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics 聚合所有 CDP Prometheus 指标。
type Metrics struct {
	ProfileUpserts    prometheus.Counter
	EventsWritten     prometheus.Counter
	AudienceEvals     prometheus.Counter
	AudienceMemberships *prometheus.GaugeVec
	JourneyEnters     prometheus.Counter
	KafkaMessagesProcessed *prometheus.CounterVec
	APIRequestDuration     *prometheus.HistogramVec
}

// New 注册并返回 Metrics 实例（promauto 自动注册到默认 registry）。
func New() *Metrics {
	return &Metrics{
		ProfileUpserts: promauto.NewCounter(prometheus.CounterOpts{
			Name: "cdp_profile_upserts_total",
			Help: "Total profile upserts",
		}),
		EventsWritten: promauto.NewCounter(prometheus.CounterOpts{
			Name: "cdp_events_written_total",
			Help: "Total events written",
		}),
		AudienceEvals: promauto.NewCounter(prometheus.CounterOpts{
			Name: "cdp_audience_evaluations_total",
			Help: "Total audience evaluations",
		}),
		AudienceMemberships: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "cdp_audience_size",
			Help: "Current audience member count",
		}, []string{"audience_id"}),
		JourneyEnters: promauto.NewCounter(prometheus.CounterOpts{
			Name: "cdp_journey_enters_total",
			Help: "Total journey entries",
		}),
		KafkaMessagesProcessed: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "cdp_kafka_messages_processed_total",
			Help: "Kafka messages processed by topic",
		}, []string{"topic"}),
		APIRequestDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "cdp_api_request_duration_seconds",
			Help:    "API request latency distribution",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "path", "status"}),
	}
}
