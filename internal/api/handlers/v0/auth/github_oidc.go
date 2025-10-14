package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/golang-jwt/jwt/v5"
	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
)

// GitHubOIDCTokenExchangeInput represents the input for GitHub OIDC token exchange
type GitHubOIDCTokenExchangeInput struct {
	Body struct {
		OIDCToken string `json:"oidc_token" doc:"GitHub Actions OIDC token" required:"true"`
	}
}

// GitHubOIDCClaims represents the claims we need from a GitHub OIDC token
type GitHubOIDCClaims struct {
	jwt.RegisteredClaims
	RepositoryOwner string `json:"repository_owner"` // e.g., "octo-org"
}

// JWKS represents a JSON Web Key Set
type JWKS struct {
	Keys []JWK `json:"keys"`
}

// JWK represents a JSON Web Key
type JWK struct {
	KTY string `json:"kty"`
	KID string `json:"kid"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// OIDCValidator defines the interface for OIDC token validation
type OIDCValidator interface {
	ValidateToken(ctx context.Context, token string, audience string) (*GitHubOIDCClaims, error)
}

// GitHubOIDCValidator validates GitHub OIDC tokens
type GitHubOIDCValidator struct {
	jwksURL string
	issuer  string
}

// NewGitHubOIDCValidator creates a new GitHub OIDC validator
func NewGitHubOIDCValidator() *GitHubOIDCValidator {
	return &GitHubOIDCValidator{
		jwksURL: "https://token.actions.githubusercontent.com/.well-known/jwks",
		issuer:  "https://token.actions.githubusercontent.com",
	}
}

// NewMockOIDCValidator creates a mock validator for testing
func NewMockOIDCValidator(jwksURL, issuer string) *GitHubOIDCValidator {
	return &GitHubOIDCValidator{
		jwksURL: jwksURL,
		issuer:  issuer,
	}
}

// ValidateToken validates a GitHub OIDC token
func (v *GitHubOIDCValidator) ValidateToken(ctx context.Context, tokenString string, audience string) (*GitHubOIDCClaims, error) {
	// Parse token to get header for key ID
	token, err := jwt.ParseWithClaims(
		tokenString,
		&GitHubOIDCClaims{},
		func(token *jwt.Token) (any, error) {
			// Get key ID from header
			kid, ok := token.Header["kid"].(string)
			if !ok {
				return nil, fmt.Errorf("missing kid in token header")
			}

			// Find matching public key
			publicKey, err := v.getPublicKey(ctx, kid)
			if err != nil {
				return nil, fmt.Errorf("failed to get public key: %w", err)
			}

			return publicKey, nil
		},
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithExpirationRequired(),
	)

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}
	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	// Extract and validate claims
	claims, ok := token.Claims.(*GitHubOIDCClaims)
	if !ok {
		return nil, fmt.Errorf("invalid token claims")
	}

	// Validate issuer
	if claims.Issuer != v.issuer {
		return nil, fmt.Errorf("invalid issuer: expected %s, got %s", v.issuer, claims.Issuer)
	}

	// Validate audience
	foundAudience := false
	for _, aud := range claims.Audience {
		if aud == audience {
			foundAudience = true
			break
		}
	}
	if !foundAudience {
		return nil, fmt.Errorf("invalid audience: expected %s, got %v", audience, claims.Audience)
	}

	// Validate repository format
	if claims.RepositoryOwner == "" {
		return nil, fmt.Errorf("repository owner claim is required")
	}

	return claims, nil
}

// fetchJWKS fetches the JSON Web Key Set from GitHub
func (v *GitHubOIDCValidator) fetchJWKS(ctx context.Context) (*JWKS, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("JWKS endpoint returned status %d: %s", resp.StatusCode, body)
	}

	var jwks JWKS
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return nil, fmt.Errorf("failed to decode JWKS: %w", err)
	}

	return &jwks, nil
}

// getPublicKey extracts the RSA public key for the given key ID
func (v *GitHubOIDCValidator) getPublicKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	// Fetch JWKS from GitHub
	jwks, err := v.fetchJWKS(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS: %w", err)
	}

	for _, key := range jwks.Keys {
		if key.KID == kid {
			return v.parseRSAPublicKey(key)
		}
	}
	return nil, fmt.Errorf("key with ID %s not found", kid)
}

// parseRSAPublicKey converts JWK to RSA public key
func (v *GitHubOIDCValidator) parseRSAPublicKey(jwk JWK) (*rsa.PublicKey, error) {
	if jwk.KTY != "RSA" {
		return nil, fmt.Errorf("invalid key type: expected RSA, got %s", jwk.KTY)
	}

	// Decode modulus (n)
	nBytes, err := base64.RawURLEncoding.DecodeString(jwk.N)
	if err != nil {
		return nil, fmt.Errorf("failed to decode modulus: %w", err)
	}

	// Decode exponent (e)
	eBytes, err := base64.RawURLEncoding.DecodeString(jwk.E)
	if err != nil {
		return nil, fmt.Errorf("failed to decode exponent: %w", err)
	}

	// Convert to big integers
	n := new(big.Int).SetBytes(nBytes)
	e := new(big.Int).SetBytes(eBytes)

	return &rsa.PublicKey{
		N: n,
		E: int(e.Int64()),
	}, nil
}

// GitHubOIDCHandler handles GitHub OIDC authentication
type GitHubOIDCHandler struct {
	config     *config.Config
	jwtManager *auth.JWTManager
	validator  OIDCValidator
}

// NewGitHubOIDCHandler creates a new GitHub OIDC handler
func NewGitHubOIDCHandler(cfg *config.Config) *GitHubOIDCHandler {
	return &GitHubOIDCHandler{
		config:     cfg,
		jwtManager: auth.NewJWTManager(cfg),
		validator:  NewGitHubOIDCValidator(),
	}
}

// SetValidator sets a custom OIDC validator (used for testing)
func (h *GitHubOIDCHandler) SetValidator(validator OIDCValidator) {
	h.validator = validator
}

// RegisterGitHubOIDCEndpoint registers the GitHub OIDC authentication endpoint
func RegisterGitHubOIDCEndpoint(api huma.API, pathPrefix string, cfg *config.Config) {
	handler := NewGitHubOIDCHandler(cfg)

	// GitHub OIDC token exchange endpoint
	huma.Register(api, huma.Operation{
		OperationID: "exchange-github-oidc-token" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPost,
		Path:        pathPrefix + "/auth/github-oidc",
		Summary:     "Exchange GitHub OIDC token for Registry JWT",
		Description: "Exchange a GitHub Actions OIDC token for a short-lived Registry JWT token",
		Tags:        []string{"auth"},
	}, func(ctx context.Context, input *GitHubOIDCTokenExchangeInput) (*v0.Response[auth.TokenResponse], error) {
		response, err := handler.ExchangeToken(ctx, input.Body.OIDCToken)
		if err != nil {
			return nil, huma.Error401Unauthorized("Token exchange failed", err)
		}

		return &v0.Response[auth.TokenResponse]{
			Body: *response,
		}, nil
	})
}

// ExchangeToken exchanges a GitHub OIDC token for a Registry JWT token
func (h *GitHubOIDCHandler) ExchangeToken(ctx context.Context, oidcToken string) (*auth.TokenResponse, error) {
	// Validate OIDC token with audience "mcp-registry"
	claims, err := h.validator.ValidateToken(ctx, oidcToken, "mcp-registry")
	if err != nil {
		return nil, fmt.Errorf("failed to validate OIDC token: %w", err)
	}

	// Extract repository information and build permissions
	permissions := h.buildPermissions(claims)

	// Create JWT claims with GitHub OIDC info
	jwtClaims := auth.JWTClaims{
		AuthMethod:        auth.MethodGitHubOIDC,
		AuthMethodSubject: claims.Subject, // e.g. "repo:octo-org/octo-repo:environment:prod"
		Permissions:       permissions,
	}

	// Generate Registry JWT token
	tokenResponse, err := h.jwtManager.GenerateTokenResponse(ctx, jwtClaims)
	if err != nil {
		return nil, fmt.Errorf("failed to generate JWT token: %w", err)
	}

	return tokenResponse, nil
}

func (h *GitHubOIDCHandler) buildPermissions(claims *GitHubOIDCClaims) []auth.Permission {
	permissions := []auth.Permission{}

	// Validate repository owner name
	if !isValidGitHubName(claims.RepositoryOwner) {
		return nil
	}

	// Grant publish permissions for the repository owner's namespace
	// We grant io.github.<owner>/* rather than io.github./repo/* because many people have monorepo setups where they want to deploy multiple servers from
	// This also reflects GitHub's permission model, in that GitHub Actions can push to any GitHub package in the repository owner's namespace (e.g. for GHCR)
	permissions = append(permissions, auth.Permission{
		Action:          auth.PermissionActionPublish,
		ResourcePattern: fmt.Sprintf("io.github.%s/*", claims.RepositoryOwner),
	})

	return permissions
}
