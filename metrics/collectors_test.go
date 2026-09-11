package metrics

import (
	"testing"

	"github.com/Jana-Hassan/ratelimit-engine/engine"
)

func TestWatchEngineMirrorsCacheStats(t *testing.T) {
	m := New()
	cache := engine.NewEngine(8)
	m.WatchEngine(cache)
	cache.Set("posts:user-1", 1)
	cache.Set("posts:user-2", 2)
	cache.Get("posts:user-1")
	cache.Get("posts:missing")

	live := cache.Stats()

	if got := gauge(t, m, "ratelimit_cache_entries", nil); got != float64(live.Entries) {
		t.Fatalf("cache entries = %v, want %d", got, live.Entries)
	}
	if got := gauge(t, m, "ratelimit_cache_capacity", nil); got != float64(live.Capacity) {
		t.Fatalf("cache capacity = %v, want %d", got, live.Capacity)
	}
	if got := gauge(t, m, "ratelimit_cache_shards", nil); got != float64(live.Shards) {
		t.Fatalf("cache shards = %v, want %d", got, live.Shards)
	}
	if got := counter(t, m, "ratelimit_cache_hits_total", nil); got != float64(live.Hits) {
		t.Fatalf("cache hits = %v, want %d", got, live.Hits)
	}
	if got := counter(t, m, "ratelimit_cache_misses_total", nil); got != float64(live.Misses) {
		t.Fatalf("cache misses = %v, want %d", got, live.Misses)
	}
	if got := counter(t, m, "ratelimit_cache_evictions_total", nil); got != float64(live.Evictions) {
		t.Fatalf("cache evictions = %v, want %d", got, live.Evictions)
	}
}

func TestWatchEngineCountsHitsAndMisses(t *testing.T) {
	m := New()
	cache := engine.NewEngine(8)
	m.WatchEngine(cache)
	cache.Set("posts:user-1", 1)

	cache.Get("posts:user-1")
	cache.Get("posts:absent")

	if got := counter(t, m, "ratelimit_cache_hits_total", nil); got != 1 {
		t.Fatalf("cache hits = %v, want 1", got)
	}
	if got := counter(t, m, "ratelimit_cache_misses_total", nil); got != 1 {
		t.Fatalf("cache misses = %v, want 1", got)
	}
}

func TestWatchEngineReportsEvictions(t *testing.T) {
	m := New()
	cache := engine.NewEngine(1)
	m.WatchEngine(cache)

	for i := 0; i < 2000; i++ {
		cache.Set(string(rune('a'+i%26))+"-"+string(rune('a'+i/26%26))+"-"+string(rune('0'+i/676)), i)
	}

	if got := counter(t, m, "ratelimit_cache_evictions_total", nil); got == 0 {
		t.Fatal("cache evictions = 0, want the one-per-shard capacity to force evictions")
	}
}

func TestWatchEngineIsLive(t *testing.T) {
	m := New()
	cache := engine.NewEngine(8)
	m.WatchEngine(cache)

	before := gauge(t, m, "ratelimit_cache_entries", nil)
	cache.Set("posts:user-1", 1)
	after := gauge(t, m, "ratelimit_cache_entries", nil)

	if before != 0 {
		t.Fatalf("entries before = %v, want 0", before)
	}
	if after != 1 {
		t.Fatalf("entries after = %v, want 1, the collector must read the engine at scrape time", after)
	}
}

func TestWatchRulesCountsByAlgorithm(t *testing.T) {
	m := New()
	counts := map[string]int{"fixed_window": 2, "token_bucket": 1}
	m.WatchRules(func() map[string]int { return counts })

	if got := gauge(t, m, "ratelimit_rules", map[string]string{"algorithm": "fixed_window"}); got != 2 {
		t.Fatalf("fixed_window rules = %v, want 2", got)
	}
	if got := gauge(t, m, "ratelimit_rules", map[string]string{"algorithm": "token_bucket"}); got != 1 {
		t.Fatalf("token_bucket rules = %v, want 1", got)
	}
	if got := seriesCount(t, m, "ratelimit_rules"); got != 2 {
		t.Fatalf("rules series = %d, want 2", got)
	}
}

func TestWatchRulesIsLive(t *testing.T) {
	m := New()
	counts := map[string]int{"fixed_window": 1}
	m.WatchRules(func() map[string]int { return counts })

	before := gauge(t, m, "ratelimit_rules", map[string]string{"algorithm": "fixed_window"})
	counts["fixed_window"] = 5
	after := gauge(t, m, "ratelimit_rules", map[string]string{"algorithm": "fixed_window"})

	if before != 1 {
		t.Fatalf("rules before = %v, want 1", before)
	}
	if after != 5 {
		t.Fatalf("rules after = %v, want 5, the collector must call the counter at scrape time", after)
	}
}

func TestWatchRulesWithNoRules(t *testing.T) {
	m := New()
	m.WatchRules(func() map[string]int { return map[string]int{} })

	families, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("gathering metrics failed: %v", err)
	}

	for _, gathered := range families {
		if gathered.GetName() == "ratelimit_rules" && len(gathered.GetMetric()) != 0 {
			t.Fatalf("ratelimit_rules has %d series, want none", len(gathered.GetMetric()))
		}
	}
}
