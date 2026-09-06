package algorithms

import (
	"testing"
	"time"

	"github.com/Jana-Hassan/ratelimit-engine/engine"
)

func newSlidingWindow(clk Clock) *SlidingWindow {
	sw := NewSlidingWindow(engine.NewEngine(64))
	sw.clock = clk
	return sw
}

func advanceToNextWindowStart(clk *fakeClock, windowSec int64) {
	now := clk.Now().Unix()
	clk.Advance(time.Duration(windowSec-now%windowSec) * time.Second)
}

func TestSlidingWindowCarriesPreviousWindowAtFullWeight(t *testing.T) {
	clk := newFakeClock()
	sw := newSlidingWindow(clk)
	rule := Rule{Name: "r", Limit: 10, WindowSec: 60}

	for i := 0; i < rule.Limit; i++ {
		sw.Allow("k", rule)
	}

	advanceToNextWindowStart(clk, 60)
	if sw.Allow("k", rule).Allowed {
		t.Fatal("allowed at the start of the next window; previous count was not carried")
	}
}

func TestSlidingWindowQuotaDecaysWithWeight(t *testing.T) {
	clk := newFakeClock()
	sw := newSlidingWindow(clk)
	rule := Rule{Name: "r", Limit: 10, WindowSec: 60}

	for i := 0; i < rule.Limit; i++ {
		sw.Allow("k", rule)
	}

	advanceToNextWindowStart(clk, 60)
	clk.Advance(30 * time.Second)

	allowed := 0
	for i := 0; i < rule.Limit; i++ {
		if sw.Allow("k", rule).Allowed {
			allowed++
		}
	}
	if allowed != 5 {
		t.Fatalf("at half weight allowed %d, want 5 (10*0.5 carried)", allowed)
	}
}

func TestSlidingWindowDropsCountAfterFullGap(t *testing.T) {
	clk := newFakeClock()
	sw := newSlidingWindow(clk)
	rule := Rule{Name: "r", Limit: 10, WindowSec: 60}

	for i := 0; i < rule.Limit; i++ {
		sw.Allow("k", rule)
	}

	advanceToNextWindowStart(clk, 60)
	clk.Advance(60 * time.Second)

	allowed := 0
	for i := 0; i < rule.Limit; i++ {
		if sw.Allow("k", rule).Allowed {
			allowed++
		}
	}
	if allowed != rule.Limit {
		t.Fatalf("after a two-window gap allowed %d, want %d", allowed, rule.Limit)
	}
}

func TestSlidingWindowIsStricterThanFixedWindowAcrossBoundary(t *testing.T) {
	clk := newFakeClock()
	sw := newSlidingWindow(clk)
	fw := newFixedWindow(clk)
	rule := Rule{Name: "r", Limit: 10, WindowSec: 60}

	drain := func(a Algorithm) int {
		allowed := 0
		for i := 0; i < rule.Limit; i++ {
			if a.Allow("k", rule).Allowed {
				allowed++
			}
		}
		return allowed
	}

	drain(sw)
	drain(fw)

	advanceToNextWindowStart(clk, 60)

	slidingAllowed := drain(sw)
	fixedAllowed := drain(fw)

	if fixedAllowed != rule.Limit {
		t.Fatalf("fixed window allowed %d across the boundary, want %d", fixedAllowed, rule.Limit)
	}
	if slidingAllowed >= fixedAllowed {
		t.Fatalf("sliding allowed %d, fixed allowed %d; sliding must be stricter", slidingAllowed, fixedAllowed)
	}
}

func TestSlidingWindowRetryInUsesFullWindowWithoutHistory(t *testing.T) {
	clk := newFakeClock()
	sw := newSlidingWindow(clk)
	rule := Rule{Name: "r", Limit: 3, WindowSec: 60}

	for i := 0; i < rule.Limit; i++ {
		sw.Allow("k", rule)
	}

	result := sw.Allow("k", rule)
	if result.Allowed {
		t.Fatal("expected denial")
	}
	if result.RetryIn != result.ResetIn {
		t.Fatalf("RetryIn = %v, want ResetIn %v when there is no previous window", result.RetryIn, result.ResetIn)
	}
}

func TestSlidingWindowRetryInUsesCarriedCount(t *testing.T) {
	clk := newFakeClock()
	sw := newSlidingWindow(clk)
	rule := Rule{Name: "r", Limit: 10, WindowSec: 60}

	for i := 0; i < rule.Limit; i++ {
		sw.Allow("k", rule)
	}

	advanceToNextWindowStart(clk, 60)
	clk.Advance(15 * time.Second)

	allowed := 0
	var denied Result
	for i := 0; i < rule.Limit; i++ {
		result := sw.Allow("k", rule)
		if result.Allowed {
			allowed++
			continue
		}
		denied = result
		break
	}

	if allowed != 3 {
		t.Fatalf("at 0.75 weight allowed %d, want 3", allowed)
	}
	if denied.RetryIn != 3*time.Second {
		t.Fatalf("RetryIn = %v, want 3s", denied.RetryIn)
	}
	if denied.RetryIn >= denied.ResetIn {
		t.Fatalf("RetryIn %v should be shorter than ResetIn %v when quota frees up mid-window", denied.RetryIn, denied.ResetIn)
	}
}

func TestSlidingWindowDeniesWithZeroRetryAtBoundary(t *testing.T) {
	clk := newFakeClock()
	sw := newSlidingWindow(clk)
	rule := Rule{Name: "r", Limit: 10, WindowSec: 60}

	for i := 0; i < rule.Limit; i++ {
		sw.Allow("k", rule)
	}

	advanceToNextWindowStart(clk, 60)
	result := sw.Allow("k", rule)

	if result.Allowed {
		t.Fatal("expected denial exactly at the boundary")
	}
	if result.RetryIn != 0 {
		t.Fatalf("RetryIn = %v, want 0; this pins current behavior", result.RetryIn)
	}

	clk.Advance(time.Second)
	if !sw.Allow("k", rule).Allowed {
		t.Fatal("one second past the boundary should be allowed")
	}
}

func TestSlidingWindowPeekMatchesAllowEstimate(t *testing.T) {
	clk := newFakeClock()
	sw := newSlidingWindow(clk)
	rule := Rule{Name: "r", Limit: 10, WindowSec: 60}

	for i := 0; i < 6; i++ {
		sw.Allow("k", rule)
	}

	advanceToNextWindowStart(clk, 60)
	clk.Advance(30 * time.Second)

	peeked := sw.Peek("k", rule)
	if peeked.Remaining != 7 {
		t.Fatalf("Peek remaining = %d, want 7 (10 - int(6*0.5))", peeked.Remaining)
	}
	allowed := sw.Allow("k", rule)
	if !allowed.Allowed || allowed.Remaining != 6 {
		t.Fatalf("Allow = %+v, want allowed with remaining 6", allowed)
	}
}
