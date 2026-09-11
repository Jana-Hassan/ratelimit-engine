package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Jana-Hassan/ratelimit-engine/algorithms"
	"github.com/Jana-Hassan/ratelimit-engine/engine"
	"github.com/Jana-Hassan/ratelimit-engine/limiter"
	"github.com/Jana-Hassan/ratelimit-engine/metrics"
)

type stubRepo struct {
	mu    sync.Mutex
	rules []limiter.Rule
	fail  bool
}

func (r *stubRepo) Load() ([]limiter.Rule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rules, nil
}

func (r *stubRepo) Save(rules []limiter.Rule) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail {
		return errors.New("save failed")
	}
	r.rules = rules
	return nil
}

func (r *stubRepo) setFail(fail bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fail = fail
}

type harness struct {
	server  *Server
	limiter *limiter.Limiter
	engine  *engine.Engine
	metrics *metrics.Metrics
	repo    *stubRepo
}

func testRule(name string, algorithm string, limit int) limiter.Rule {
	return limiter.Rule{
		Rule:      algorithms.Rule{Name: name, Limit: limit, WindowSec: 3600},
		Algorithm: algorithm,
	}
}

func newHarness(t *testing.T, rules ...limiter.Rule) *harness {
	t.Helper()
	cache := engine.NewEngine(256)
	observability := metrics.New()
	observability.WatchEngine(cache)
	repo := &stubRepo{}
	l := limiter.New(cache, repo, observability)
	observability.WatchRules(l.Rules().CountByAlgorithm)
	for _, rule := range rules {
		if err := l.Rules().Set(rule); err != nil {
			t.Fatalf("seeding rule %s failed: %v", rule.Name, err)
		}
	}
	return &harness{
		server:  NewServer(l, cache, observability, false),
		limiter: l,
		engine:  cache,
		metrics: observability,
		repo:    repo,
	}
}

func (h *harness) do(t *testing.T, method string, target string, body string) *httptest.ResponseRecorder {
	t.Helper()
	var request *http.Request
	if body == "" {
		request = httptest.NewRequest(method, target, nil)
	} else {
		request = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	recorder := httptest.NewRecorder()
	h.server.ServeHTTP(recorder, request)
	return recorder
}

func (h *harness) check(t *testing.T, ruleName string, clientID string) *httptest.ResponseRecorder {
	t.Helper()
	return h.do(t, http.MethodPost, "/check", `{"rule":"`+ruleName+`","client_id":"`+clientID+`"}`)
}

func decodeCheck(t *testing.T, recorder *httptest.ResponseRecorder) checkResponse {
	t.Helper()
	var body checkResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding check body %q failed: %v", recorder.Body.String(), err)
	}
	return body
}

func decodeRules(t *testing.T, recorder *httptest.ResponseRecorder) []rulePayload {
	t.Helper()
	var body []rulePayload
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding rules body %q failed: %v", recorder.Body.String(), err)
	}
	return body
}

func errorMessage(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding error body %q failed: %v", recorder.Body.String(), err)
	}
	return body["error"]
}

func requireStatus(t *testing.T, recorder *httptest.ResponseRecorder, want int) {
	t.Helper()
	if recorder.Code != want {
		t.Fatalf("status = %d, want %d, body %q", recorder.Code, want, recorder.Body.String())
	}
}

func TestCheckAllowed(t *testing.T) {
	h := newHarness(t, testRule("posts", "fixed_window", 5))

	recorder := h.check(t, "posts", "user-1")

	requireStatus(t, recorder, http.StatusOK)
	body := decodeCheck(t, recorder)
	if !body.Allowed {
		t.Fatal("allowed = false, want true")
	}
	if body.Remaining != 4 {
		t.Fatalf("remaining = %d, want 4", body.Remaining)
	}
	if body.RetryIn != 0 {
		t.Fatalf("retry_in = %d, want 0", body.RetryIn)
	}
}

func TestCheckIsJSON(t *testing.T) {
	h := newHarness(t, testRule("posts", "fixed_window", 5))

	recorder := h.check(t, "posts", "user-1")

	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
}

func TestCheckConsumesQuota(t *testing.T) {
	h := newHarness(t, testRule("posts", "fixed_window", 3))

	for i, want := range []int{2, 1, 0} {
		body := decodeCheck(t, h.check(t, "posts", "user-1"))
		if !body.Allowed {
			t.Fatalf("call %d denied, want allowed", i+1)
		}
		if body.Remaining != want {
			t.Fatalf("call %d remaining = %d, want %d", i+1, body.Remaining, want)
		}
	}
}

func TestCheckDeniedWhenExhausted(t *testing.T) {
	h := newHarness(t, testRule("posts", "fixed_window", 1))
	h.check(t, "posts", "user-1")

	recorder := h.check(t, "posts", "user-1")

	requireStatus(t, recorder, http.StatusTooManyRequests)
	body := decodeCheck(t, recorder)
	if body.Allowed {
		t.Fatal("allowed = true, want false")
	}
	if body.RetryIn <= 0 {
		t.Fatalf("retry_in = %d, want positive", body.RetryIn)
	}
}

func TestCheckClientsAreIndependent(t *testing.T) {
	h := newHarness(t, testRule("posts", "fixed_window", 1))
	h.check(t, "posts", "user-1")

	body := decodeCheck(t, h.check(t, "posts", "user-2"))

	if !body.Allowed {
		t.Fatal("user-2 denied, want allowed")
	}
}

func TestCheckInvalidJSON(t *testing.T) {
	h := newHarness(t, testRule("posts", "fixed_window", 5))

	recorder := h.do(t, http.MethodPost, "/check", `{"rule":`)

	requireStatus(t, recorder, http.StatusBadRequest)
	if got := errorMessage(t, recorder); got != "invalid json body" {
		t.Fatalf("error = %q, want invalid json body", got)
	}
}

func TestCheckRequiresRuleAndClient(t *testing.T) {
	cases := map[string]string{
		"missing rule":      `{"client_id":"user-1"}`,
		"missing client_id": `{"rule":"posts"}`,
		"both empty":        `{"rule":"","client_id":""}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, testRule("posts", "fixed_window", 5))

			recorder := h.do(t, http.MethodPost, "/check", body)

			requireStatus(t, recorder, http.StatusBadRequest)
			if got := errorMessage(t, recorder); got != "rule and client_id are required" {
				t.Fatalf("error = %q, want rule and client_id are required", got)
			}
		})
	}
}

func TestCheckUnknownRule(t *testing.T) {
	h := newHarness(t)

	recorder := h.check(t, "missing", "user-1")

	requireStatus(t, recorder, http.StatusNotFound)
	if got := errorMessage(t, recorder); got != limiter.ErrRuleNotFound.Error() {
		t.Fatalf("error = %q, want %q", got, limiter.ErrRuleNotFound.Error())
	}
}

func TestCheckOversizedBody(t *testing.T) {
	h := newHarness(t, testRule("posts", "fixed_window", 5))
	body := `{"rule":"` + strings.Repeat("a", maxBodyBytes+1) + `","client_id":"user-1"}`

	recorder := h.do(t, http.MethodPost, "/check", body)

	requireStatus(t, recorder, http.StatusBadRequest)
}

func TestCheckStoredRuleWithUnknownAlgorithm(t *testing.T) {
	h := newHarness(t)
	if err := h.limiter.Rules().Set(testRule("posts", "slyding_window", 5)); err != nil {
		t.Fatalf("seeding rule failed: %v", err)
	}

	recorder := h.check(t, "posts", "user-1")

	requireStatus(t, recorder, http.StatusInternalServerError)
	if got := errorMessage(t, recorder); got != limiter.ErrAlgorithmNotFound.Error() {
		t.Fatalf("error = %q, want %q", got, limiter.ErrAlgorithmNotFound.Error())
	}
}

func TestPeekDoesNotConsumeQuota(t *testing.T) {
	h := newHarness(t, testRule("posts", "fixed_window", 5))

	h.do(t, http.MethodHead, "/check?rule=posts&client_id=user-1", "")
	h.do(t, http.MethodHead, "/check?rule=posts&client_id=user-1", "")

	body := decodeCheck(t, h.check(t, "posts", "user-1"))
	if body.Remaining != 4 {
		t.Fatalf("remaining after two peeks = %d, want 4", body.Remaining)
	}
}

func TestPeekHasNoBody(t *testing.T) {
	h := newHarness(t, testRule("posts", "fixed_window", 5))

	recorder := h.do(t, http.MethodHead, "/check?rule=posts&client_id=user-1", "")

	requireStatus(t, recorder, http.StatusOK)
	if recorder.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", recorder.Body.String())
	}
}

func TestPeekRequiresRuleAndClient(t *testing.T) {
	targets := map[string]string{
		"missing rule":      "/check?client_id=user-1",
		"missing client_id": "/check?rule=posts",
		"both missing":      "/check",
	}
	for name, target := range targets {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, testRule("posts", "fixed_window", 5))

			recorder := h.do(t, http.MethodHead, target, "")

			requireStatus(t, recorder, http.StatusBadRequest)
		})
	}
}

func TestPeekUnknownRule(t *testing.T) {
	h := newHarness(t)

	recorder := h.do(t, http.MethodHead, "/check?rule=missing&client_id=user-1", "")

	requireStatus(t, recorder, http.StatusNotFound)
}

func TestPeekReportsExhaustedQuota(t *testing.T) {
	h := newHarness(t, testRule("posts", "fixed_window", 1))
	h.check(t, "posts", "user-1")

	recorder := h.do(t, http.MethodHead, "/check?rule=posts&client_id=user-1", "")

	requireStatus(t, recorder, http.StatusTooManyRequests)
	if recorder.Header().Get("Retry-After") == "" {
		t.Fatal("Retry-After is empty, want a value")
	}
}

func TestSetRuleCreatesRule(t *testing.T) {
	h := newHarness(t)

	recorder := h.do(t, http.MethodPost, "/rules", `{"name":"posts","limit":5,"window_sec":60,"algorithm":"token_bucket"}`)

	requireStatus(t, recorder, http.StatusOK)
	stored := decodeRules(t, h.do(t, http.MethodGet, "/rules", ""))
	if len(stored) != 1 {
		t.Fatalf("stored %d rules, want 1", len(stored))
	}
	want := rulePayload{Name: "posts", Limit: 5, WindowSec: 60, Algorithm: "token_bucket"}
	if stored[0] != want {
		t.Fatalf("stored rule = %+v, want %+v", stored[0], want)
	}
}

func TestSetRuleReplacesExisting(t *testing.T) {
	h := newHarness(t, testRule("posts", "fixed_window", 5))

	h.do(t, http.MethodPost, "/rules", `{"name":"posts","limit":9,"window_sec":60,"algorithm":"token_bucket"}`)

	stored := decodeRules(t, h.do(t, http.MethodGet, "/rules", ""))
	if len(stored) != 1 {
		t.Fatalf("stored %d rules, want 1", len(stored))
	}
	if stored[0].Limit != 9 || stored[0].Algorithm != "token_bucket" {
		t.Fatalf("stored rule = %+v, want limit 9 and token_bucket", stored[0])
	}
}

func TestSetRuleInvalidJSON(t *testing.T) {
	h := newHarness(t)

	recorder := h.do(t, http.MethodPost, "/rules", `{"name":`)

	requireStatus(t, recorder, http.StatusBadRequest)
	if got := errorMessage(t, recorder); got != "invalid json body" {
		t.Fatalf("error = %q, want invalid json body", got)
	}
}

func TestSetRuleRequiresName(t *testing.T) {
	h := newHarness(t)

	recorder := h.do(t, http.MethodPost, "/rules", `{"limit":5,"window_sec":60,"algorithm":"token_bucket"}`)

	requireStatus(t, recorder, http.StatusBadRequest)
	if got := errorMessage(t, recorder); got != "name is required" {
		t.Fatalf("error = %q, want name is required", got)
	}
}

func TestSetRuleRequiresPositiveLimitAndWindow(t *testing.T) {
	bodies := map[string]string{
		"zero limit":      `{"name":"posts","limit":0,"window_sec":60,"algorithm":"token_bucket"}`,
		"negative limit":  `{"name":"posts","limit":-1,"window_sec":60,"algorithm":"token_bucket"}`,
		"zero window":     `{"name":"posts","limit":5,"window_sec":0,"algorithm":"token_bucket"}`,
		"negative window": `{"name":"posts","limit":5,"window_sec":-1,"algorithm":"token_bucket"}`,
		"missing both":    `{"name":"posts","algorithm":"token_bucket"}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)

			recorder := h.do(t, http.MethodPost, "/rules", body)

			requireStatus(t, recorder, http.StatusBadRequest)
			if got := errorMessage(t, recorder); got != "limit and window_sec must be positive" {
				t.Fatalf("error = %q, want limit and window_sec must be positive", got)
			}
		})
	}
}

func TestSetRuleRejectsUnknownAlgorithmWithoutStoring(t *testing.T) {
	bodies := map[string]string{
		"typo":    `{"name":"posts","limit":5,"window_sec":60,"algorithm":"slyding_window"}`,
		"missing": `{"name":"posts","limit":5,"window_sec":60}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)

			recorder := h.do(t, http.MethodPost, "/rules", body)

			requireStatus(t, recorder, http.StatusBadRequest)
			if got := errorMessage(t, recorder); got != "unknown algorithm" {
				t.Fatalf("error = %q, want unknown algorithm", got)
			}
			if stored := decodeRules(t, h.do(t, http.MethodGet, "/rules", "")); len(stored) != 0 {
				t.Fatalf("stored %d rules, want the bad rule rejected before storage", len(stored))
			}
		})
	}
}

func TestSetRulePersistFailure(t *testing.T) {
	h := newHarness(t)
	h.repo.setFail(true)

	recorder := h.do(t, http.MethodPost, "/rules", `{"name":"posts","limit":5,"window_sec":60,"algorithm":"token_bucket"}`)

	requireStatus(t, recorder, http.StatusInternalServerError)
	if got := errorMessage(t, recorder); got != "failed to persist rule" {
		t.Fatalf("error = %q, want failed to persist rule", got)
	}
	if stored := decodeRules(t, h.do(t, http.MethodGet, "/rules", "")); len(stored) != 0 {
		t.Fatalf("stored %d rules, want the failed write rolled back", len(stored))
	}
}

func TestListRulesEmptyIsEmptyArray(t *testing.T) {
	h := newHarness(t)

	recorder := h.do(t, http.MethodGet, "/rules", "")

	requireStatus(t, recorder, http.StatusOK)
	if got := strings.TrimSpace(recorder.Body.String()); got != "[]" {
		t.Fatalf("body = %q, want []", got)
	}
}

func TestListRulesReturnsEveryRule(t *testing.T) {
	h := newHarness(t, testRule("posts", "fixed_window", 5), testRule("logins", "token_bucket", 3))

	stored := decodeRules(t, h.do(t, http.MethodGet, "/rules", ""))

	if len(stored) != 2 {
		t.Fatalf("listed %d rules, want 2", len(stored))
	}
	names := map[string]bool{}
	for _, rule := range stored {
		names[rule.Name] = true
	}
	if !names["posts"] || !names["logins"] {
		t.Fatalf("listed names = %v, want posts and logins", names)
	}
}

func TestDeleteRule(t *testing.T) {
	h := newHarness(t, testRule("posts", "fixed_window", 5))

	recorder := h.do(t, http.MethodDelete, "/rules/posts", "")

	requireStatus(t, recorder, http.StatusNoContent)
	if stored := decodeRules(t, h.do(t, http.MethodGet, "/rules", "")); len(stored) != 0 {
		t.Fatalf("stored %d rules, want 0", len(stored))
	}
}

func TestDeleteRuleUnknown(t *testing.T) {
	h := newHarness(t)

	recorder := h.do(t, http.MethodDelete, "/rules/missing", "")

	requireStatus(t, recorder, http.StatusNotFound)
	if got := errorMessage(t, recorder); got != limiter.ErrRuleNotFound.Error() {
		t.Fatalf("error = %q, want %q", got, limiter.ErrRuleNotFound.Error())
	}
}

func TestDeleteRulePersistFailure(t *testing.T) {
	h := newHarness(t, testRule("posts", "fixed_window", 5))
	h.repo.setFail(true)

	recorder := h.do(t, http.MethodDelete, "/rules/posts", "")

	requireStatus(t, recorder, http.StatusInternalServerError)
	if got := errorMessage(t, recorder); got != "failed to persist rule deletion" {
		t.Fatalf("error = %q, want failed to persist rule deletion", got)
	}
	if stored := decodeRules(t, h.do(t, http.MethodGet, "/rules", "")); len(stored) != 1 {
		t.Fatalf("stored %d rules, want the failed delete rolled back", len(stored))
	}
}

func TestHealth(t *testing.T) {
	h := newHarness(t)

	recorder := h.do(t, http.MethodGet, "/healthz", "")

	requireStatus(t, recorder, http.StatusOK)
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding health body %q failed: %v", recorder.Body.String(), err)
	}
	if body["status"] != "ok" {
		t.Fatalf("status = %q, want ok", body["status"])
	}
}

func TestRoutingRejectsWrongMethod(t *testing.T) {
	h := newHarness(t)

	recorder := h.do(t, http.MethodGet, "/check", "")

	requireStatus(t, recorder, http.StatusMethodNotAllowed)
}

func TestRoutingUnknownPath(t *testing.T) {
	h := newHarness(t)

	recorder := h.do(t, http.MethodGet, "/nope", "")

	requireStatus(t, recorder, http.StatusNotFound)
}

func decodeStats(t *testing.T, h *harness) statsResponse {
	t.Helper()
	recorder := h.do(t, http.MethodGet, "/stats", "")
	requireStatus(t, recorder, http.StatusOK)
	var body statsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding stats body %q failed: %v", recorder.Body.String(), err)
	}
	return body
}

func TestStatsCountsRules(t *testing.T) {
	h := newHarness(t, testRule("posts", "fixed_window", 5), testRule("logins", "token_bucket", 3))

	stats := decodeStats(t, h)

	if stats.Rules != 2 {
		t.Fatalf("rules = %d, want 2", stats.Rules)
	}
}

func TestStatsReportsCacheShape(t *testing.T) {
	h := newHarness(t, testRule("posts", "fixed_window", 5))
	h.check(t, "posts", "user-1")

	stats := decodeStats(t, h)

	live := h.engine.Stats()
	if stats.Cache.Shards != live.Shards {
		t.Fatalf("shards = %d, want %d", stats.Cache.Shards, live.Shards)
	}
	if stats.Cache.Capacity != live.Capacity {
		t.Fatalf("capacity = %d, want %d", stats.Cache.Capacity, live.Capacity)
	}
	if stats.Cache.Entries != live.Entries {
		t.Fatalf("entries = %d, want %d", stats.Cache.Entries, live.Entries)
	}
	if stats.Cache.Entries != 1 {
		t.Fatalf("entries = %d, want 1 after a single client check", stats.Cache.Entries)
	}
}

func TestStatsReportsDecisions(t *testing.T) {
	h := newHarness(t, testRule("posts", "fixed_window", 2))
	h.check(t, "posts", "user-1")
	h.check(t, "posts", "user-1")
	h.check(t, "posts", "user-1")
	h.do(t, http.MethodHead, "/check?rule=posts&client_id=user-1", "")

	stats := decodeStats(t, h)

	if len(stats.Decisions) != 1 {
		t.Fatalf("decisions = %d groups, want 1", len(stats.Decisions))
	}
	decision := stats.Decisions[0]
	if decision.Rule != "posts" || decision.Algorithm != "fixed_window" {
		t.Fatalf("decision = %+v, want rule posts on fixed_window", decision)
	}
	if decision.Allowed != 2 {
		t.Fatalf("allowed = %v, want 2", decision.Allowed)
	}
	if decision.Denied != 1 {
		t.Fatalf("denied = %v, want 1", decision.Denied)
	}
	if decision.Peeks != 1 {
		t.Fatalf("peeks = %v, want 1", decision.Peeks)
	}
}

func TestStatsWithoutTrafficIsEmpty(t *testing.T) {
	h := newHarness(t)

	stats := decodeStats(t, h)

	if stats.Rules != 0 {
		t.Fatalf("rules = %d, want 0", stats.Rules)
	}
	if len(stats.Decisions) != 0 {
		t.Fatalf("decisions = %d groups, want 0", len(stats.Decisions))
	}
	if stats.Cache.HitRate != 0 {
		t.Fatalf("hit_rate = %v, want 0", stats.Cache.HitRate)
	}
}

func TestNewCacheStatsHitRate(t *testing.T) {
	cases := map[string]struct {
		stats engine.Stats
		want  float64
	}{
		"no lookups":    {engine.Stats{}, 0},
		"all hits":      {engine.Stats{Hits: 4}, 1},
		"all misses":    {engine.Stats{Misses: 4}, 0},
		"three of four": {engine.Stats{Hits: 3, Misses: 1}, 0.75},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := newCacheStats(tc.stats).HitRate; got != tc.want {
				t.Fatalf("hit rate = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNewCacheStatsCopiesCounters(t *testing.T) {
	stats := engine.Stats{Hits: 7, Misses: 3, Evictions: 2, Entries: 5, Capacity: 100, Shards: 256}

	got := newCacheStats(stats)

	want := cacheStats{Hits: 7, Misses: 3, HitRate: 0.7, Evictions: 2, Entries: 5, Capacity: 100, Shards: 256}
	if got != want {
		t.Fatalf("cache stats = %+v, want %+v", got, want)
	}
}

func TestMetricsEndpointExposesTheRegistry(t *testing.T) {
	h := newHarness(t, testRule("posts", "fixed_window", 5))
	h.check(t, "posts", "user-1")

	recorder := h.do(t, http.MethodGet, "/metrics", "")

	requireStatus(t, recorder, http.StatusOK)
	body := recorder.Body.String()
	for _, name := range []string{
		"ratelimit_decisions_total",
		"ratelimit_decision_duration_seconds",
		"ratelimit_http_requests_total",
		"ratelimit_cache_shards",
		"ratelimit_cache_entries",
		"ratelimit_rules",
	} {
		if !strings.Contains(body, name) {
			t.Fatalf("/metrics body is missing %s", name)
		}
	}
}
