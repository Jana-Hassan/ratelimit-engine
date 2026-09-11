package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Jana-Hassan/ratelimit-engine/algorithms"
	"github.com/Jana-Hassan/ratelimit-engine/metrics"
	dto "github.com/prometheus/client_model/go"
)

func gatherFamily(t *testing.T, m *metrics.Metrics, name string) *dto.MetricFamily {
	t.Helper()
	families, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("gathering metrics failed: %v", err)
	}
	for _, family := range families {
		if family.GetName() == name {
			return family
		}
	}
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

func counterTotal(t *testing.T, m *metrics.Metrics, name string, labels map[string]string) float64 {
	t.Helper()
	family := gatherFamily(t, m, name)
	if family == nil {
		return 0
	}
	total := 0.0
	for _, metric := range family.GetMetric() {
		if hasLabels(metric, labels) {
			total += metric.GetCounter().GetValue()
		}
	}
	return total
}

func (h *harness) probe(t *testing.T, remoteAddr string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodHead, "/check?rule=posts&client_id=user-1", nil)
	request.RemoteAddr = remoteAddr
	recorder := httptest.NewRecorder()
	h.server.ServeHTTP(recorder, request)
	return recorder
}

func TestRateLimitHeadersOnAllowedResponse(t *testing.T) {
	h := newHarness(t, testRule("posts", "fixed_window", 5))

	recorder := h.check(t, "posts", "user-1")

	header := recorder.Header()
	if got := header.Get("X-RateLimit-Limit"); got != "5" {
		t.Fatalf("X-RateLimit-Limit = %q, want 5", got)
	}
	if got := header.Get("X-RateLimit-Remaining"); got != "4" {
		t.Fatalf("X-RateLimit-Remaining = %q, want 4", got)
	}
	rateLimit := header.Get("RateLimit")
	if !strings.HasPrefix(rateLimit, "limit=5, remaining=4, reset=") {
		t.Fatalf("RateLimit = %q, want a limit=5, remaining=4, reset= prefix", rateLimit)
	}
	reset, err := strconv.Atoi(resetField(t, rateLimit))
	if err != nil {
		t.Fatalf("parsing RateLimit reset failed: %v", err)
	}
	if reset <= 0 || reset > 3600 {
		t.Fatalf("RateLimit reset = %d, want within the aligned 3600s window", reset)
	}
	if got := header.Get("RateLimit-Policy"); got != `"posts";q=5;w=3600` {
		t.Fatalf("RateLimit-Policy = %q, want \"posts\";q=5;w=3600", got)
	}
	if got := header.Get("Retry-After"); got != "" {
		t.Fatalf("Retry-After = %q, want absent on an allowed response", got)
	}
}

func TestRateLimitHeadersOnDeniedResponse(t *testing.T) {
	h := newHarness(t, testRule("posts", "fixed_window", 1))
	h.check(t, "posts", "user-1")

	recorder := h.check(t, "posts", "user-1")

	header := recorder.Header()
	if got := header.Get("X-RateLimit-Limit"); got != "1" {
		t.Fatalf("X-RateLimit-Limit = %q, want 1", got)
	}
	if got := header.Get("X-RateLimit-Remaining"); got != "0" {
		t.Fatalf("X-RateLimit-Remaining = %q, want 0", got)
	}
	retryAfter, err := strconv.Atoi(header.Get("Retry-After"))
	if err != nil {
		t.Fatalf("parsing Retry-After %q failed: %v", header.Get("Retry-After"), err)
	}
	if retryAfter <= 0 {
		t.Fatalf("Retry-After = %d, want positive", retryAfter)
	}
}

func TestResetHeadersDescribeTheSameMoment(t *testing.T) {
	h := newHarness(t, testRule("posts", "fixed_window", 5))

	recorder := h.check(t, "posts", "user-1")
	now := time.Now().Unix()

	resetAt, err := strconv.ParseInt(recorder.Header().Get("X-RateLimit-Reset"), 10, 64)
	if err != nil {
		t.Fatalf("parsing X-RateLimit-Reset %q failed: %v", recorder.Header().Get("X-RateLimit-Reset"), err)
	}
	delta, err := strconv.ParseInt(resetField(t, recorder.Header().Get("RateLimit")), 10, 64)
	if err != nil {
		t.Fatalf("parsing RateLimit reset failed: %v", err)
	}

	drift := (resetAt - now) - delta
	if drift < -1 || drift > 1 {
		t.Fatalf("X-RateLimit-Reset is %ds away but RateLimit reset is %ds, drift %ds", resetAt-now, delta, drift)
	}
}

func resetField(t *testing.T, header string) string {
	t.Helper()
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if value, found := strings.CutPrefix(part, "reset="); found {
			return value
		}
	}
	t.Fatalf("RateLimit header %q has no reset field", header)
	return ""
}

func TestPeekSendsTheSameHeaders(t *testing.T) {
	h := newHarness(t, testRule("posts", "fixed_window", 5))

	recorder := h.do(t, http.MethodHead, "/check?rule=posts&client_id=user-1", "")

	for _, name := range []string{"X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset", "RateLimit", "RateLimit-Policy"} {
		if recorder.Header().Get(name) == "" {
			t.Fatalf("%s is empty on a peek response", name)
		}
	}
}

func TestWholeSeconds(t *testing.T) {
	cases := map[string]struct {
		input time.Duration
		want  int
	}{
		"negative":       {-time.Second, 0},
		"zero":           {0, 0},
		"sub second":     {time.Nanosecond, 1},
		"exact second":   {time.Second, 1},
		"rounds up":      {1200 * time.Millisecond, 2},
		"rounds up half": {2500 * time.Millisecond, 3},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := wholeSeconds(tc.input); got != tc.want {
				t.Fatalf("wholeSeconds(%v) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

func TestStatusFor(t *testing.T) {
	if got := statusFor(algorithms.Result{Allowed: true}); got != http.StatusOK {
		t.Fatalf("statusFor(allowed) = %d, want %d", got, http.StatusOK)
	}
	if got := statusFor(algorithms.Result{Allowed: false}); got != http.StatusTooManyRequests {
		t.Fatalf("statusFor(denied) = %d, want %d", got, http.StatusTooManyRequests)
	}
}

func TestClientIP(t *testing.T) {
	cases := map[string]struct {
		trustProxy bool
		remoteAddr string
		flyHeader  string
		want       string
	}{
		"untrusted proxy ignores the header": {false, "10.0.0.1:5000", "203.0.113.9", "10.0.0.1"},
		"trusted proxy uses the header":      {true, "10.0.0.1:5000", "203.0.113.9", "203.0.113.9"},
		"trusted proxy without a header":     {true, "10.0.0.1:5000", "", "10.0.0.1"},
		"remote address without a port":      {false, "10.0.0.1", "", "10.0.0.1"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := &Server{trustProxy: tc.trustProxy}
			request := httptest.NewRequest(http.MethodHead, "/check", nil)
			request.RemoteAddr = tc.remoteAddr
			if tc.flyHeader != "" {
				request.Header.Set("Fly-Client-IP", tc.flyHeader)
			}

			if got := s.clientIP(request); got != tc.want {
				t.Fatalf("clientIP() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestProbeAllowsTheFullBudget(t *testing.T) {
	h := newHarness(t, testRule("posts", "fixed_window", 1000))

	for i := 0; i < probeLimit; i++ {
		if recorder := h.probe(t, "10.0.0.1:5000"); recorder.Code != http.StatusOK {
			t.Fatalf("probe %d returned %d, want %d", i+1, recorder.Code, http.StatusOK)
		}
	}
}

func TestProbeThrottlesBeyondTheBudget(t *testing.T) {
	h := newHarness(t, testRule("posts", "fixed_window", 1000))

	throttled := 0
	var lastThrottled *httptest.ResponseRecorder
	for i := 0; i < 2*probeLimit+1; i++ {
		recorder := h.probe(t, "10.0.0.1:5000")
		if recorder.Code == http.StatusTooManyRequests {
			throttled++
			lastThrottled = recorder
		}
	}

	if throttled == 0 {
		t.Fatalf("no probe was throttled across %d requests, want at least one", 2*probeLimit+1)
	}
	if lastThrottled.Header().Get("Retry-After") == "" {
		t.Fatal("throttled probe has no Retry-After header")
	}
	if got := counterTotal(t, h.metrics, "ratelimit_probes_throttled_total", nil); got != float64(throttled) {
		t.Fatalf("probes_throttled_total = %v, want %d", got, throttled)
	}
}

func TestProbeBudgetsArePerClient(t *testing.T) {
	h := newHarness(t, testRule("posts", "fixed_window", 1000))
	for i := 0; i < 2*probeLimit+1; i++ {
		h.probe(t, "10.0.0.1:5000")
	}

	recorder := h.probe(t, "10.0.0.2:5000")

	if recorder.Code != http.StatusOK {
		t.Fatalf("second client got %d, want %d", recorder.Code, http.StatusOK)
	}
}

func TestProbeCountersStayOutOfTheMainEngine(t *testing.T) {
	h := newHarness(t, testRule("posts", "fixed_window", 1000))

	for i := 0; i < probeLimit; i++ {
		h.probe(t, "10.0.0.1:5000")
	}

	if entries := h.engine.Stats().Entries; entries != 0 {
		t.Fatalf("main engine holds %d entries after probing, want the probe limiter to use its own engine", entries)
	}
}

func TestInstrumentRecordsTheRoutePattern(t *testing.T) {
	h := newHarness(t, testRule("posts", "fixed_window", 5))

	h.check(t, "posts", "user-1")

	labels := map[string]string{"route": "POST /check", "method": http.MethodPost, "status": "200"}
	if got := counterTotal(t, h.metrics, "ratelimit_http_requests_total", labels); got != 1 {
		t.Fatalf("http_requests_total%v = %v, want 1", labels, got)
	}
}

func TestInstrumentLabelsUnmatchedRoutes(t *testing.T) {
	h := newHarness(t)

	h.do(t, http.MethodGet, "/nope", "")

	labels := map[string]string{"route": "unmatched", "status": "404"}
	if got := counterTotal(t, h.metrics, "ratelimit_http_requests_total", labels); got != 1 {
		t.Fatalf("http_requests_total%v = %v, want 1", labels, got)
	}
}

func TestInstrumentRecordsTheRealStatus(t *testing.T) {
	h := newHarness(t, testRule("posts", "fixed_window", 1))
	h.check(t, "posts", "user-1")

	h.check(t, "posts", "user-1")

	denied := map[string]string{"route": "POST /check", "status": "429"}
	if got := counterTotal(t, h.metrics, "ratelimit_http_requests_total", denied); got != 1 {
		t.Fatalf("http_requests_total%v = %v, want 1", denied, got)
	}
	allowed := map[string]string{"route": "POST /check", "status": "200"}
	if got := counterTotal(t, h.metrics, "ratelimit_http_requests_total", allowed); got != 1 {
		t.Fatalf("http_requests_total%v = %v, want 1", allowed, got)
	}
}

func TestInstrumentRecordsEveryRoute(t *testing.T) {
	h := newHarness(t, testRule("posts", "fixed_window", 5))

	h.do(t, http.MethodGet, "/healthz", "")
	h.do(t, http.MethodGet, "/rules", "")
	h.do(t, http.MethodHead, "/check?rule=posts&client_id=user-1", "")

	for _, route := range []string{"GET /healthz", "GET /rules", "HEAD /check"} {
		labels := map[string]string{"route": route}
		if got := counterTotal(t, h.metrics, "ratelimit_http_requests_total", labels); got != 1 {
			t.Fatalf("http_requests_total for %s = %v, want 1", route, got)
		}
	}
}
