package algorithms

import (
	"time"

	"github.com/Jana-Hassan/ratelimit-engine/engine"
)

type slidingWindowState struct {
	windowStart int64
	count       int
	prevCount   int
}

type SlidingWindow struct {
	engine *engine.Engine
}

func NewSlidingWindow(e *engine.Engine) *SlidingWindow {
	return &SlidingWindow{engine: e}
}

func slidingRetryIn(state slidingWindowState, rule Rule, now int64, window int64) time.Duration {
	end := state.windowStart + window
	if state.count >= rule.Limit || state.prevCount == 0 {
		return time.Duration(end-now) * time.Second
	}
	span := float64(rule.Limit-state.count) * float64(window) / float64(state.prevCount)
	wait := float64(end-now) - span
	if wait <= 0 {
		return 0
	}
	return time.Duration(wait * float64(time.Second))
}

func (sw *SlidingWindow) Allow(key string, rule Rule) Result {
	if rule.WindowSec <= 0 || rule.Limit <= 0 {
		return Result{Allowed: false}
	}

	now := time.Now().Unix()
	window := int64(rule.WindowSec)
	start := now - now%window
	resetIn := time.Duration(start+window-now) * time.Second
	weight := float64(start+window-now) / float64(window)

	allowed := false
	remaining := 0
	final := slidingWindowState{windowStart: start}

	sw.engine.Update(key, func(value interface{}, found bool) interface{} {
		state := slidingWindowState{windowStart: start}
		if found {
			if previous, ok := value.(slidingWindowState); ok {
				switch previous.windowStart {
				case start:
					state = previous
				case start - window:
					state.prevCount = previous.count
				}
			}
		}

		estimated := float64(state.prevCount)*weight + float64(state.count)
		if estimated < float64(rule.Limit) {
			state.count++
			allowed = true
			estimated = float64(state.prevCount)*weight + float64(state.count)
			if left := rule.Limit - int(estimated); left > 0 {
				remaining = left
			}
		}
		final = state
		return state
	})

	retryIn := time.Duration(0)
	if !allowed {
		retryIn = slidingRetryIn(final, rule, now, window)
	}
	return Result{Allowed: allowed, Remaining: remaining, ResetIn: resetIn, RetryIn: retryIn}
}

func (sw *SlidingWindow) Peek(key string, rule Rule) Result {
	if rule.WindowSec <= 0 || rule.Limit <= 0 {
		return Result{Allowed: false}
	}

	now := time.Now().Unix()
	window := int64(rule.WindowSec)
	start := now - now%window
	resetIn := time.Duration(start+window-now) * time.Second
	weight := float64(start+window-now) / float64(window)

	state := slidingWindowState{windowStart: start}
	sw.engine.View(key, func(value interface{}, found bool) {
		if !found {
			return
		}
		if previous, ok := value.(slidingWindowState); ok {
			switch previous.windowStart {
			case start:
				state = previous
			case start - window:
				state.prevCount = previous.count
			}
		}
	})

	estimated := float64(state.prevCount)*weight + float64(state.count)
	allowed := estimated < float64(rule.Limit)
	remaining := 0
	if left := rule.Limit - int(estimated); left > 0 {
		remaining = left
	}
	retryIn := time.Duration(0)
	if !allowed {
		retryIn = slidingRetryIn(state, rule, now, window)
	}
	return Result{Allowed: allowed, Remaining: remaining, ResetIn: resetIn, RetryIn: retryIn}
}
