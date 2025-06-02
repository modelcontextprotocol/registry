package api

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/registry/internal/api/router"
	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/service"
)

// Server represents the HTTP server
type Server struct {
	config      *config.Config
	registry    service.RegistryService
	authService auth.Service
	router      *http.ServeMux
	server      *http.Server
}

// NewServer creates a new HTTP server
func NewServer(cfg *config.Config, registryService service.RegistryService, authService auth.Service) *Server {
	// Create router with all API versions registered
	mux := router.New(cfg, registryService, authService)

	server := &Server{
		config:      cfg,
		registry:    registryService,
		authService: authService,
		router:      mux,
		server: &http.Server{
			Addr:              cfg.ServerAddress,
			Handler:           mux,
			ReadHeaderTimeout: 10 * time.Second,
		},
	}

	return server
}

// Start begins listening for incoming HTTP requests
func (s *Server) Start() error {
	log.Printf("HTTP server starting on %s", s.config.ServerAddress)
	return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}
