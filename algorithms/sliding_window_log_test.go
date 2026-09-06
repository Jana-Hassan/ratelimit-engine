package algorithms

import (
	"testing"
	"time"

	"github.com/Jana-Hassan/ratelimit-engine/engine"
)

func newSlidingWindowLog(clk Clock) *SlidingWindowLog {
	swl := NewSlidingWindowLog(engine.NewEngine(64))
	swl.clock = clk
	return swl
}

func TestSlidingWindowLogRollsExactly(t *testing.T) {
	clk := newFakeClock()
	swl := newSlidingWindowLog(clk)
	rule := Rule{Name: "r", Limit: 3, WindowSec: 60}

	for i := 0; i < rule.Limit; i++ {
		if !swl.Allow("k", rule).Allowed {
			t.Fatalf("call %d denied", i+1)
		}
	}

	denied := swl.Allow("k", rule)
	if denied.Allowed {
		t.Fatal("expected denial")
	}
	if denied.RetryIn != 60*time.Second {
		t.Fatalf("RetryIn = %v, want 60s (oldest stamp + window)", denied.RetryIn)
	}

	clk.Advance(60*time.Second - time.Nanosecond)
	if swl.Allow("k", rule).Allowed {
		t.Fatal("allowed one nanosecond early")
	}

	clk.Advance(time.Nanosecond)
	for i := 0; i < rule.Limit; i++ {
		if !swl.Allow("k", rule).Allowed {
			t.Fatalf("call %d after the window rolled was denied", i+1)
		}
	}
}

func TestSlidingWindowLogSteadyDripNeverDenied(t *testing.T) {
	clk := newFakeClock()
	swl := newSlidingWindowLog(clk)
	rule := Rule{Name: "r", Limit: 3, WindowSec: 60}

	for i := 0; i < 50; i++ {
		if !swl.Allow("k", rule).Allowed {
			t.Fatalf("drip call %d denied; a steady rate under the limit must pass", i+1)
		}
		clk.Advance(21 * time.Second)
	}
}

func TestSlidingWindowLogRingSurvivesWraparound(t *testing.T) {
	clk := newFakeClock()
	swl := newSlidingWindowLog(clk)
	rule := Rule{Name: "r", Limit: 3, WindowSec: 60}

	for cycle := 0; cycle < 20; cycle++ {
		for i := 0; i < rule.Limit; i++ {
			if !swl.Allow("k", rule).Allowed {
				t.Fatalf("cycle %d call %d denied", cycle, i+1)
			}
		}
		if swl.Allow("k", rule).Allowed {
			t.Fatalf("cycle %d allowed past the limit", cycle)
		}
		clk.Advance(60 * time.Second)
	}
}

func TestSlidingWindowLogResetsWhenLimitChanges(t *testing.T) {
	clk := newFakeClock()
	swl := newSlidingWindowLog(clk)
	small := Rule{Name: "r", Limit: 3, WindowSec: 60}
	large := Rule{Name: "r", Limit: 5, WindowSec: 60}

	for i := 0; i < small.Limit; i++ {
		swl.Allow("k", small)
	}
	if swl.Allow("k", small).Allowed {
		t.Fatal("expected denial at the small limit")
	}

	allowed := 0
	for i := 0; i < large.Limit; i++ {
		if swl.Allow("k", large).Allowed {
			allowed++
		}
	}
	if allowed != large.Limit {
		t.Fatalf("after the limit changed allowed %d, want %d (ring resized, history dropped)", allowed, large.Limit)
	}

	if swl.Peek("k", small).Remaining != small.Limit {
		t.Fatal("Peek with the old limit should see a mismatched ring and report full quota")
	}
}

func TestSlidingWindowLogPeekCountsExpiryLikeAllow(t *testing.T) {
	clk := newFakeClock()
	swl := newSlidingWindowLog(clk)
	rule := Rule{Name: "r", Limit: 5, WindowSec: 60}

	swl.Allow("k", rule)
	clk.Advance(10 * time.Second)
	swl.Allow("k", rule)
	clk.Advance(10 * time.Second)
	swl.Allow("k", rule)

	clk.Advance(45 * time.Second)

	peeked := swl.Peek("k", rule)
	if peeked.Remaining != 3 {
		t.Fatalf("Peek remaining = %d, want 3 (one of three stamps expired)", peeked.Remaining)
	}
	if peeked.ResetIn != 15*time.Second {
		t.Fatalf("Peek ResetIn = %v, want 15s (newest stamp + window)", peeked.ResetIn)
	}

	allowed := swl.Allow("k", rule)
	if !allowed.Allowed || allowed.Remaining != 2 {
		t.Fatalf("Allow = %+v, want allowed with remaining 2; prune and expiredCount disagree", allowed)
	}
}

func TestSlidingWindowLogPeekAgreesWithAllowAcrossPartialExpiry(t *testing.T) {
	clk := newFakeClock()
	swl := newSlidingWindowLog(clk)
	rule := Rule{Name: "r", Limit: 8, WindowSec: 60}

	for i := 0; i < rule.Limit; i++ {
		swl.Allow("k", rule)
		clk.Advance(7 * time.Second)
	}

	for step := 0; step < 20; step++ {
		peeked := swl.Peek("k", rule)
		allowed := swl.Allow("k", rule)
		if peeked.Allowed != allowed.Allowed {
			t.Fatalf("step %d: Peek allowed=%v, Allow allowed=%v", step, peeked.Allowed, allowed.Allowed)
		}
		if peeked.Remaining != allowed.Remaining+boolToInt(allowed.Allowed) {
			t.Fatalf("step %d: Peek remaining %d, Allow remaining %d", step, peeked.Remaining, allowed.Remaining)
		}
		clk.Advance(5 * time.Second)
	}
}

func TestSlidingWindowLogRetryInPointsAtOldestStamp(t *testing.T) {
	clk := newFakeClock()
	swl := newSlidingWindowLog(clk)
	rule := Rule{Name: "r", Limit: 2, WindowSec: 60}

	swl.Allow("k", rule)
	clk.Advance(20 * time.Second)
	swl.Allow("k", rule)

	denied := swl.Allow("k", rule)
	if denied.Allowed {
		t.Fatal("expected denial")
	}
	if denied.RetryIn != 40*time.Second {
		t.Fatalf("RetryIn = %v, want 40s", denied.RetryIn)
	}

	clk.Advance(denied.RetryIn)
	if !swl.Allow("k", rule).Allowed {
		t.Fatal("RetryIn elapsed but the call was still denied")
	}
}
