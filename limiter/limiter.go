package limiter

import (
	"errors"

	"github.com/Jana-Hassan/ratelimit-engine/algorithms"
	"github.com/Jana-Hassan/ratelimit-engine/engine"
)

var (
	ErrRuleNotFound      = errors.New("rule not found")
	ErrAlgorithmNotFound = errors.New("algorithm not found")
)

type Limiter struct {
	engine     *engine.Engine
	rules      *RuleStore
	algorithms map[string]algorithms.Algorithm
}

func New(e *engine.Engine, repo RuleRepository) *Limiter {
	return &Limiter{
		engine: e,
		rules:  NewRuleStore(repo),
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
	return ruleName + ":" + clientID
}

func (l *Limiter) Check(ruleName string, clientID string) (Rule, algorithms.Result, error) {
	rule, algorithm, err := l.resolve(ruleName)
	if err != nil {
		return Rule{}, algorithms.Result{}, err
	}
	return rule, algorithm.Allow(cacheKey(ruleName, clientID), rule.Rule), nil
}

func (l *Limiter) Peek(ruleName string, clientID string) (Rule, algorithms.Result, error) {
	rule, algorithm, err := l.resolve(ruleName)
	if err != nil {
		return Rule{}, algorithms.Result{}, err
	}
	return rule, algorithm.Peek(cacheKey(ruleName, clientID), rule.Rule), nil
}
