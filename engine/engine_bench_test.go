package engine

import (
	"hash/fnv"
	"strconv"
	"sync/atomic"
	"testing"
)

const benchShardCapacity = 1024

func benchKeys(count int) []string {
	keys := make([]string, count)
	for i := range keys {
		keys[i] = "rule:client-" + strconv.Itoa(i)
	}
	return keys
}

func filledEngine(perShard int, keys []string) *Engine {
	e := NewEngine(perShard)
	for _, key := range keys {
		e.Set(key, 1)
	}
	return e
}

func BenchmarkEngineSet(b *testing.B) {
	keys := benchKeys(1 << 14)
	e := NewEngine(benchShardCapacity)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.Set(keys[i&(len(keys)-1)], i)
	}
}

func BenchmarkEngineGetHit(b *testing.B) {
	keys := benchKeys(1 << 14)
	e := filledEngine(benchShardCapacity, keys)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.Get(keys[i&(len(keys)-1)])
	}
}

func BenchmarkEngineGetMiss(b *testing.B) {
	e := NewEngine(benchShardCapacity)
	keys := benchKeys(1 << 14)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.Get(keys[i&(len(keys)-1)])
	}
}

func BenchmarkEngineUpdate(b *testing.B) {
	keys := benchKeys(1 << 14)
	e := filledEngine(benchShardCapacity, keys)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.Update(keys[i&(len(keys)-1)], func(value interface{}, found bool) interface{} {
			return value
		})
	}
}

func BenchmarkEngineView(b *testing.B) {
	keys := benchKeys(1 << 14)
	e := filledEngine(benchShardCapacity, keys)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.View(keys[i&(len(keys)-1)], func(value interface{}, found bool) {})
	}
}

func BenchmarkEngineGetBySize(b *testing.B) {
	for _, perShard := range []int{100, 1000, 10000} {
		b.Run(strconv.Itoa(perShard)+"-per-shard", func(b *testing.B) {
			keys := benchKeys(perShard * 256)
			e := filledEngine(perShard, keys)
			mask := len(keys) - 1
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				e.Get(keys[i&mask])
			}
		})
	}
}

func BenchmarkEngineEvict(b *testing.B) {
	keys := benchKeys(1 << 16)
	e := filledEngine(1, keys)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.Set(keys[i&(len(keys)-1)], i)
	}
}

func BenchmarkEngineParallelUpdateSpread(b *testing.B) {
	keys := benchKeys(1 << 14)
	e := filledEngine(benchShardCapacity, keys)
	var seed atomic.Uint64
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := int(seed.Add(1)) * 4096
		for pb.Next() {
			e.Update(keys[i&(len(keys)-1)], func(value interface{}, found bool) interface{} {
				return value
			})
			i++
		}
	})
}

func BenchmarkEngineParallelUpdateHotKey(b *testing.B) {
	e := filledEngine(benchShardCapacity, []string{"rule:hot"})
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			e.Update("rule:hot", func(value interface{}, found bool) interface{} {
				return value
			})
		}
	})
}

func BenchmarkEngineParallelViewSpread(b *testing.B) {
	keys := benchKeys(1 << 14)
	e := filledEngine(benchShardCapacity, keys)
	var seed atomic.Uint64
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := int(seed.Add(1)) * 4096
		for pb.Next() {
			e.View(keys[i&(len(keys)-1)], func(value interface{}, found bool) {})
			i++
		}
	})
}

func BenchmarkEngineParallelViewHotKey(b *testing.B) {
	e := filledEngine(benchShardCapacity, []string{"rule:hot"})
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			e.View("rule:hot", func(value interface{}, found bool) {})
		}
	})
}

type singleShardEngine struct {
	s shard
}

func newSingleShardEngine(capacity int) *singleShardEngine {
	return &singleShardEngine{s: shard{
		freqMap:  make(map[int]*frequencyList),
		keyMap:   make(map[string]*Node),
		capacity: capacity,
	}}
}

func (e *singleShardEngine) getShard(key string) *shard {
	hash := fnv.New32a()
	hash.Write([]byte(key))
	_ = hash.Sum32() % 256
	return &e.s
}

func (e *singleShardEngine) Set(key string, value interface{}) {
	s := e.getShard(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.set(key, value)
}

func (e *singleShardEngine) Update(key string, fn func(value interface{}, found bool) interface{}) {
	s := e.getShard(key)
	s.mu.Lock()
	defer s.mu.Unlock()

	var current interface{}
	node, found := s.keyMap[key]
	if found {
		current = node.value
	}
	s.set(key, fn(current, s.record(found)))
}

func BenchmarkShardingUpdate(b *testing.B) {
	keys := benchKeys(1 << 14)
	noop := func(value interface{}, found bool) interface{} { return value }

	b.Run("256-shards", func(b *testing.B) {
		e := filledEngine(benchShardCapacity, keys)
		var seed atomic.Uint64
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			i := int(seed.Add(1)) * 4096
			for pb.Next() {
				e.Update(keys[i&(len(keys)-1)], noop)
				i++
			}
		})
	})

	b.Run("1-shard", func(b *testing.B) {
		e := newSingleShardEngine(benchShardCapacity * 256)
		for _, key := range keys {
			e.Set(key, 1)
		}
		var seed atomic.Uint64
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			i := int(seed.Add(1)) * 4096
			for pb.Next() {
				e.Update(keys[i&(len(keys)-1)], noop)
				i++
			}
		})
	})
}
