package algorithms

import (
	"testing"
	"time"

	"github.com/Jana-Hassan/ratelimit-engine/engine"
)

func newFixedWindow(clk Clock) *FixedWindow {
	fw := NewFixedWindow(engine.NewEngine(64))
	fw.clock = clk
	return fw
}

func TestFixedWindowResetInTracksEpochBoundary(t *testing.T) {
	clk := newFakeClock()
	fw := newFixedWindow(clk)
	rule := Rule{Name: "r", Limit: 5, WindowSec: 60}

	result := fw.Allow("k", rule)
	if want := time.Duration(baseUnix%60) * time.Second; result.ResetIn != 60*time.Second-want {
		t.Fatalf("ResetIn = %v, want %v", result.ResetIn, 60*time.Second-want)
	}

	clk.Advance(10 * time.Second)
	if result := fw.Allow("k", rule); result.ResetIn != 30*time.Second {
		t.Fatalf("ResetIn after 10s = %v, want 30s", result.ResetIn)
	}
}

func TestFixedWindowResetsAtBoundary(t *testing.T) {
	clk := newFakeClock()
	fw := newFixedWindow(clk)
	rule := Rule{Name: "r", Limit: 3, WindowSec: 60}

	for i := 0; i < rule.Limit; i++ {
		fw.Allow("k", rule)
	}
	denied := fw.Allow("k", rule)
	if denied.Allowed {
		t.Fatal("expected denial after exhausting the window")
	}

	clk.Advance(denied.ResetIn - time.Nanosecond)
	if fw.Allow("k", rule).Allowed {
		t.Fatal("allowed one tick before the boundary")
	}

	clk.Advance(time.Nanosecond)
	result := fw.Allow("k", rule)
	if !result.Allowed {
		t.Fatal("denied at the start of a fresh window")
	}
	if result.Remaining != rule.Limit-1 {
		t.Fatalf("remaining = %d, want %d, counter did not reset", result.Remaining, rule.Limit-1)
	}
}

func TestFixedWindowStaleWindowIsDiscarded(t *testing.T) {
	clk := newFakeClock()
	fw := newFixedWindow(clk)
	rule := Rule{Name: "r", Limit: 3, WindowSec: 60}

	for i := 0; i < rule.Limit; i++ {
		fw.Allow("k", rule)
	}

	clk.Advance(10 * time.Minute)
	if result := fw.Peek("k", rule); !result.Allowed || result.Remaining != rule.Limit {
		t.Fatalf("Peek after a long gap = %+v, want full quota", result)
	}
}

func TestFixedWindowBoundaryBurstIsTwiceTheLimit(t *testing.T) {
	clk := newFakeClock()
	fw := newFixedWindow(clk)
	rule := Rule{Name: "r", Limit: 5, WindowSec: 60}

	allowed := 0
	for i := 0; i < rule.Limit; i++ {
		if fw.Allow("k", rule).Allowed {
			allowed++
		}
	}

	clk.Advance(time.Duration(60-baseUnix%60) * time.Second)
	for i := 0; i < rule.Limit; i++ {
		if fw.Allow("k", rule).Allowed {
			allowed++
		}
	}

	if allowed != 2*rule.Limit {
		t.Fatalf("boundary burst allowed %d, want %d", allowed, 2*rule.Limit)
	}
}
