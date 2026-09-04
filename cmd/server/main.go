package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/Jana-Hassan/ratelimit-engine/api"
	"github.com/Jana-Hassan/ratelimit-engine/engine"
	"github.com/Jana-Hassan/ratelimit-engine/limiter"
)

func main() {
	addr := flag.String("addr", ":8080", "http listen address")
	shardCapacity := flag.Int("shard-capacity", 1024, "entries kept per cache shard")
	trustProxy := flag.Bool("trust-proxy", false, "trust the Fly-Client-IP header for probe rate limiting")
	flag.Parse()

	server := &http.Server{
		Addr:         *addr,
		Handler:      api.NewServer(limiter.New(engine.NewEngine(*shardCapacity)), *trustProxy),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("listening on %s", *addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}
