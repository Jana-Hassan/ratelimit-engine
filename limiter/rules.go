package limiter

import (
	"sync"

	"github.com/Jana-Hassan/ratelimit-engine/algorithms"
)

type Rule struct {
	algorithms.Rule
	Algorithm string `json:"algorithm"`
}

type RuleRepository interface {
	Load() ([]Rule, error)
	Save(rules []Rule) error
}

type nopRepository struct{}

func (nopRepository) Load() ([]Rule, error) { return nil, nil }

func (nopRepository) Save([]Rule) error { return nil }

type RuleStore struct {
	mu    sync.RWMutex
	rules map[string]Rule
	repo  RuleRepository
}

func NewRuleStore(repo RuleRepository) *RuleStore {
	if repo == nil {
		repo = nopRepository{}
	}
	return &RuleStore{rules: make(map[string]Rule), repo: repo}
}

func (rs *RuleStore) Load() (int, error) {
	rules, err := rs.repo.Load()
	if err != nil {
		return 0, err
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	for _, rule := range rules {
		rs.rules[rule.Name] = rule
	}
	return len(rules), nil
}

func (rs *RuleStore) Get(name string) (Rule, bool) {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	rule, found := rs.rules[name]
	return rule, found
}

func (rs *RuleStore) Set(rule Rule) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	previous, existed := rs.rules[rule.Name]
	rs.rules[rule.Name] = rule
	if err := rs.repo.Save(rs.listLocked()); err != nil {
		if existed {
			rs.rules[rule.Name] = previous
		} else {
			delete(rs.rules, rule.Name)
		}
		return err
	}
	return nil
}

func (rs *RuleStore) Delete(name string) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	previous, existed := rs.rules[name]
	if !existed {
		return nil
	}
	delete(rs.rules, name)
	if err := rs.repo.Save(rs.listLocked()); err != nil {
		rs.rules[name] = previous
		return err
	}
	return nil
}

func (rs *RuleStore) List() []Rule {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	return rs.listLocked()
}

func (rs *RuleStore) listLocked() []Rule {
	list := make([]Rule, 0, len(rs.rules))
	for _, rule := range rs.rules {
		list = append(list, rule)
	}
	return list
}

func (rs *RuleStore) CountByAlgorithm() map[string]int {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	counts := make(map[string]int)
	for _, rule := range rs.rules {
		counts[rule.Algorithm]++
	}
	return counts
}
