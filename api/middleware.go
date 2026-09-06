package api

import (
	"fmt"
	"math"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/Jana-Hassan/ratelimit-engine/algorithms"
	"github.com/Jana-Hassan/ratelimit-engine/limiter"
)

type recordingWriter struct {
	http.ResponseWriter
	status int
}

func (s *Server) limitProbe(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result := s.probe.Allow(s.clientIP(r), s.probeRule)
		if !result.Allowed {
			s.metrics.RecordProbeThrottled()
			w.Header().Set("Retry-After", strconv.Itoa(wholeSeconds(result.RetryIn)))
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}

func (s *Server) clientIP(r *http.Request) string {
	if s.trustProxy {
		if ip := r.Header.Get("Fly-Client-IP"); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func writeRateLimitHeaders(w http.ResponseWriter, rule limiter.Rule, result algorithms.Result) {
	header := w.Header()
	resetIn := wholeSeconds(result.ResetIn)

	header.Set("X-RateLimit-Limit", strconv.Itoa(rule.Limit))
	header.Set("X-RateLimit-Remaining", strconv.Itoa(result.Remaining))
	header.Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(result.ResetIn).Unix(), 10))
	header.Set("RateLimit", fmt.Sprintf("limit=%d, remaining=%d, reset=%d", rule.Limit, result.Remaining, resetIn))
	header.Set("RateLimit-Policy", fmt.Sprintf("%q;q=%d;w=%d", rule.Name, rule.Limit, rule.WindowSec))

	if !result.Allowed {
		header.Set("Retry-After", strconv.Itoa(wholeSeconds(result.RetryIn)))
	}
}

func wholeSeconds(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int(math.Ceil(d.Seconds()))
}

func statusFor(result algorithms.Result) int {
	if result.Allowed {
		return http.StatusOK
	}
	return http.StatusTooManyRequests
}


func (w *recordingWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (s *Server) route(r *http.Request) string {
	if _, pattern := s.mux.Handler(r); pattern != "" {
		return pattern
	}
	return "unmatched"
}

func (s *Server) instrument(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route := s.route(r)
		recorder := &recordingWriter{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(recorder, r)
		s.metrics.RecordHTTP(route, r.Method, strconv.Itoa(recorder.status), time.Since(start))
	})
}
