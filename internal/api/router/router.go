// Package router contains API routing logic
package router

import (
	"net/http"

	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/service"
)

// New creates a new router with all API versions registered
func New(cfg *config.Config, registry service.RegistryService, authService auth.Service) *http.ServeMux {
	mux := http.NewServeMux()

	// Register routes for all API versions
	RegisterV0Routes(mux, cfg, registry, authService)

	// Redirect root to Swagger UI
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/v0/swagger/index.html", http.StatusFound)
	})

	return mux
}
