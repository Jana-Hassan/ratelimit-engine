package algorithms

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Jana-Hassan/ratelimit-engine/engine"
)

type factory struct {
	name string
	make func(e *engine.Engine, c Clock) Algorithm
}

func factories() []factory {
	return []factory{
		{"fixed_window", func(e *engine.Engine, c Clock) Algorithm {
			a := NewFixedWindow(e)
			a.clock = c
			return a
		}},
		{"sliding_window", func(e *engine.Engine, c Clock) Algorithm {
			a := NewSlidingWindow(e)
			a.clock = c
			return a
		}},
		{"sliding_window_log", func(e *engine.Engine, c Clock) Algorithm {
			a := NewSlidingWindowLog(e)
			a.clock = c
			return a
		}},
		{"token_bucket", func(e *engine.Engine, c Clock) Algorithm {
			a := NewTokenBucket(e)
			a.clock = c
			return a
		}},
	}
}

func newAlgorithm(t *testing.T, f factory) (Algorithm, *fakeClock) {
	t.Helper()
	clk := newFakeClock()
	return f.make(engine.NewEngine(64), clk), clk
}

func TestContractInvalidRuleDenies(t *testing.T) {
	invalid := []Rule{
		{Name: "r", Limit: 0, WindowSec: 60},
		{Name: "r", Limit: -1, WindowSec: 60},
		{Name: "r", Limit: 5, WindowSec: 0},
		{Name: "r", Limit: 5, WindowSec: -1},
	}
	for _, f := range factories() {
		t.Run(f.name, func(t *testing.T) {
			for _, rule := range invalid {
				e := engine.NewEngine(64)
				algorithm := f.make(e, newFakeClock())

				if algorithm.Allow("k", rule).Allowed {
					t.Fatalf("Allow allowed invalid rule %+v", rule)
				}
				if algorithm.Peek("k", rule).Allowed {
					t.Fatalf("Peek allowed invalid rule %+v", rule)
				}
				if _, found := e.Get("k"); found {
					t.Fatalf("invalid rule %+v wrote state to the engine", rule)
				}
			}
		})
	}
}

func TestContractAllowsUpToLimitThenDenies(t *testing.T) {
	rule := Rule{Name: "r", Limit: 5, WindowSec: 60}
	for _, f := range factories() {
		t.Run(f.name, func(t *testing.T) {
			algorithm, _ := newAlgorithm(t, f)

			for i := 0; i < rule.Limit; i++ {
				result := algorithm.Allow("k", rule)
				if !result.Allowed {
					t.Fatalf("call %d denied, want allowed", i+1)
				}
				if want := rule.Limit - (i + 1); result.Remaining != want {
					t.Fatalf("call %d remaining = %d, want %d", i+1, result.Remaining, want)
				}
				if result.RetryIn != 0 {
					t.Fatalf("call %d allowed but RetryIn = %v, want 0", i+1, result.RetryIn)
				}
			}

			result := algorithm.Allow("k", rule)
			if result.Allowed {
				t.Fatal("call over the limit was allowed")
			}
			if result.Remaining != 0 {
				t.Fatalf("denied remaining = %d, want 0", result.Remaining)
			}
			if result.RetryIn <= 0 {
				t.Fatalf("denied RetryIn = %v, want > 0", result.RetryIn)
			}
		})
	}
}

func TestContractDenialStaysDenied(t *testing.T) {
	rule := Rule{Name: "r", Limit: 2, WindowSec: 60}
	for _, f := range factories() {
		t.Run(f.name, func(t *testing.T) {
			algorithm, _ := newAlgorithm(t, f)
			for i := 0; i < rule.Limit; i++ {
				algorithm.Allow("k", rule)
			}
			for i := 0; i < 20; i++ {
				if algorithm.Allow("k", rule).Allowed {
					t.Fatalf("denied call %d slipped through", i+1)
				}
			}
		})
	}
}

func TestContractPeekDoesNotMutate(t *testing.T) {
	rule := Rule{Name: "r", Limit: 5, WindowSec: 60}
	for _, f := range factories() {
		t.Run(f.name, func(t *testing.T) {
			e := engine.NewEngine(64)
			algorithm := f.make(e, newFakeClock())

			algorithm.Allow("k", rule)
			algorithm.Allow("k", rule)
			before, _ := e.Get("k")

			for i := 0; i < 50; i++ {
				algorithm.Peek("k", rule)
			}

			after, found := e.Get("k")
			if !found {
				t.Fatal("Peek removed the key")
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("Peek mutated state: before %+v, after %+v", before, after)
			}
			for i := 0; i < 3; i++ {
				if !algorithm.Allow("k", rule).Allowed {
					t.Fatalf("call %d after peeking denied, Peek consumed quota", i+1)
				}
			}
		})
	}
}

func TestContractPeekPredictsNextAllow(t *testing.T) {
	rule := Rule{Name: "r", Limit: 3, WindowSec: 60}
	for _, f := range factories() {
		t.Run(f.name, func(t *testing.T) {
			algorithm, _ := newAlgorithm(t, f)
			for i := 0; i < rule.Limit+3; i++ {
				peeked := algorithm.Peek("k", rule)
				allowed := algorithm.Allow("k", rule)
				if peeked.Allowed != allowed.Allowed {
					t.Fatalf("call %d: Peek said allowed=%v, Allow said %v", i+1, peeked.Allowed, allowed.Allowed)
				}
				if peeked.Remaining != allowed.Remaining+boolToInt(allowed.Allowed) {
					t.Fatalf("call %d: Peek remaining %d inconsistent with Allow remaining %d", i+1, peeked.Remaining, allowed.Remaining)
				}
			}
		})
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestContractPeekOnUnknownKeyIsFull(t *testing.T) {
	rule := Rule{Name: "r", Limit: 7, WindowSec: 60}
	for _, f := range factories() {
		t.Run(f.name, func(t *testing.T) {
			algorithm, _ := newAlgorithm(t, f)
			result := algorithm.Peek("never-seen", rule)
			if !result.Allowed {
				t.Fatal("Peek on an unknown key denied")
			}
			if result.Remaining != rule.Limit {
				t.Fatalf("Peek remaining = %d, want %d", result.Remaining, rule.Limit)
			}
		})
	}
}

func TestContractKeysAreIsolated(t *testing.T) {
	rule := Rule{Name: "r", Limit: 2, WindowSec: 60}
	for _, f := range factories() {
		t.Run(f.name, func(t *testing.T) {
			algorithm, _ := newAlgorithm(t, f)
			for i := 0; i < rule.Limit; i++ {
				algorithm.Allow("a", rule)
			}
			if algorithm.Allow("a", rule).Allowed {
				t.Fatal("key a should be exhausted")
			}
			for i := 0; i < rule.Limit; i++ {
				if !algorithm.Allow("b", rule).Allowed {
					t.Fatalf("key b call %d denied, keys are not isolated", i+1)
				}
			}
		})
	}
}

func TestContractCorruptStateDoesNotPanic(t *testing.T) {
	rule := Rule{Name: "r", Limit: 3, WindowSec: 60}
	for _, f := range factories() {
		t.Run(f.name, func(t *testing.T) {
			e := engine.NewEngine(64)
			algorithm := f.make(e, newFakeClock())
			e.Set("k", "not a state struct")

			if !algorithm.Peek("k", rule).Allowed {
				t.Fatal("Peek over corrupt state denied instead of falling back to fresh")
			}
			if !algorithm.Allow("k", rule).Allowed {
				t.Fatal("Allow over corrupt state denied instead of falling back to fresh")
			}
		})
	}
}

func TestContractConcurrentAllowNeverExceedsLimit(t *testing.T) {
	rule := Rule{Name: "r", Limit: 100, WindowSec: 60}
	for _, f := range factories() {
		t.Run(f.name, func(t *testing.T) {
			algorithm, _ := newAlgorithm(t, f)

			const goroutines = 50
			const perGoroutine = 10

			var wg sync.WaitGroup
			var mu sync.Mutex
			allowed := 0

			for i := 0; i < goroutines; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					local := 0
					for j := 0; j < perGoroutine; j++ {
						if algorithm.Allow("k", rule).Allowed {
							local++
						}
					}
					mu.Lock()
					allowed += local
					mu.Unlock()
				}()
			}
			wg.Wait()

			if allowed != rule.Limit {
				t.Fatalf("allowed %d of %d attempts, want exactly %d", allowed, goroutines*perGoroutine, rule.Limit)
			}
		})
	}
}

func TestContractResetInWithinWindow(t *testing.T) {
	rule := Rule{Name: "r", Limit: 3, WindowSec: 60}
	window := time.Duration(rule.WindowSec) * time.Second
	for _, f := range factories() {
		t.Run(f.name, func(t *testing.T) {
			algorithm, _ := newAlgorithm(t, f)
			result := algorithm.Allow("k", rule)
			if result.ResetIn <= 0 || result.ResetIn > window {
				t.Fatalf("ResetIn = %v, want within (0, %v]", result.ResetIn, window)
			}
		})
	}
}
