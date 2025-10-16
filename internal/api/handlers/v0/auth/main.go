package auth

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/modelcontextprotocol/registry/internal/config"
)

// RegisterAuthEndpoints registers all authentication endpoints with a custom path prefix
func RegisterAuthEndpoints(api huma.API, pathPrefix string, cfg *config.Config) {
	// Register GitHub access token authentication endpoint
	RegisterGitHubATEndpoint(api, pathPrefix, cfg)

	// Register GitHub OIDC authentication endpoint
	RegisterGitHubOIDCEndpoint(api, pathPrefix, cfg)

	// Register configurable OIDC authentication endpoints
	RegisterOIDCEndpoints(api, pathPrefix, cfg)

	// Register DNS-based authentication endpoint
	RegisterDNSEndpoint(api, pathPrefix, cfg)

	// Register HTTP-based authentication endpoint
	RegisterHTTPEndpoint(api, pathPrefix, cfg)

	// Register anonymous authentication endpoint
	RegisterNoneEndpoint(api, pathPrefix, cfg)
}
