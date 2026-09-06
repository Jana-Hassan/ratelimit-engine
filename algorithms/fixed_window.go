package algorithms

import (
	"time"

	"github.com/Jana-Hassan/ratelimit-engine/engine"
)

type fixedWindowState struct {
	windowStart int64
	count       int
}

type FixedWindow struct {
	engine *engine.Engine
	clock  Clock
}

func NewFixedWindow(e *engine.Engine) *FixedWindow {
	return &FixedWindow{engine: e, clock: systemClock{}}
}

func (fw *FixedWindow) Allow(key string, rule Rule) Result {
	if rule.WindowSec <= 0 || rule.Limit <= 0 {
		return Result{Allowed: false}
	}

	now := fw.clock.Now().Unix()
	window := int64(rule.WindowSec)
	start := now - now%window
	resetIn := time.Duration(start+window-now) * time.Second

	allowed := false
	remaining := 0

	fw.engine.Update(key, func(value interface{}, found bool) interface{} {
		state := fixedWindowState{windowStart: start}
		if found {
			if previous, ok := value.(fixedWindowState); ok && previous.windowStart == start {
				state = previous
			}
		}
		if state.count < rule.Limit {
			state.count++
			allowed = true
			remaining = rule.Limit - state.count
		}
		return state
	})

	return Result{Allowed: allowed, Remaining: remaining, ResetIn: resetIn, RetryIn: retryAfter(allowed, resetIn)}
}

func (fw *FixedWindow) Peek(key string, rule Rule) Result {
	if rule.WindowSec <= 0 || rule.Limit <= 0 {
		return Result{Allowed: false}
	}

	now := fw.clock.Now().Unix()
	window := int64(rule.WindowSec)
	start := now - now%window
	resetIn := time.Duration(start+window-now) * time.Second

	count := 0
	fw.engine.View(key, func(value interface{}, found bool) {
		if !found {
			return
		}
		if previous, ok := value.(fixedWindowState); ok && previous.windowStart == start {
			count = previous.count
		}
	})

	allowed := count < rule.Limit
	remaining := 0
	if left := rule.Limit - count; left > 0 {
		remaining = left
	}
	return Result{Allowed: allowed, Remaining: remaining, ResetIn: resetIn, RetryIn: retryAfter(allowed, resetIn)}
}

func retryAfter(allowed bool, wait time.Duration) time.Duration {
	if allowed {
		return 0
	}
	return wait
}
