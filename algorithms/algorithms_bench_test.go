package algorithms

import (
	"strconv"
	"testing"
	"time"

	"github.com/Jana-Hassan/ratelimit-engine/engine"
)

func benchAlgorithms(e *engine.Engine) map[string]Algorithm {
	return map[string]Algorithm{
		"fixed_window":       NewFixedWindow(e),
		"sliding_window":     NewSlidingWindow(e),
		"sliding_window_log": NewSlidingWindowLog(e),
		"token_bucket":       NewTokenBucket(e),
	}
}

func benchAlgorithmKeys(count int) []string {
	keys := make([]string, count)
	for i := range keys {
		keys[i] = "bench:client-" + strconv.Itoa(i)
	}
	return keys
}

func BenchmarkAllowAllowed(b *testing.B) {
	for name, algorithm := range benchAlgorithms(engine.NewEngine(1024)) {
		for _, limit := range []int{10, 100, 1000} {
			b.Run(name+"/limit-"+strconv.Itoa(limit), func(b *testing.B) {
				rule := Rule{Name: name, Limit: limit, WindowSec: 1}
				keys := benchAlgorithmKeys(1 << 12)
				algorithm.Allow(keys[0], rule)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					algorithm.Allow(keys[i&(len(keys)-1)], rule)
				}
			})
		}
	}
}

func BenchmarkAllowDenied(b *testing.B) {
	for name, algorithm := range benchAlgorithms(engine.NewEngine(1024)) {
		for _, limit := range []int{10, 100, 1000} {
			b.Run(name+"/limit-"+strconv.Itoa(limit), func(b *testing.B) {
				rule := Rule{Name: name, Limit: limit, WindowSec: 3600}
				key := "denied:" + name + ":" + strconv.Itoa(limit)
				for i := 0; i < limit; i++ {
					algorithm.Allow(key, rule)
				}
				if algorithm.Allow(key, rule).Allowed {
					b.Fatal("key is not exhausted")
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					algorithm.Allow(key, rule)
				}
			})
		}
	}
}

func BenchmarkPeek(b *testing.B) {
	for name, algorithm := range benchAlgorithms(engine.NewEngine(1024)) {
		for _, limit := range []int{10, 100, 1000} {
			b.Run(name+"/limit-"+strconv.Itoa(limit), func(b *testing.B) {
				rule := Rule{Name: name, Limit: limit, WindowSec: 3600}
				key := "peek:" + name + ":" + strconv.Itoa(limit)
				for i := 0; i < limit/2; i++ {
					algorithm.Allow(key, rule)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					algorithm.Peek(key, rule)
				}
			})
		}
	}
}

func BenchmarkSlidingWindowLogSteadyState(b *testing.B) {
	for _, limit := range []int{10, 100, 1000} {
		b.Run("limit-"+strconv.Itoa(limit), func(b *testing.B) {
			swl := NewSlidingWindowLog(engine.NewEngine(1024))
			rule := Rule{Name: "swl", Limit: limit, WindowSec: 1}
			key := "swl:steady:" + strconv.Itoa(limit)
			for i := 0; i < limit; i++ {
				swl.Allow(key, rule)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				swl.Allow(key, rule)
			}
		})
	}
}

func BenchmarkSlidingWindowLogPeekAllExpired(b *testing.B) {
	for _, limit := range []int{10, 100, 1000} {
		b.Run("limit-"+strconv.Itoa(limit), func(b *testing.B) {
			swl := NewSlidingWindowLog(engine.NewEngine(1024))
			rule := Rule{Name: "swl", Limit: limit, WindowSec: 1}
			key := "swl:expired:" + strconv.Itoa(limit)
			for i := 0; i < limit; i++ {
				swl.Allow(key, rule)
			}
			time.Sleep(1100 * time.Millisecond)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				swl.Peek(key, rule)
			}
		})
	}
}
