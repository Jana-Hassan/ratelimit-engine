package api

import (
	"net/http"

	"github.com/Jana-Hassan/ratelimit-engine/algorithms"
	"github.com/Jana-Hassan/ratelimit-engine/engine"
	"github.com/Jana-Hassan/ratelimit-engine/limiter"
)

const (
	probeShardCapacity = 1024
	probeLimit         = 60
	probeWindowSec     = 60
)

type Server struct {
	limiter    *limiter.Limiter
	probe      algorithms.Algorithm
	probeRule  algorithms.Rule
	trustProxy bool
	mux        *http.ServeMux
}

func NewServer(l *limiter.Limiter, trustProxy bool) *Server {
	s := &Server{
		limiter:   l,
		probe:     algorithms.NewFixedWindow(engine.NewEngine(probeShardCapacity)),
		probeRule: algorithms.Rule{Name: "probe", Limit: probeLimit, WindowSec: probeWindowSec},

		trustProxy: trustProxy,
		mux:        http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("POST /check", s.handleCheck)
	s.mux.HandleFunc("HEAD /check", s.limitProbe(s.handlePeek))
	s.mux.HandleFunc("GET /rules", s.handleListRules)
	s.mux.HandleFunc("POST /rules", s.handleSetRule)
	s.mux.HandleFunc("DELETE /rules/{name}", s.handleDeleteRule)
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}
