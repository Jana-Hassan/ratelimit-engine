package algorithms

import (
	"time"

	"github.com/Jana-Hassan/ratelimit-engine/engine"
)

type tokenBucketState struct {
	tokens     float64
	lastRefill int64
}

type TokenBucket struct {
	engine *engine.Engine
}

func NewTokenBucket(e *engine.Engine) *TokenBucket {
	return &TokenBucket{engine: e}
}

func (state tokenBucketState) refill(now int64, capacity float64, rate float64) tokenBucketState {
	elapsed := float64(now-state.lastRefill) / float64(time.Second)
	state.tokens += elapsed * rate
	if state.tokens > capacity {
		state.tokens = capacity
	}
	state.lastRefill = now
	return state
}

func (state tokenBucketState) resetIn(capacity float64, rate float64) time.Duration {
	return time.Duration((capacity - state.tokens) / rate * float64(time.Second))
}

func (state tokenBucketState) retryIn(rate float64) time.Duration {
	if state.tokens >= 1 {
		return 0
	}
	return time.Duration((1 - state.tokens) / rate * float64(time.Second))
}

func restoreBucket(value interface{}, found bool, now int64, capacity float64, rate float64) tokenBucketState {
	if found {
		if previous, ok := value.(tokenBucketState); ok {
			return previous.refill(now, capacity, rate)
		}
	}
	return tokenBucketState{tokens: capacity, lastRefill: now}
}

func (tb *TokenBucket) Allow(key string, rule Rule) Result {
	if rule.WindowSec <= 0 || rule.Limit <= 0 {
		return Result{Allowed: false}
	}

	now := time.Now().UnixNano()
	capacity := float64(rule.Limit)
	rate := capacity / float64(rule.WindowSec)

	result := Result{}
	tb.engine.Update(key, func(value interface{}, found bool) interface{} {
		state := restoreBucket(value, found, now, capacity, rate)

		if state.tokens >= 1 {
			state.tokens--
			result.Allowed = true
			result.Remaining = int(state.tokens)
		} else {
			result.RetryIn = state.retryIn(rate)
		}
		result.ResetIn = state.resetIn(capacity, rate)
		return state
	})

	return result
}

func (tb *TokenBucket) Peek(key string, rule Rule) Result {
	if rule.WindowSec <= 0 || rule.Limit <= 0 {
		return Result{Allowed: false}
	}

	now := time.Now().UnixNano()
	capacity := float64(rule.Limit)
	rate := capacity / float64(rule.WindowSec)

	state := tokenBucketState{tokens: capacity, lastRefill: now}
	tb.engine.View(key, func(value interface{}, found bool) {
		state = restoreBucket(value, found, now, capacity, rate)
	})

	return Result{
		Allowed:   state.tokens >= 1,
		Remaining: int(state.tokens),
		ResetIn:   state.resetIn(capacity, rate),
		RetryIn:   state.retryIn(rate),
	}
}
