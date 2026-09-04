package algorithms

import "time"

type Rule struct {
	Name      string
	Limit     int
	WindowSec int
}

type Result struct {
	Allowed   bool
	Remaining int
	ResetIn   time.Duration
	RetryIn   time.Duration
}

type Algorithm interface {
	Allow(key string, rule Rule) Result
	Peek(key string, rule Rule) Result
}
