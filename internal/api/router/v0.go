// Package router contains API routing logic
package router

import (
	"github.com/danielgtaylor/huma/v2"

	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	v0auth "github.com/modelcontextprotocol/registry/internal/api/handlers/v0/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/service"
	"github.com/modelcontextprotocol/registry/internal/telemetry"
)

func RegisterV0Routes(
	api huma.API, cfg *config.Config, registry service.RegistryService, metrics *telemetry.Metrics,
) {
	v0.RegisterHealthEndpoint(api, cfg, metrics)
	v0.RegisterPingEndpoint(api)
	v0.RegisterServersEndpoints(api, registry)
	v0auth.RegisterAuthEndpoints(api, cfg)
	v0.RegisterPublishEndpoint(api, registry, cfg)
}
