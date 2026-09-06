package algorithms

import (
	"testing"
	"time"

	"github.com/Jana-Hassan/ratelimit-engine/engine"
)

func newTokenBucket(clk Clock) *TokenBucket {
	tb := NewTokenBucket(engine.NewEngine(64))
	tb.clock = clk
	return tb
}

func drainBucket(t *testing.T, tb *TokenBucket, key string, rule Rule) {
	t.Helper()
	for i := 0; i < rule.Limit; i++ {
		if !tb.Allow(key, rule).Allowed {
			t.Fatalf("drain call %d denied", i+1)
		}
	}
	if tb.Allow(key, rule).Allowed {
		t.Fatal("bucket was not empty after draining")
	}
}

func TestTokenBucketStartsFull(t *testing.T) {
	tb := newTokenBucket(newFakeClock())
	rule := Rule{Name: "r", Limit: 5, WindowSec: 60}

	if result := tb.Peek("fresh", rule); !result.Allowed || result.Remaining != rule.Limit {
		t.Fatalf("Peek on a fresh bucket = %+v, want full", result)
	}
	drainBucket(t, tb, "k", rule)
}

func TestTokenBucketRefillsAtRate(t *testing.T) {
	clk := newFakeClock()
	tb := newTokenBucket(clk)
	rule := Rule{Name: "r", Limit: 5, WindowSec: 60}

	drainBucket(t, tb, "k", rule)
	clk.Advance(30 * time.Second)

	allowed := 0
	for i := 0; i < rule.Limit; i++ {
		if tb.Allow("k", rule).Allowed {
			allowed++
		}
	}
	if allowed != 2 {
		t.Fatalf("after half a window allowed %d, want 2 (2.5 tokens refilled)", allowed)
	}
}

func TestTokenBucketClampsAtCapacity(t *testing.T) {
	clk := newFakeClock()
	tb := newTokenBucket(clk)
	rule := Rule{Name: "r", Limit: 5, WindowSec: 60}

	drainBucket(t, tb, "k", rule)
	clk.Advance(3 * time.Hour)

	allowed := 0
	for i := 0; i < 3*rule.Limit; i++ {
		if tb.Allow("k", rule).Allowed {
			allowed++
		}
	}
	if allowed != rule.Limit {
		t.Fatalf("after a long idle allowed %d, want %d; tokens accumulated past capacity", allowed, rule.Limit)
	}
}

func TestTokenBucketPartialTokenIsNotSpendable(t *testing.T) {
	clk := newFakeClock()
	tb := newTokenBucket(clk)
	rule := Rule{Name: "r", Limit: 5, WindowSec: 60}

	drainBucket(t, tb, "k", rule)
	clk.Advance(6 * time.Second)

	result := tb.Allow("k", rule)
	if result.Allowed {
		t.Fatal("half a token should not be spendable")
	}
	if result.Remaining != 0 {
		t.Fatalf("remaining = %d, want 0", result.Remaining)
	}
	if result.RetryIn != 6*time.Second {
		t.Fatalf("RetryIn = %v, want 6s to earn the missing half token", result.RetryIn)
	}

	clk.Advance(result.RetryIn)
	if !tb.Allow("k", rule).Allowed {
		t.Fatal("RetryIn elapsed but the call was still denied")
	}
}

func TestTokenBucketPeekRefillsWithoutConsuming(t *testing.T) {
	clk := newFakeClock()
	tb := newTokenBucket(clk)
	rule := Rule{Name: "r", Limit: 5, WindowSec: 60}

	drainBucket(t, tb, "k", rule)
	clk.Advance(24 * time.Second)

	first := tb.Peek("k", rule)
	if first.Remaining != 2 {
		t.Fatalf("Peek remaining = %d, want 2", first.Remaining)
	}
	for i := 0; i < 10; i++ {
		if got := tb.Peek("k", rule).Remaining; got != 2 {
			t.Fatalf("repeated Peek remaining = %d, want 2; Peek is consuming", got)
		}
	}

	allowed := 0
	for i := 0; i < rule.Limit; i++ {
		if tb.Allow("k", rule).Allowed {
			allowed++
		}
	}
	if allowed != 2 {
		t.Fatalf("allowed %d after peeking, want 2", allowed)
	}
}

func TestTokenBucketResetInReachesCapacity(t *testing.T) {
	clk := newFakeClock()
	tb := newTokenBucket(clk)
	rule := Rule{Name: "r", Limit: 5, WindowSec: 60}

	drainBucket(t, tb, "k", rule)

	result := tb.Peek("k", rule)
	if result.ResetIn != 60*time.Second {
		t.Fatalf("ResetIn on an empty bucket = %v, want a full window", result.ResetIn)
	}

	clk.Advance(result.ResetIn)
	if got := tb.Peek("k", rule); got.Remaining != rule.Limit || got.ResetIn != 0 {
		t.Fatalf("after ResetIn elapsed = %+v, want full bucket with ResetIn 0", got)
	}
}
