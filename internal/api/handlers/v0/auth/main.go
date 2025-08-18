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

	// Register anonymous authentication endpoint
	RegisterNoneEndpoint(api, cfg)

	// Future auth providers can be registered here:
	// RegisterGitLabEndpoint(api, cfg)
	// RegisterOIDCEndpoint(api, cfg)
}
