package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/danielgtaylor/huma/v2"
	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
)

// OIDCTokenExchangeInput represents the input for OIDC token exchange
type OIDCTokenExchangeInput struct {
	Body struct {
		OIDCToken string `json:"oidc_token" doc:"OIDC ID token from any provider" required:"true"`
	}
}

// OIDCClaims represents the claims we extract from any OIDC token
type OIDCClaims struct {
	Subject     string         `json:"sub"`
	Issuer      string         `json:"iss"`
	Audience    []string       `json:"aud"`
	ExtraClaims map[string]any `json:"-"`
}

// GenericOIDCValidator defines the interface for validating OIDC tokens from any provider
type GenericOIDCValidator interface {
	ValidateToken(ctx context.Context, token string) (*OIDCClaims, error)
}

// StandardOIDCValidator validates OIDC tokens using go-oidc library
type StandardOIDCValidator struct {
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
}

// NewStandardOIDCValidator creates a new standard OIDC validator using go-oidc
func NewStandardOIDCValidator(issuer, clientID string) (*StandardOIDCValidator, error) {
	ctx := context.Background()

	// Initialize the OIDC provider
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize OIDC provider: %w", err)
	}

	// Create ID token verifier
	verifierConfig := &oidc.Config{
		ClientID: clientID,
	}
	verifier := provider.Verifier(verifierConfig)

	return &StandardOIDCValidator{
		provider: provider,
		verifier: verifier,
	}, nil
}

// ValidateToken validates an OIDC ID token using go-oidc library
func (v *StandardOIDCValidator) ValidateToken(ctx context.Context, tokenString string) (*OIDCClaims, error) {
	// Verify and parse the ID token using go-oidc
	idToken, err := v.verifier.Verify(ctx, tokenString)
	if err != nil {
		return nil, fmt.Errorf("failed to verify ID token: %w", err)
	}

	// Extract all claims
	var allClaims map[string]any
	if err := idToken.Claims(&allClaims); err != nil {
		return nil, fmt.Errorf("failed to extract claims: %w", err)
	}

	// Build our claims structure
	oidcClaims := &OIDCClaims{
		Subject:     idToken.Subject,
		Issuer:      idToken.Issuer,
		ExtraClaims: make(map[string]any),
	}

	// Extract audience
	if aud, ok := allClaims["aud"]; ok {
		switch v := aud.(type) {
		case string:
			oidcClaims.Audience = []string{v}
		case []any:
			for _, a := range v {
				if s, ok := a.(string); ok {
					oidcClaims.Audience = append(oidcClaims.Audience, s)
				}
			}
		}
	}

	// Store all non-standard claims in ExtraClaims
	standardClaims := map[string]bool{
		"iss": true, "sub": true, "aud": true, "exp": true, "nbf": true, "iat": true, "jti": true,
	}

	for key, value := range allClaims {
		if !standardClaims[key] {
			oidcClaims.ExtraClaims[key] = value
		}
	}

	return oidcClaims, nil
}

// OIDCHandler handles configurable OIDC authentication
type OIDCHandler struct {
	config     *config.Config
	jwtManager *auth.JWTManager
	validator  GenericOIDCValidator
}

// NewOIDCHandler creates a new OIDC handler
func NewOIDCHandler(cfg *config.Config) *OIDCHandler {
	if !cfg.OIDCEnabled {
		panic("OIDC is not enabled - should not create OIDC handler")
	}
	if cfg.OIDCIssuer == "" {
		panic("OIDC issuer is required when OIDC is enabled")
	}

	validator, err := NewStandardOIDCValidator(cfg.OIDCIssuer, cfg.OIDCClientID)
	if err != nil {
		panic(fmt.Sprintf("Failed to initialize OIDC validator: %v", err))
	}

	return &OIDCHandler{
		config:     cfg,
		jwtManager: auth.NewJWTManager(cfg),
		validator:  validator,
	}
}

// SetValidator sets a custom OIDC validator (used for testing)
func (h *OIDCHandler) SetValidator(validator GenericOIDCValidator) {
	h.validator = validator
}

// RegisterOIDCEndpoints registers all OIDC authentication endpoints
func RegisterOIDCEndpoints(api huma.API, pathPrefix string, cfg *config.Config) {
	if !cfg.OIDCEnabled {
		return // Skip registration if OIDC is not enabled
	}

	handler := NewOIDCHandler(cfg)

	// Direct token exchange endpoint
	huma.Register(api, huma.Operation{
		OperationID: "exchange-oidc-token" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPost,
		Path:        pathPrefix + "/auth/oidc",
		Summary:     "Exchange OIDC ID token for Registry JWT",
		Description: "Exchange an OIDC ID token from any configured provider for a short-lived Registry JWT token",
		Tags:        []string{"auth"},
	}, func(ctx context.Context, input *OIDCTokenExchangeInput) (*v0.Response[auth.TokenResponse], error) {
		response, err := handler.ExchangeToken(ctx, input.Body.OIDCToken)
		if err != nil {
			return nil, huma.Error401Unauthorized("Token exchange failed", err)
		}

		return &v0.Response[auth.TokenResponse]{
			Body: *response,
		}, nil
	})
}

// ExchangeToken exchanges an OIDC ID token for a Registry JWT token
func (h *OIDCHandler) ExchangeToken(ctx context.Context, oidcToken string) (*auth.TokenResponse, error) {
	// Validate OIDC token
	claims, err := h.validator.ValidateToken(ctx, oidcToken)
	if err != nil {
		return nil, fmt.Errorf("failed to validate OIDC token: %w", err)
	}

	// Validate extra claims if configured
	if err := h.validateExtraClaims(claims); err != nil {
		return nil, fmt.Errorf("extra claims validation failed: %w", err)
	}

	// Build permissions based on claims and configuration
	permissions := h.buildPermissions(claims)

	// Create JWT claims
	jwtClaims := auth.JWTClaims{
		AuthMethod:        auth.MethodOIDC,
		AuthMethodSubject: claims.Subject,
		Permissions:       permissions,
	}

	// Generate Registry JWT token
	tokenResponse, err := h.jwtManager.GenerateTokenResponse(ctx, jwtClaims)
	if err != nil {
		return nil, fmt.Errorf("failed to generate JWT token: %w", err)
	}

	return tokenResponse, nil
}

// validateExtraClaims validates additional claims based on configuration
func (h *OIDCHandler) validateExtraClaims(claims *OIDCClaims) error {
	if h.config.OIDCExtraClaims == "" {
		return nil // No extra validation required
	}

	// Parse extra claims configuration
	var extraClaimsRules []map[string]any
	if err := json.Unmarshal([]byte(h.config.OIDCExtraClaims), &extraClaimsRules); err != nil {
		return fmt.Errorf("invalid extra claims configuration: %w", err)
	}

	// Validate each rule
	for _, rule := range extraClaimsRules {
		for key, expectedValue := range rule {
			actualValue, exists := claims.ExtraClaims[key]
			if !exists {
				return fmt.Errorf("claim validation failed: required claim %s not found", key)
			}

			if actualValue != expectedValue {
				return fmt.Errorf("claim validation failed: %s expected %v, got %v", key, expectedValue, actualValue)
			}
		}
	}

	return nil
}

// buildPermissions builds permissions based on OIDC claims and configuration
func (h *OIDCHandler) buildPermissions(_ *OIDCClaims) []auth.Permission {
	var permissions []auth.Permission

	// Parse permission patterns from configuration
	if h.config.OIDCPublishPerms != "" {
		for _, pattern := range strings.Split(h.config.OIDCPublishPerms, ",") {
			pattern = strings.TrimSpace(pattern)
			if pattern != "" {
				permissions = append(permissions, auth.Permission{
					Action:          auth.PermissionActionPublish,
					ResourcePattern: pattern,
				})
			}
		}
	}

	if h.config.OIDCEditPerms != "" {
		for _, pattern := range strings.Split(h.config.OIDCEditPerms, ",") {
			pattern = strings.TrimSpace(pattern)
			if pattern != "" {
				permissions = append(permissions, auth.Permission{
					Action:          auth.PermissionActionEdit,
					ResourcePattern: pattern,
				})
			}
		}
	}

	return permissions
}
