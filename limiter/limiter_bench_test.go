package limiter

import (
	"strconv"
	"testing"
	"time"

	"github.com/Jana-Hassan/ratelimit-engine/algorithms"
	"github.com/Jana-Hassan/ratelimit-engine/engine"
)

var benchAlgorithmNames = []string{"fixed_window", "sliding_window", "sliding_window_log", "token_bucket"}

type countingRecorder struct {
	decisions int
}

func (r *countingRecorder) RecordDecision(algorithm string, rule string, mode string, allowed bool, elapsed time.Duration) {
	r.decisions++
}

func benchLimiter(recorder Recorder) *Limiter {
	l := New(engine.NewEngine(1024), nil, recorder)
	for _, name := range benchAlgorithmNames {
		l.Rules().Set(Rule{
			Rule:      algorithms.Rule{Name: name, Limit: 1000, WindowSec: 1},
			Algorithm: name,
		})
	}
	return l
}

func benchClients(count int) []string {
	clients := make([]string, count)
	for i := range clients {
		clients[i] = "client-" + strconv.Itoa(i)
	}
	return clients
}

func BenchmarkLimiterCheck(b *testing.B) {
	l := benchLimiter(nil)
	clients := benchClients(1 << 12)
	for _, name := range benchAlgorithmNames {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				l.Check(name, clients[i&(len(clients)-1)])
			}
		})
	}
}

func BenchmarkLimiterPeek(b *testing.B) {
	l := benchLimiter(nil)
	clients := benchClients(1 << 12)
	for _, name := range benchAlgorithmNames {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				l.Peek(name, clients[i&(len(clients)-1)])
			}
		})
	}
}

func BenchmarkLimiterRecorderOverhead(b *testing.B) {
	clients := benchClients(1 << 12)
	cases := map[string]Recorder{
		"nop":      nil,
		"counting": &countingRecorder{},
	}
	for name, recorder := range cases {
		b.Run(name, func(b *testing.B) {
			l := benchLimiter(recorder)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				l.Check("fixed_window", clients[i&(len(clients)-1)])
			}
		})
	}
}

func BenchmarkLimiterCheckParallel(b *testing.B) {
	l := benchLimiter(nil)
	clients := benchClients(1 << 12)
	for _, name := range benchAlgorithmNames {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					l.Check(name, clients[i&(len(clients)-1)])
					i++
				}
			})
		})
	}
}

func BenchmarkCacheKey(b *testing.B) {
	clients := benchClients(1 << 12)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cacheKey("fixed_window", clients[i&(len(clients)-1)])
	}
}
