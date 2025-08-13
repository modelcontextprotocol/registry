// Package router contains API routing logic
package router

import (
	"github.com/danielgtaylor/huma/v2"
	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/service"
)

func RegisterV0Routes(
	api huma.API, cfg *config.Config, registry service.RegistryService, authService auth.Service,
) {
	v0.RegisterHealthEndpoint(api, cfg)
	v0.RegisterPingEndpoint(api)
	v0.RegisterServersEndpoints(api, registry)
	// v0.RegisterAuthEndpoints(api, authService)
	v0.RegisterPublishEndpoint(api, registry, authService)
}
