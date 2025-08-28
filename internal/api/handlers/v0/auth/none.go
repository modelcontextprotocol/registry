package auth

import (
	"context"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
)

// NoneHandler handles anonymous authentication
type NoneHandler struct {
	config     *config.Config
	jwtManager *auth.JWTManager
}

// NewNoneHandler creates a new anonymous authentication handler
func NewNoneHandler(cfg *config.Config) *NoneHandler {
	return &NoneHandler{
		config:     cfg,
		jwtManager: auth.NewJWTManager(cfg),
	}
}

// RegisterNoneEndpoint registers the anonymous authentication endpoint
func RegisterNoneEndpoint(api huma.API, cfg *config.Config) {
	if !cfg.EnableAnonymousAuth {
		return
	}

	handler := NewNoneHandler(cfg)

	// Anonymous token endpoint
	huma.Register(api, huma.Operation{
		OperationID: "get-anonymous-token",
		Method:      http.MethodPost,
		Path:        "/v0/auth/none",
		Summary:     "Get anonymous Registry JWT",
		Description: "Get a short-lived Registry JWT token for publishing to io.modelcontextprotocol.anonymous/* namespace",
		Tags:        []string{"auth"},
	}, func(ctx context.Context, _ *struct{}) (*v0.Response[auth.TokenResponse], error) {
		response, err := handler.GetAnonymousToken(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to generate token", err)
		}

		return &v0.Response[auth.TokenResponse]{
			Body: *response,
		}, nil
	})
}

// GetAnonymousToken generates an anonymous Registry JWT token
func (h *NoneHandler) GetAnonymousToken(ctx context.Context) (*auth.TokenResponse, error) {
	// Build permissions for anonymous namespace only
	permissions := []auth.Permission{
		{
			Action:          auth.PermissionActionPublish,
			ResourcePattern: "io.modelcontextprotocol.anonymous/*",
		},
	}

	// Create JWT claims for anonymous user
	claims := auth.JWTClaims{
		AuthMethod:        auth.MethodNone,
		AuthMethodSubject: "anonymous",
		Permissions:       permissions,
	}

	// Generate Registry JWT token
	tokenResponse, err := h.jwtManager.GenerateTokenResponse(ctx, claims)
	if err != nil {
		return nil, fmt.Errorf("failed to generate JWT token: %w", err)
	}

	return tokenResponse, nil
}
