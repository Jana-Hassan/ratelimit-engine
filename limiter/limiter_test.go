package limiter

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Jana-Hassan/ratelimit-engine/algorithms"
	"github.com/Jana-Hassan/ratelimit-engine/engine"
)

type decision struct {
	algorithm string
	rule      string
	mode      string
	allowed   bool
	elapsed   time.Duration
}

type spyRecorder struct {
	mu        sync.Mutex
	decisions []decision
}

func (r *spyRecorder) RecordDecision(algorithm string, rule string, mode string, allowed bool, elapsed time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.decisions = append(r.decisions, decision{algorithm, rule, mode, allowed, elapsed})
}

func (r *spyRecorder) last() decision {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.decisions[len(r.decisions)-1]
}

func (r *spyRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.decisions)
}

func testLimiter(t *testing.T, recorder Recorder, rules ...Rule) *Limiter {
	t.Helper()
	l := New(engine.NewEngine(256), nil, recorder)
	for _, rule := range rules {
		if err := l.Rules().Set(rule); err != nil {
			t.Fatalf("Set(%s) failed: %v", rule.Name, err)
		}
	}
	return l
}

func rule(name string, algorithm string, limit int) Rule {
	return Rule{
		Rule:      algorithms.Rule{Name: name, Limit: limit, WindowSec: 3600},
		Algorithm: algorithm,
	}
}

func TestLimiterCheckUnknownRule(t *testing.T) {
	l := testLimiter(t, nil)

	_, _, err := l.Check("nope", "client")
	if !errors.Is(err, ErrRuleNotFound) {
		t.Fatalf("Check error = %v, want ErrRuleNotFound", err)
	}
	if _, _, err := l.Peek("nope", "client"); !errors.Is(err, ErrRuleNotFound) {
		t.Fatalf("Peek error = %v, want ErrRuleNotFound", err)
	}
}

func TestLimiterUnknownAlgorithm(t *testing.T) {
	l := testLimiter(t, nil, rule("r", "leaky_bucket", 5))

	_, _, err := l.Check("r", "client")
	if !errors.Is(err, ErrAlgorithmNotFound) {
		t.Fatalf("Check error = %v, want ErrAlgorithmNotFound", err)
	}
	if _, _, err := l.Peek("r", "client"); !errors.Is(err, ErrAlgorithmNotFound) {
		t.Fatalf("Peek error = %v, want ErrAlgorithmNotFound", err)
	}
}

func TestLimiterHasAlgorithm(t *testing.T) {
	l := testLimiter(t, nil)
	for _, name := range []string{"fixed_window", "sliding_window", "sliding_window_log", "token_bucket"} {
		if !l.HasAlgorithm(name) {
			t.Fatalf("HasAlgorithm(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"", "leaky_bucket", "Fixed_Window"} {
		if l.HasAlgorithm(name) {
			t.Fatalf("HasAlgorithm(%q) = true, want false", name)
		}
	}
}

func TestLimiterCheckEnforcesLimitPerAlgorithm(t *testing.T) {
	for _, name := range []string{"fixed_window", "sliding_window", "sliding_window_log", "token_bucket"} {
		t.Run(name, func(t *testing.T) {
			l := testLimiter(t, nil, rule("r", name, 3))

			for i := 0; i < 3; i++ {
				_, result, err := l.Check("r", "client")
				if err != nil {
					t.Fatalf("Check failed: %v", err)
				}
				if !result.Allowed {
					t.Fatalf("call %d denied", i+1)
				}
			}
			_, result, err := l.Check("r", "client")
			if err != nil {
				t.Fatalf("Check failed: %v", err)
			}
			if result.Allowed {
				t.Fatal("call over the limit was allowed")
			}
		})
	}
}

func TestLimiterCheckReturnsTheRule(t *testing.T) {
	want := rule("posts", "token_bucket", 50)
	l := testLimiter(t, nil, want)

	got, _, err := l.Check("posts", "client")
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if got != want {
		t.Fatalf("Check returned rule %+v, want %+v", got, want)
	}
}

func TestLimiterPeekDoesNotConsume(t *testing.T) {
	for _, name := range []string{"fixed_window", "sliding_window", "sliding_window_log", "token_bucket"} {
		t.Run(name, func(t *testing.T) {
			l := testLimiter(t, nil, rule("r", name, 2))

			for i := 0; i < 20; i++ {
				_, result, err := l.Peek("r", "client")
				if err != nil {
					t.Fatalf("Peek failed: %v", err)
				}
				if !result.Allowed {
					t.Fatalf("Peek %d denied; Peek consumed quota", i+1)
				}
			}
			for i := 0; i < 2; i++ {
				if _, result, _ := l.Check("r", "client"); !result.Allowed {
					t.Fatalf("Check %d denied after peeking", i+1)
				}
			}
		})
	}
}

func TestLimiterClientsAreIsolated(t *testing.T) {
	l := testLimiter(t, nil, rule("r", "fixed_window", 2))

	l.Check("r", "alice")
	l.Check("r", "alice")
	if _, result, _ := l.Check("r", "alice"); result.Allowed {
		t.Fatal("alice should be exhausted")
	}
	if _, result, _ := l.Check("r", "bob"); !result.Allowed {
		t.Fatal("bob was affected by another client")
	}
}

func TestLimiterRulesAreIsolated(t *testing.T) {
	l := testLimiter(t, nil, rule("posts", "fixed_window", 1), rule("logins", "fixed_window", 1))

	if _, result, _ := l.Check("posts", "alice"); !result.Allowed {
		t.Fatal("first posts call denied")
	}
	if _, result, _ := l.Check("posts", "alice"); result.Allowed {
		t.Fatal("second posts call allowed")
	}
	if _, result, _ := l.Check("logins", "alice"); !result.Allowed {
		t.Fatal("logins shares a counter with posts")
	}
}

func TestLimiterCacheKeysDoNotCollideAcrossColon(t *testing.T) {
	l := testLimiter(t, nil, rule("a", "fixed_window", 1), rule("a:b", "fixed_window", 1))

	if _, result, _ := l.Check("a", "b:c"); !result.Allowed {
		t.Fatal("first call denied")
	}
	_, result, err := l.Check("a:b", "c")
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if !result.Allowed {
		t.Fatal("rule a + client b:c and rule a:b + client c share a counter")
	}
}

func TestCacheKeyIsUnambiguous(t *testing.T) {
	pairs := [][2]string{
		{"a", "b:c"},
		{"a:b", "c"},
		{"", "a:b:c"},
		{"a:b:c", ""},
		{"1", "0:x"},
		{"10", ":x"},
	}

	seen := map[string][2]string{}
	for _, pair := range pairs {
		key := cacheKey(pair[0], pair[1])
		if previous, found := seen[key]; found {
			t.Fatalf("cacheKey%v and cacheKey%v both produced %q", previous, pair, key)
		}
		seen[key] = pair
	}
}

func TestLimiterRecordsDecisions(t *testing.T) {
	recorder := &spyRecorder{}
	l := testLimiter(t, recorder, rule("posts", "token_bucket", 1))

	l.Check("posts", "alice")
	got := recorder.last()
	if got.algorithm != "token_bucket" || got.rule != "posts" || got.mode != "check" || !got.allowed {
		t.Fatalf("check decision = %+v", got)
	}

	l.Check("posts", "alice")
	if got := recorder.last(); got.allowed {
		t.Fatalf("denied call recorded as allowed: %+v", got)
	}

	l.Peek("posts", "alice")
	if got := recorder.last(); got.mode != "peek" {
		t.Fatalf("peek mode = %q, want peek", got.mode)
	}

	if recorder.count() != 3 {
		t.Fatalf("recorded %d decisions, want 3", recorder.count())
	}
}

func TestLimiterDoesNotRecordResolveFailures(t *testing.T) {
	recorder := &spyRecorder{}
	l := testLimiter(t, recorder, rule("bad", "leaky_bucket", 1))

	l.Check("missing", "alice")
	l.Check("bad", "alice")

	if recorder.count() != 0 {
		t.Fatalf("recorded %d decisions for failed lookups, want 0", recorder.count())
	}
}

func TestLimiterNilRecorderIsSafe(t *testing.T) {
	l := New(engine.NewEngine(64), nil, nil)
	if err := l.Rules().Set(rule("r", "fixed_window", 1)); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	if _, _, err := l.Check("r", "alice"); err != nil {
		t.Fatalf("Check with a nil recorder failed: %v", err)
	}
	if _, _, err := l.Peek("r", "alice"); err != nil {
		t.Fatalf("Peek with a nil recorder failed: %v", err)
	}
}

func TestLimiterNilRepositoryIsSafe(t *testing.T) {
	l := New(engine.NewEngine(64), nil, nil)
	if _, err := l.Rules().Load(); err != nil {
		t.Fatalf("Load with a nil repository failed: %v", err)
	}
}

func TestLimiterConcurrentCheckHoldsTheLimit(t *testing.T) {
	l := testLimiter(t, &spyRecorder{}, rule("r", "sliding_window_log", 100))

	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := 0
			for j := 0; j < 10; j++ {
				if _, result, _ := l.Check("r", "shared"); result.Allowed {
					local++
				}
			}
			mu.Lock()
			allowed += local
			mu.Unlock()
		}()
	}
	wg.Wait()

	if allowed != 100 {
		t.Fatalf("allowed %d of 500 concurrent calls, want exactly 100", allowed)
	}
}
