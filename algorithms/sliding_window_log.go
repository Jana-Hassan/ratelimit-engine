package algorithms

import (
	"time"

	"github.com/Jana-Hassan/ratelimit-engine/engine"
)

type slidingWindowLogState struct {
	stamps []int64
	head   int
	size   int
}

type SlidingWindowLog struct {
	engine *engine.Engine
}

func NewSlidingWindowLog(e *engine.Engine) *SlidingWindowLog {
	return &SlidingWindowLog{engine: e}
}

func (state *slidingWindowLogState) prune(cutoff int64) {
	for state.size > 0 && state.stamps[state.head] <= cutoff {
		state.head = (state.head + 1) % len(state.stamps)
		state.size--
	}
}

func (state *slidingWindowLogState) append(now int64) {
	state.stamps[(state.head+state.size)%len(state.stamps)] = now
	state.size++
}

func (state *slidingWindowLogState) resetIn(now int64, window int64) time.Duration {
	if state.size == 0 {
		return 0
	}
	newest := state.stamps[(state.head+state.size-1)%len(state.stamps)]
	return time.Duration(newest + window - now)
}

func (state *slidingWindowLogState) retryIn(now int64, window int64) time.Duration {
	if state.size == 0 {
		return 0
	}
	return time.Duration(state.stamps[state.head] + window - now)
}

func restoreLog(value interface{}, found bool, limit int) slidingWindowLogState {
	if found {
		if previous, ok := value.(slidingWindowLogState); ok && len(previous.stamps) == limit {
			return previous
		}
	}
	return slidingWindowLogState{stamps: make([]int64, limit)}
}

func (swl *SlidingWindowLog) Allow(key string, rule Rule) Result {
	if rule.WindowSec <= 0 || rule.Limit <= 0 {
		return Result{Allowed: false}
	}

	now := time.Now().UnixNano()
	window := int64(rule.WindowSec) * int64(time.Second)

	result := Result{}
	swl.engine.Update(key, func(value interface{}, found bool) interface{} {
		state := restoreLog(value, found, rule.Limit)
		state.prune(now - window)

		if state.size < rule.Limit {
			state.append(now)
			result.Allowed = true
			result.Remaining = rule.Limit - state.size
		} else {
			result.RetryIn = state.retryIn(now, window)
		}
		result.ResetIn = state.resetIn(now, window)
		return state
	})

	return result
}

func (swl *SlidingWindowLog) Peek(key string, rule Rule) Result {
	if rule.WindowSec <= 0 || rule.Limit <= 0 {
		return Result{Allowed: false}
	}

	now := time.Now().UnixNano()
	window := int64(rule.WindowSec) * int64(time.Second)
	cutoff := now - window

	inWindow := 0
	oldest := int64(0)
	newest := int64(0)
	swl.engine.View(key, func(value interface{}, found bool) {
		if !found {
			return
		}
		state, ok := value.(slidingWindowLogState)
		if !ok || len(state.stamps) != rule.Limit {
			return
		}
		expired := 0
		for expired < state.size && state.stamps[(state.head+expired)%len(state.stamps)] <= cutoff {
			expired++
		}
		inWindow = state.size - expired
		if inWindow > 0 {
			oldest = state.stamps[(state.head+expired)%len(state.stamps)]
			newest = state.stamps[(state.head+state.size-1)%len(state.stamps)]
		}
	})

	allowed := inWindow < rule.Limit
	remaining := 0
	if left := rule.Limit - inWindow; left > 0 {
		remaining = left
	}
	resetIn := time.Duration(0)
	if inWindow > 0 {
		resetIn = time.Duration(newest + window - now)
	}
	retryIn := time.Duration(0)
	if !allowed {
		retryIn = time.Duration(oldest + window - now)
	}
	return Result{
		Allowed:   allowed,
		Remaining: remaining,
		ResetIn:   resetIn,
		RetryIn:   retryIn,
	}
}
