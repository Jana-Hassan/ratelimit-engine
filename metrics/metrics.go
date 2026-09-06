package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

const namespace = "ratelimit"

var decisionBuckets = []float64{
	100e-9, 250e-9, 500e-9, 1e-6, 2.5e-6, 5e-6, 10e-6, 25e-6, 50e-6, 100e-6, 250e-6, 500e-6, 1e-3,
}

var httpBuckets = []float64{
	50e-6, 100e-6, 250e-6, 500e-6, 1e-3, 2.5e-3, 5e-3, 10e-3, 25e-3, 50e-3, 100e-3, 250e-3, 1,
}

type Metrics struct {
	registry *prometheus.Registry

	decisions        *prometheus.CounterVec
	decisionDuration *prometheus.HistogramVec

	httpRequests    *prometheus.CounterVec
	httpDuration    *prometheus.HistogramVec
	probesThrottled prometheus.Counter

	startedAt time.Time
}

func New() *Metrics {
	m := &Metrics{
		registry: prometheus.NewRegistry(),
		decisions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "decisions_total",
			Help:      "Rate limit decisions by algorithm, rule, mode and outcome.",
		}, []string{"algorithm", "rule", "mode", "decision"}),
		decisionDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "decision_duration_seconds",
			Help:      "Time spent inside the algorithm producing a decision.",
			Buckets:   decisionBuckets,
		}, []string{"algorithm", "rule", "mode"}),
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "http_requests_total",
			Help:      "HTTP requests by route, method and status code.",
		}, []string{"route", "method", "status"}),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "http_request_duration_seconds",
			Help:      "HTTP request latency by route and method.",
			Buckets:   httpBuckets,
		}, []string{"route", "method"}),
		probesThrottled: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "probes_throttled_total",
			Help:      "HEAD /check probes rejected by the probe rate limit.",
		}),
		startedAt: time.Now(),
	}

	m.registry.MustRegister(
		m.decisions,
		m.decisionDuration,
		m.httpRequests,
		m.httpDuration,
		m.probesThrottled,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	return m
}

func (m *Metrics) Registry() *prometheus.Registry {
	return m.registry
}

func (m *Metrics) Uptime() time.Duration {
	return time.Since(m.startedAt)
}

func decisionLabel(allowed bool) string {
	if allowed {
		return "allowed"
	}
	return "denied"
}

func (m *Metrics) RecordDecision(algorithm string, rule string, mode string, allowed bool, elapsed time.Duration) {
	m.decisions.WithLabelValues(algorithm, rule, mode, decisionLabel(allowed)).Inc()
	m.decisionDuration.WithLabelValues(algorithm, rule, mode).Observe(elapsed.Seconds())
}

func (m *Metrics) RecordHTTP(route string, method string, status string, elapsed time.Duration) {
	m.httpRequests.WithLabelValues(route, method, status).Inc()
	m.httpDuration.WithLabelValues(route, method).Observe(elapsed.Seconds())
}

func (m *Metrics) RecordProbeThrottled() {
	m.probesThrottled.Inc()
}
