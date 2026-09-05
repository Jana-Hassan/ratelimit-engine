package algorithms

import "time"

type Rule struct {
	Name      string `json:"name"`
	Limit     int    `json:"limit"`
	WindowSec int    `json:"window_sec"`
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
