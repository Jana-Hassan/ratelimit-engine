package engine

type Stats struct {
	Hits      uint64
	Misses    uint64
	Evictions uint64
	Entries   int
	Capacity  int
	Shards    int
}

func (engine *Engine) Stats() Stats {
	stats := Stats{Shards: len(engine.shards)}
	for i := range engine.shards {
		s := &engine.shards[i]
		stats.Hits += s.hits.Load()
		stats.Misses += s.misses.Load()
		s.mu.RLock()
		stats.Entries += s.size
		stats.Capacity += s.capacity
		stats.Evictions += s.evictions
		s.mu.RUnlock()
	}
	return stats
}

func (s *shard) record(found bool) bool {
	if found {
		s.hits.Add(1)
	} else {
		s.misses.Add(1)
	}
	return found
}
