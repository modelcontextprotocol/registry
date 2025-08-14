// Package router contains API routing logic
package router

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/service"
)

// NewHumaAPI creates a new Huma API with all routes registered
//
//nolint:ireturn // huma.API is the expected interface type for Huma APIs
func NewHumaAPI(cfg *config.Config, registry service.RegistryService, mux *http.ServeMux) huma.API {
	// Create Huma API configuration
	humaConfig := huma.DefaultConfig("MCP Registry API", "1.0.0")
	humaConfig.Info.Description = "A community driven registry service for Model Context Protocol (MCP) servers."
	// Disable $schema property in responses: https://github.com/danielgtaylor/huma/issues/230
	humaConfig.CreateHooks = []func(huma.Config) huma.Config{}

	// Create a new API using humago adapter for standard library
	api := humago.New(mux, humaConfig)

	// Register routes for all API versions
	RegisterV0Routes(api, cfg, registry)

	// Add redirect from / to /docs
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/docs", http.StatusMovedPermanently)
		}
	})

	return api
}
