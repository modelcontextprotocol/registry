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
	api huma.API, cfg *config.Config, registry service.RegistryService, metrics *telemetry.Metrics, versionInfo *v0.VersionBody,
) {
	v0.RegisterHealthEndpoint(api, "/v0", cfg, metrics)
	v0.RegisterPingEndpoint(api, "/v0")
	v0.RegisterVersionEndpoint(api, "/v0", versionInfo)
	v0.RegisterServersEndpoints(api, "/v0", registry)
	v0.RegisterEditEndpoints(api, "/v0", registry, cfg)
	v0auth.RegisterAuthEndpoints(api, "/v0", cfg)
	v0.RegisterPublishEndpoint(api, "/v0", registry, cfg)
}

func RegisterV0_1Routes(
	api huma.API, cfg *config.Config, registry service.RegistryService, metrics *telemetry.Metrics, versionInfo *v0.VersionBody,
) {
	v0.RegisterHealthEndpoint(api, "/v0.1", cfg, metrics)
	v0.RegisterPingEndpoint(api, "/v0.1")
	v0.RegisterVersionEndpoint(api, "/v0.1", versionInfo)
	v0.RegisterServersEndpoints(api, "/v0.1", registry)
	v0.RegisterEditEndpoints(api, "/v0.1", registry, cfg)
	v0auth.RegisterAuthEndpoints(api, "/v0.1", cfg)
	v0.RegisterPublishEndpoint(api, "/v0.1", registry, cfg)
}
