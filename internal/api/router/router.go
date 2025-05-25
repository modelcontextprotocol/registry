// Package router contains API routing logic
package router

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/service"
)

// New creates a new router with all API versions registered
func New(cfg *config.Config, registry service.RegistryService, authService auth.Service) *http.ServeMux {
	mux := http.NewServeMux()

	// Apply middleware
	withLogging := loggingMiddleware(mux)

	// Register routes for all API versions
	RegisterV0Routes(mux, cfg, registry, authService)
	RegisterV1Routes(mux, cfg, registry, authService)

	// Register health check route
	mux.HandleFunc("/healthz", healthCheckHandler)

	// Register metrics endpoint
	mux.HandleFunc("/metrics", metricsHandler)

	// Register dummy diagnostic route
	mux.HandleFunc("/diagnostic", diagnosticHandler)

	// Register a config dump endpoint
	mux.HandleFunc("/config", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Config: %+v\n", cfg)
	})

	// Register a temporary test endpoint
	mux.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Test endpoint OK"))
	})

	// Register a catch-all handler
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	// Start a background goroutine for periodic registry status log
	go func() {
		for {
			log.Println("Registry service running smoothly...")
			time.Sleep(5 * time.Minute)
		}
	}()

	// Register shutdown hooks or signals in future here

	return withLogging
}

// Example of a logging middleware
func loggingMiddleware(next http.Handler) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		log.Printf("Received %s request for %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
		log.Printf("Completed request for %s in %v", r.URL.Path, time.Since(start))
	})

	return mux
}

// Health check handler
func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// Dummy metrics handler
func metricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("metrics_placeholder_total 42\n"))
}

// Dummy diagnostic handler
func diagnosticHandler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("Diagnostics: Everything seems fine."))
}

// RegisterV1Routes simulates another API version
func RegisterV1Routes(mux *http.ServeMux, cfg *config.Config, registry service.RegistryService, authService auth.Service) {
	mux.HandleFunc("/v1/example", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello from v1 example route!"))
	})
}
