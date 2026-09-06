package metrics

import (
	"github.com/Jana-Hassan/ratelimit-engine/engine"
	"github.com/prometheus/client_golang/prometheus"
)

type engineCollector struct {
	source *engine.Engine

	hits      *prometheus.Desc
	misses    *prometheus.Desc
	evictions *prometheus.Desc
	entries   *prometheus.Desc
	capacity  *prometheus.Desc
	shards    *prometheus.Desc
}

func newEngineCollector(source *engine.Engine) *engineCollector {
	return &engineCollector{
		source:    source,
		hits:      prometheus.NewDesc(namespace+"_cache_hits_total", "Cache lookups that found a live entry.", nil, nil),
		misses:    prometheus.NewDesc(namespace+"_cache_misses_total", "Cache lookups that found nothing.", nil, nil),
		evictions: prometheus.NewDesc(namespace+"_cache_evictions_total", "Entries evicted by the LFU policy.", nil, nil),
		entries:   prometheus.NewDesc(namespace+"_cache_entries", "Entries currently held across all shards.", nil, nil),
		capacity:  prometheus.NewDesc(namespace+"_cache_capacity", "Total entry capacity across all shards.", nil, nil),
		shards:    prometheus.NewDesc(namespace+"_cache_shards", "Number of cache shards.", nil, nil),
	}
}

func (c *engineCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.hits
	ch <- c.misses
	ch <- c.evictions
	ch <- c.entries
	ch <- c.capacity
	ch <- c.shards
}

func (c *engineCollector) Collect(ch chan<- prometheus.Metric) {
	stats := c.source.Stats()
	ch <- prometheus.MustNewConstMetric(c.hits, prometheus.CounterValue, float64(stats.Hits))
	ch <- prometheus.MustNewConstMetric(c.misses, prometheus.CounterValue, float64(stats.Misses))
	ch <- prometheus.MustNewConstMetric(c.evictions, prometheus.CounterValue, float64(stats.Evictions))
	ch <- prometheus.MustNewConstMetric(c.entries, prometheus.GaugeValue, float64(stats.Entries))
	ch <- prometheus.MustNewConstMetric(c.capacity, prometheus.GaugeValue, float64(stats.Capacity))
	ch <- prometheus.MustNewConstMetric(c.shards, prometheus.GaugeValue, float64(stats.Shards))
}

func (m *Metrics) WatchEngine(source *engine.Engine) {
	m.registry.MustRegister(newEngineCollector(source))
}

type rulesCollector struct {
	count func() map[string]int
	desc  *prometheus.Desc
}

func (c *rulesCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.desc
}

func (c *rulesCollector) Collect(ch chan<- prometheus.Metric) {
	for algorithm, count := range c.count() {
		ch <- prometheus.MustNewConstMetric(c.desc, prometheus.GaugeValue, float64(count), algorithm)
	}
}

func (m *Metrics) WatchRules(count func() map[string]int) {
	m.registry.MustRegister(&rulesCollector{
		count: count,
		desc:  prometheus.NewDesc(namespace+"_rules", "Configured rules by algorithm.", []string{"algorithm"}, nil),
	})
}
