package limiter

import (
	"sync"

	"github.com/Jana-Hassan/ratelimit-engine/algorithms"
)

type Rule struct {
	algorithms.Rule
	Algorithm string
}

type RuleStore struct {
	mu    sync.RWMutex
	rules map[string]Rule
}

func NewRuleStore() *RuleStore {
	return &RuleStore{rules: make(map[string]Rule)}
}

func (rs *RuleStore) Get(name string) (Rule, bool) {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	rule, found := rs.rules[name]
	return rule, found
}

func (rs *RuleStore) Set(rule Rule) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.rules[rule.Name] = rule
}

func (rs *RuleStore) Delete(name string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	delete(rs.rules, name)
}

func (rs *RuleStore) List() []Rule {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	list := make([]Rule, 0, len(rs.rules))
	for _, rule := range rs.rules {
		list = append(list, rule)
	}
	return list
}
