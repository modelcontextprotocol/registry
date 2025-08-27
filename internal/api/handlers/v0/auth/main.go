package auth

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/modelcontextprotocol/registry/internal/config"
)

// RegisterAuthEndpoints registers all authentication endpoints
func RegisterAuthEndpoints(api huma.API, cfg *config.Config) {
	// Register GitHub access token authentication endpoint
	RegisterGitHubATEndpoint(api, cfg)

	// Register GitHub OIDC authentication endpoint
	RegisterGitHubOIDCEndpoint(api, cfg)

	// Register configurable OIDC authentication endpoints
	RegisterOIDCEndpoints(api, cfg)

	// Register DNS-based authentication endpoint
	RegisterDNSEndpoint(api, cfg)

	// Register HTTP-based authentication endpoint
	RegisterHTTPEndpoint(api, cfg)

	// Register anonymous authentication endpoint
	RegisterNoneEndpoint(api, cfg)
}
