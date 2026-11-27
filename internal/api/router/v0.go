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

// APIVersionPrefixes lists the supported versioned route prefixes.
var APIVersionPrefixes = []string{"/v0", "/v0.1"}

func registerVersionedRoutes(
	api huma.API, pathPrefix string, cfg *config.Config, registry service.RegistryService, metrics *telemetry.Metrics, versionInfo *v0.VersionBody,
) {
	v0.RegisterHealthEndpoint(api, pathPrefix, cfg, metrics)
	v0.RegisterPingEndpoint(api, pathPrefix)
	v0.RegisterVersionEndpoint(api, pathPrefix, versionInfo)
	v0.RegisterServersEndpoints(api, pathPrefix, registry, cfg.PreparedListingJSON)
	v0.RegisterEditEndpoints(api, pathPrefix, registry, cfg)
	v0.RegisterStatusEndpoints(api, pathPrefix, registry, cfg)
	v0.RegisterAllVersionsStatusEndpoints(api, pathPrefix, registry, cfg)
	v0auth.RegisterAuthEndpoints(api, pathPrefix, cfg)
	v0.RegisterPublishEndpoint(api, pathPrefix, registry, cfg)
	v0.RegisterValidateEndpoint(api, pathPrefix, cfg)
}

// RegisterRoutes registers all endpoints across every supported API version prefix.
func RegisterRoutes(
	api huma.API, cfg *config.Config, registry service.RegistryService, metrics *telemetry.Metrics, versionInfo *v0.VersionBody,
) {
	for _, prefix := range APIVersionPrefixes {
		registerVersionedRoutes(api, prefix, cfg, registry, metrics, versionInfo)
	}
}
