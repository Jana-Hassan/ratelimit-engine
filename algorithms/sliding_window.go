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
		return state
	})

	return Result{Allowed: allowed, Remaining: remaining, ResetIn: resetIn}
}

func (sw *SlidingWindow) Peek(key string, rule Rule) Result {
	if rule.WindowSec <= 0 || rule.Limit <= 0 {
		return Result{Allowed: false}
	}

	now := time.Now().Unix()
	window := int64(rule.WindowSec)
	start := now - now%window
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
	remaining := 0
	if left := rule.Limit - int(estimated); left > 0 {
		remaining = left
	}
	return Result{
		Allowed:   estimated < float64(rule.Limit),
		Remaining: remaining,
		ResetIn:   time.Duration(start+window-now) * time.Second,
	}
}
