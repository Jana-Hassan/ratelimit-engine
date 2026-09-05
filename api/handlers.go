package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Jana-Hassan/ratelimit-engine/algorithms"
	"github.com/Jana-Hassan/ratelimit-engine/limiter"
)

const maxBodyBytes = 1 << 20

type checkRequest struct {
	Rule     string `json:"rule"`
	ClientID string `json:"client_id"`
}

type checkResponse struct {
	Allowed   bool `json:"allowed"`
	Remaining int  `json:"remaining"`
	ResetIn   int  `json:"reset_in"`
	RetryIn   int  `json:"retry_in"`
}

type rulePayload struct {
	Name      string `json:"name"`
	Limit     int    `json:"limit"`
	WindowSec int    `json:"window_sec"`
	Algorithm string `json:"algorithm"`
}

func (p rulePayload) toRule() limiter.Rule {
	return limiter.Rule{
		Rule:      algorithms.Rule{Name: p.Name, Limit: p.Limit, WindowSec: p.WindowSec},
		Algorithm: p.Algorithm,
	}
}

func fromRule(rule limiter.Rule) rulePayload {
	return rulePayload{
		Name:      rule.Name,
		Limit:     rule.Limit,
		WindowSec: rule.WindowSec,
		Algorithm: rule.Algorithm,
	}
}

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeLimiterError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, limiter.ErrRuleNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func (s *Server) handleCheck(w http.ResponseWriter, r *http.Request) {
	var request checkRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if request.Rule == "" || request.ClientID == "" {
		writeError(w, http.StatusBadRequest, "rule and client_id are required")
		return
	}

	rule, result, err := s.limiter.Check(request.Rule, request.ClientID)
	if err != nil {
		writeLimiterError(w, err)
		return
	}

	writeRateLimitHeaders(w, rule, result)
	writeJSON(w, statusFor(result), checkResponse{
		Allowed:   result.Allowed,
		Remaining: result.Remaining,
		ResetIn:   wholeSeconds(result.ResetIn),
		RetryIn:   wholeSeconds(result.RetryIn),
	})
}

func (s *Server) handlePeek(w http.ResponseWriter, r *http.Request) {
	ruleName := r.URL.Query().Get("rule")
	clientID := r.URL.Query().Get("client_id")
	if ruleName == "" || clientID == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	rule, result, err := s.limiter.Peek(ruleName, clientID)
	if err != nil {
		if errors.Is(err, limiter.ErrRuleNotFound) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	writeRateLimitHeaders(w, rule, result)
	w.WriteHeader(statusFor(result))
}

func (s *Server) handleListRules(w http.ResponseWriter, r *http.Request) {
	rules := s.limiter.Rules().List()
	payload := make([]rulePayload, 0, len(rules))
	for _, rule := range rules {
		payload = append(payload, fromRule(rule))
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleSetRule(w http.ResponseWriter, r *http.Request) {
	var payload rulePayload
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if payload.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if payload.Limit <= 0 || payload.WindowSec <= 0 {
		writeError(w, http.StatusBadRequest, "limit and window_sec must be positive")
		return
	}
	if !s.limiter.HasAlgorithm(payload.Algorithm) {
		writeError(w, http.StatusBadRequest, "unknown algorithm")
		return
	}

	if err := s.limiter.Rules().Set(payload.toRule()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist rule")
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if _, found := s.limiter.Rules().Get(name); !found {
		writeError(w, http.StatusNotFound, limiter.ErrRuleNotFound.Error())
		return
	}
	if err := s.limiter.Rules().Delete(name); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist rule deletion")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
