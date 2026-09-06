package limiter

import (
	"errors"
	"strconv"
	"time"

	"github.com/Jana-Hassan/ratelimit-engine/algorithms"
	"github.com/Jana-Hassan/ratelimit-engine/engine"
)

var (
	ErrRuleNotFound      = errors.New("rule not found")
	ErrAlgorithmNotFound = errors.New("algorithm not found")
)

type Recorder interface {
	RecordDecision(algorithm string, rule string, mode string, allowed bool, elapsed time.Duration)
}

type nopRecorder struct{}

func (nopRecorder) RecordDecision(string, string, string, bool, time.Duration) {}

type Limiter struct {
	engine     *engine.Engine
	rules      *RuleStore
	recorder   Recorder
	algorithms map[string]algorithms.Algorithm
}

func New(e *engine.Engine, repo RuleRepository, recorder Recorder) *Limiter {
	if recorder == nil {
		recorder = nopRecorder{}
	}
	return &Limiter{
		engine:   e,
		rules:    NewRuleStore(repo),
		recorder: recorder,
		algorithms: map[string]algorithms.Algorithm{
			"fixed_window":       algorithms.NewFixedWindow(e),
			"sliding_window":     algorithms.NewSlidingWindow(e),
			"sliding_window_log": algorithms.NewSlidingWindowLog(e),
			"token_bucket":       algorithms.NewTokenBucket(e),
		},
	}
}

func (l *Limiter) Rules() *RuleStore {
	return l.rules
}

func (l *Limiter) HasAlgorithm(name string) bool {
	_, found := l.algorithms[name]
	return found
}

func (l *Limiter) resolve(ruleName string) (Rule, algorithms.Algorithm, error) {
	rule, found := l.rules.Get(ruleName)
	if !found {
		return Rule{}, nil, ErrRuleNotFound
	}
	algorithm, found := l.algorithms[rule.Algorithm]
	if !found {
		return Rule{}, nil, ErrAlgorithmNotFound
	}
	return rule, algorithm, nil
}

func cacheKey(ruleName string, clientID string) string {
	return strconv.Itoa(len(ruleName)) + ":" + ruleName + ":" + clientID
}

// calculates algo latency
func (l *Limiter) observe(key string, rule Rule, mode string, decide func(key string, rule algorithms.Rule) algorithms.Result) algorithms.Result {
	start := time.Now()
	result := decide(key, rule.Rule)
	l.recorder.RecordDecision(rule.Algorithm, rule.Name, mode, result.Allowed, time.Since(start))
	return result
}

func (l *Limiter) Check(ruleName string, clientID string) (Rule, algorithms.Result, error) {
	rule, algorithm, err := l.resolve(ruleName)
	if err != nil {
		return Rule{}, algorithms.Result{}, err
	}
	return rule, l.observe(cacheKey(ruleName, clientID), rule, "check", algorithm.Allow), nil
}

func (l *Limiter) Peek(ruleName string, clientID string) (Rule, algorithms.Result, error) {
	rule, algorithm, err := l.resolve(ruleName)
	if err != nil {
		return Rule{}, algorithms.Result{}, err
	}
	return rule, l.observe(cacheKey(ruleName, clientID), rule, "peek", algorithm.Peek), nil
}
