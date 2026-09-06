package api

import (
	"net/http"

	"github.com/Jana-Hassan/ratelimit-engine/engine"
	"github.com/Jana-Hassan/ratelimit-engine/metrics"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type cacheStats struct {
	Hits      uint64  `json:"hits"`
	Misses    uint64  `json:"misses"`
	HitRate   float64 `json:"hit_rate"`
	Evictions uint64  `json:"evictions"`
	Entries   int     `json:"entries"`
	Capacity  int     `json:"capacity"`
	Shards    int     `json:"shards"`
}

type statsResponse struct {
	UptimeSec int                `json:"uptime_sec"`
	Rules     int                `json:"rules"`
	Cache     cacheStats         `json:"cache"`
	Decisions []metrics.RuleStat `json:"decisions"`
}

func newCacheStats(stats engine.Stats) cacheStats {
	lookups := stats.Hits + stats.Misses
	var hitRate float64
	if lookups > 0 {
		hitRate = float64(stats.Hits) / float64(lookups)
	}
	return cacheStats{
		Hits:      stats.Hits,
		Misses:    stats.Misses,
		HitRate:   hitRate,
		Evictions: stats.Evictions,
		Entries:   stats.Entries,
		Capacity:  stats.Capacity,
		Shards:    stats.Shards,
	}
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, statsResponse{
		UptimeSec: wholeSeconds(s.metrics.Uptime()),
		Rules:     len(s.limiter.Rules().List()),
		Cache:     newCacheStats(s.engine.Stats()),
		Decisions: s.metrics.RuleStats(),
	})
}

func (s *Server) metricsHandler() http.Handler {
	return promhttp.HandlerFor(s.metrics.Registry(), promhttp.HandlerOpts{})
}
