package metrics

import (
	"sort"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

type RuleStat struct {
	Rule      string  `json:"rule"`
	Algorithm string  `json:"algorithm"`
	Allowed   float64 `json:"allowed"`
	Denied    float64 `json:"denied"`
	Peeks     float64 `json:"peeks"`
}

func labels(metric *dto.Metric) map[string]string {
	pairs := make(map[string]string, len(metric.GetLabel()))
	for _, label := range metric.GetLabel() {
		pairs[label.GetName()] = label.GetValue()
	}
	return pairs
}

func (m *Metrics) RuleStats() []RuleStat {
	collected := make(chan prometheus.Metric, 128)
	go func() {
		m.decisions.Collect(collected)
		close(collected)
	}()

	grouped := make(map[[2]string]*RuleStat)
	for metric := range collected {
		var measured dto.Metric
		if err := metric.Write(&measured); err != nil {
			continue
		}
		pairs := labels(&measured)
		key := [2]string{pairs["rule"], pairs["algorithm"]}
		stat, found := grouped[key]
		if !found {
			stat = &RuleStat{Rule: key[0], Algorithm: key[1]}
			grouped[key] = stat
		}
		value := measured.GetCounter().GetValue()
		switch {
		case pairs["mode"] == "peek":
			stat.Peeks += value
		case pairs["decision"] == "allowed":
			stat.Allowed += value
		default:
			stat.Denied += value
		}
	}

	stats := make([]RuleStat, 0, len(grouped))
	for _, stat := range grouped {
		stats = append(stats, *stat)
	}
	sort.Slice(stats, func(i, j int) bool { return stats[i].Rule < stats[j].Rule })
	return stats
}
