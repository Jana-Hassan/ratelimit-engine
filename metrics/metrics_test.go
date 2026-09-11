package metrics

import (
	"strings"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
)

func family(t *testing.T, m *Metrics, name string) *dto.MetricFamily {
	t.Helper()
	families, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("gathering metrics failed: %v", err)
	}
	for _, gathered := range families {
		if gathered.GetName() == name {
			return gathered
		}
	}
	t.Fatalf("metric family %s was not registered", name)
	return nil
}

func hasLabels(metric *dto.Metric, labels map[string]string) bool {
	for key, want := range labels {
		found := false
		for _, pair := range metric.GetLabel() {
			if pair.GetName() == key && pair.GetValue() == want {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func metricWith(t *testing.T, m *Metrics, name string, labels map[string]string) *dto.Metric {
	t.Helper()
	for _, metric := range family(t, m, name).GetMetric() {
		if hasLabels(metric, labels) {
			return metric
		}
	}
	t.Fatalf("%s has no series with labels %v", name, labels)
	return nil
}

func counter(t *testing.T, m *Metrics, name string, labels map[string]string) float64 {
	t.Helper()
	return metricWith(t, m, name, labels).GetCounter().GetValue()
}

func gauge(t *testing.T, m *Metrics, name string, labels map[string]string) float64 {
	t.Helper()
	return metricWith(t, m, name, labels).GetGauge().GetValue()
}

func seriesCount(t *testing.T, m *Metrics, name string) int {
	t.Helper()
	return len(family(t, m, name).GetMetric())
}

func TestRecordDecisionLabelsTheOutcome(t *testing.T) {
	m := New()

	m.RecordDecision("token_bucket", "posts", "check", true, time.Microsecond)
	m.RecordDecision("token_bucket", "posts", "check", true, time.Microsecond)
	m.RecordDecision("token_bucket", "posts", "check", false, time.Microsecond)

	allowed := map[string]string{"algorithm": "token_bucket", "rule": "posts", "mode": "check", "decision": "allowed"}
	if got := counter(t, m, "ratelimit_decisions_total", allowed); got != 2 {
		t.Fatalf("allowed decisions = %v, want 2", got)
	}
	denied := map[string]string{"algorithm": "token_bucket", "rule": "posts", "mode": "check", "decision": "denied"}
	if got := counter(t, m, "ratelimit_decisions_total", denied); got != 1 {
		t.Fatalf("denied decisions = %v, want 1", got)
	}
}

func TestRecordDecisionSeparatesModes(t *testing.T) {
	m := New()

	m.RecordDecision("fixed_window", "posts", "check", true, time.Microsecond)
	m.RecordDecision("fixed_window", "posts", "peek", true, time.Microsecond)

	check := map[string]string{"mode": "check", "decision": "allowed"}
	if got := counter(t, m, "ratelimit_decisions_total", check); got != 1 {
		t.Fatalf("check decisions = %v, want 1", got)
	}
	peek := map[string]string{"mode": "peek", "decision": "allowed"}
	if got := counter(t, m, "ratelimit_decisions_total", peek); got != 1 {
		t.Fatalf("peek decisions = %v, want 1", got)
	}
}

func TestRecordDecisionObservesDuration(t *testing.T) {
	m := New()

	m.RecordDecision("fixed_window", "posts", "check", true, 2*time.Microsecond)
	m.RecordDecision("fixed_window", "posts", "check", false, 4*time.Microsecond)

	labels := map[string]string{"algorithm": "fixed_window", "rule": "posts", "mode": "check"}
	histogram := metricWith(t, m, "ratelimit_decision_duration_seconds", labels).GetHistogram()
	if histogram.GetSampleCount() != 2 {
		t.Fatalf("sample count = %d, want 2", histogram.GetSampleCount())
	}
	if want := 6e-6; histogram.GetSampleSum() < want*0.99 || histogram.GetSampleSum() > want*1.01 {
		t.Fatalf("sample sum = %v, want about %v", histogram.GetSampleSum(), want)
	}
}

func TestRecordHTTPLabelsRouteMethodAndStatus(t *testing.T) {
	m := New()

	m.RecordHTTP("POST /check", "POST", "200", time.Millisecond)
	m.RecordHTTP("POST /check", "POST", "429", time.Millisecond)
	m.RecordHTTP("POST /check", "POST", "200", time.Millisecond)

	allowed := map[string]string{"route": "POST /check", "method": "POST", "status": "200"}
	if got := counter(t, m, "ratelimit_http_requests_total", allowed); got != 2 {
		t.Fatalf("200 requests = %v, want 2", got)
	}
	denied := map[string]string{"route": "POST /check", "method": "POST", "status": "429"}
	if got := counter(t, m, "ratelimit_http_requests_total", denied); got != 1 {
		t.Fatalf("429 requests = %v, want 1", got)
	}
}

func TestRecordHTTPObservesDuration(t *testing.T) {
	m := New()

	m.RecordHTTP("GET /rules", "GET", "200", time.Millisecond)

	labels := map[string]string{"route": "GET /rules", "method": "GET"}
	histogram := metricWith(t, m, "ratelimit_http_request_duration_seconds", labels).GetHistogram()
	if histogram.GetSampleCount() != 1 {
		t.Fatalf("sample count = %d, want 1", histogram.GetSampleCount())
	}
}

func TestRecordProbeThrottled(t *testing.T) {
	m := New()

	m.RecordProbeThrottled()
	m.RecordProbeThrottled()

	if got := counter(t, m, "ratelimit_probes_throttled_total", nil); got != 2 {
		t.Fatalf("probes throttled = %v, want 2", got)
	}
}

func TestUptimeNeverGoesBackwards(t *testing.T) {
	m := New()

	first := m.Uptime()
	second := m.Uptime()

	if first < 0 {
		t.Fatalf("uptime = %v, want zero or more", first)
	}
	if second < first {
		t.Fatalf("uptime went backwards from %v to %v", first, second)
	}
}

func TestUptimeMeasuresElapsedTime(t *testing.T) {
	m := New()
	m.startedAt = time.Now().Add(-90 * time.Second)

	got := m.Uptime()

	if got < 90*time.Second || got > 95*time.Second {
		t.Fatalf("uptime = %v, want about 90s", got)
	}
}

func TestDecisionLabel(t *testing.T) {
	if got := decisionLabel(true); got != "allowed" {
		t.Fatalf("decisionLabel(true) = %q, want allowed", got)
	}
	if got := decisionLabel(false); got != "denied" {
		t.Fatalf("decisionLabel(false) = %q, want denied", got)
	}
}

func TestRegistryIsIsolatedPerInstance(t *testing.T) {
	first := New()
	second := New()

	first.RecordProbeThrottled()

	if got := counter(t, first, "ratelimit_probes_throttled_total", nil); got != 1 {
		t.Fatalf("first registry = %v, want 1", got)
	}
	if got := counter(t, second, "ratelimit_probes_throttled_total", nil); got != 0 {
		t.Fatalf("second registry = %v, want 0", got)
	}
}

func TestRegistryIncludesRuntimeCollectors(t *testing.T) {
	m := New()

	families, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("gathering metrics failed: %v", err)
	}

	runtime := false
	for _, gathered := range families {
		if strings.HasPrefix(gathered.GetName(), "go_") {
			runtime = true
			break
		}
	}
	if !runtime {
		t.Fatal("registry exposes no go_ metrics, want the Go collector registered")
	}
}

func findStat(t *testing.T, stats []RuleStat, rule string) RuleStat {
	t.Helper()
	for _, stat := range stats {
		if stat.Rule == rule {
			return stat
		}
	}
	t.Fatalf("no stat for rule %s in %+v", rule, stats)
	return RuleStat{}
}

func TestRuleStatsWithoutTraffic(t *testing.T) {
	m := New()

	stats := m.RuleStats()

	if len(stats) != 0 {
		t.Fatalf("stats = %+v, want empty", stats)
	}
}

func TestRuleStatsSplitsAllowedAndDenied(t *testing.T) {
	m := New()
	m.RecordDecision("token_bucket", "posts", "check", true, time.Microsecond)
	m.RecordDecision("token_bucket", "posts", "check", true, time.Microsecond)
	m.RecordDecision("token_bucket", "posts", "check", false, time.Microsecond)

	stats := m.RuleStats()

	if len(stats) != 1 {
		t.Fatalf("stats = %d groups, want 1", len(stats))
	}
	stat := stats[0]
	if stat.Rule != "posts" || stat.Algorithm != "token_bucket" {
		t.Fatalf("stat = %+v, want posts on token_bucket", stat)
	}
	if stat.Allowed != 2 {
		t.Fatalf("allowed = %v, want 2", stat.Allowed)
	}
	if stat.Denied != 1 {
		t.Fatalf("denied = %v, want 1", stat.Denied)
	}
	if stat.Peeks != 0 {
		t.Fatalf("peeks = %v, want 0", stat.Peeks)
	}
}

func TestRuleStatsCountsPeeksSeparately(t *testing.T) {
	m := New()
	m.RecordDecision("fixed_window", "posts", "check", true, time.Microsecond)
	m.RecordDecision("fixed_window", "posts", "peek", true, time.Microsecond)
	m.RecordDecision("fixed_window", "posts", "peek", false, time.Microsecond)

	stat := findStat(t, m.RuleStats(), "posts")

	if stat.Peeks != 2 {
		t.Fatalf("peeks = %v, want 2", stat.Peeks)
	}
	if stat.Allowed != 1 {
		t.Fatalf("allowed = %v, want 1, peeks must not count as allowed", stat.Allowed)
	}
	if stat.Denied != 0 {
		t.Fatalf("denied = %v, want 0, a denied peek must not count as a denial", stat.Denied)
	}
}

func TestRuleStatsGroupsByRuleAndAlgorithm(t *testing.T) {
	m := New()
	m.RecordDecision("token_bucket", "posts", "check", true, time.Microsecond)
	m.RecordDecision("fixed_window", "logins", "check", true, time.Microsecond)
	m.RecordDecision("fixed_window", "logins", "check", false, time.Microsecond)

	stats := m.RuleStats()

	if len(stats) != 2 {
		t.Fatalf("stats = %d groups, want 2", len(stats))
	}
	posts := findStat(t, stats, "posts")
	if posts.Allowed != 1 || posts.Denied != 0 {
		t.Fatalf("posts = %+v, want 1 allowed and 0 denied", posts)
	}
	logins := findStat(t, stats, "logins")
	if logins.Allowed != 1 || logins.Denied != 1 {
		t.Fatalf("logins = %+v, want 1 allowed and 1 denied", logins)
	}
}

func TestRuleStatsSeparatesAlgorithmsSharingARuleName(t *testing.T) {
	m := New()
	m.RecordDecision("token_bucket", "posts", "check", true, time.Microsecond)
	m.RecordDecision("fixed_window", "posts", "check", true, time.Microsecond)

	stats := m.RuleStats()

	if len(stats) != 2 {
		t.Fatalf("stats = %d groups, want 2 so a rule that changed algorithm stays separable", len(stats))
	}
}

func TestRuleStatsSortedByRule(t *testing.T) {
	m := New()
	for _, rule := range []string{"zeta", "alpha", "midway"} {
		m.RecordDecision("fixed_window", rule, "check", true, time.Microsecond)
	}

	stats := m.RuleStats()

	want := []string{"alpha", "midway", "zeta"}
	for i, name := range want {
		if stats[i].Rule != name {
			t.Fatalf("stats[%d] = %s, want %s in %+v", i, stats[i].Rule, name, stats)
		}
	}
}
