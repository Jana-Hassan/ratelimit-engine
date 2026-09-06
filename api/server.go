package api

import (
	"net/http"

	"github.com/Jana-Hassan/ratelimit-engine/algorithms"
	"github.com/Jana-Hassan/ratelimit-engine/engine"
	"github.com/Jana-Hassan/ratelimit-engine/limiter"
	"github.com/Jana-Hassan/ratelimit-engine/metrics"
)

const (
	probeShardCapacity = 1024
	probeLimit         = 60
	probeWindowSec     = 60
)

type Server struct {
	limiter    *limiter.Limiter
	engine     *engine.Engine
	metrics    *metrics.Metrics
	probe      algorithms.Algorithm
	probeRule  algorithms.Rule
	trustProxy bool
	mux        *http.ServeMux
	handler    http.Handler
}

func NewServer(l *limiter.Limiter, e *engine.Engine, m *metrics.Metrics, trustProxy bool) *Server {
	s := &Server{
		limiter:   l,
		engine:    e,
		metrics:   m,
		probe:     algorithms.NewFixedWindow(engine.NewEngine(probeShardCapacity)),
		probeRule: algorithms.Rule{Name: "probe", Limit: probeLimit, WindowSec: probeWindowSec},

		trustProxy: trustProxy,
		mux:        http.NewServeMux(),
	}
	s.routes()
	s.handler = s.instrument(s.mux)
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("POST /check", s.handleCheck)
	s.mux.HandleFunc("HEAD /check", s.limitProbe(s.handlePeek))
	s.mux.HandleFunc("GET /rules", s.handleListRules)
	s.mux.HandleFunc("POST /rules", s.handleSetRule)
	s.mux.HandleFunc("DELETE /rules/{name}", s.handleDeleteRule)
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("GET /stats", s.handleStats)
	s.mux.Handle("GET /metrics", s.metricsHandler())
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}
