// Package router contains API routing logic
package router

import (
	"net/http"

	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/service"
)

// RegisterV0Routes registers all v0 API routes to the provided router
func RegisterV0Routes(mux *http.ServeMux, cfg *config.Config, registry service.RegistryService, authService auth.Service) {
	// Register v0 endpoints
	mux.HandleFunc("/v0/health", v0.HealthHandler())
	mux.HandleFunc("/v0/servers", v0.ServersHandler(registry))
	mux.HandleFunc("/v0/servers/{id}", v0.ServersDetailHandler(registry))
	mux.HandleFunc("/v0/ping", v0.PingHandler(cfg))
	mux.HandleFunc("/v0/publish", v0.PublishHandler(registry, authService))

	// Register Swagger UI routes
	mux.HandleFunc("/v0/swagger/", v0.SwaggerHandler())
	mux.HandleFunc("/v0/swagger/doc.json", v0.SwaggerJSONHandler())
}
