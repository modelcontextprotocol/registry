package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/danielgtaylor/huma/v2"
	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
	"golang.org/x/oauth2"
)

// OIDCTokenExchangeInput represents the input for OIDC token exchange
type OIDCTokenExchangeInput struct {
	Body struct {
		OIDCToken string `json:"oidc_token" doc:"OIDC ID token from any provider" required:"true"`
	}
}

// OIDCStartInput represents the input for OIDC authorization start
type OIDCStartInput struct {
	RedirectURI string `query:"redirect_uri" doc:"Optional redirect URI after authentication"`
}

// OIDCCallbackInput represents the input for OIDC callback
type OIDCCallbackInput struct {
	Code  string `query:"code" doc:"Authorization code from OIDC provider" required:"true"`
	State string `query:"state" doc:"State parameter for CSRF protection" required:"true"`
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
	GetAuthorizationURL(state, nonce string, redirectURI string) string
	ExchangeCodeForToken(ctx context.Context, code string, redirectURI string) (string, error)
}

// StandardOIDCValidator validates OIDC tokens using go-oidc library
type StandardOIDCValidator struct {
	provider     *oidc.Provider
	verifier     *oidc.IDTokenVerifier
	oauth2Config *oauth2.Config
}

// NewStandardOIDCValidator creates a new standard OIDC validator using go-oidc
func NewStandardOIDCValidator(issuer, clientID, clientSecret string) (*StandardOIDCValidator, error) {
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

	// Create OAuth2 config for authorization flow
	oauth2Config := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
	}

	return &StandardOIDCValidator{
		provider:     provider,
		verifier:     verifier,
		oauth2Config: oauth2Config,
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

// GetAuthorizationURL constructs the OIDC authorization URL using oauth2
func (v *StandardOIDCValidator) GetAuthorizationURL(state, nonce string, redirectURI string) string {
	// Update redirect URI for this request
	config := *v.oauth2Config
	config.RedirectURL = redirectURI

	// Add nonce as additional parameter
	authURL := config.AuthCodeURL(state, oauth2.SetAuthURLParam("nonce", nonce))
	return authURL
}

// ExchangeCodeForToken exchanges authorization code for ID token using oauth2
func (v *StandardOIDCValidator) ExchangeCodeForToken(ctx context.Context, code string, redirectURI string) (string, error) {
	// Update redirect URI for this exchange
	config := *v.oauth2Config
	config.RedirectURL = redirectURI

	// Exchange authorization code for token
	token, err := config.Exchange(ctx, code)
	if err != nil {
		return "", fmt.Errorf("failed to exchange code for token: %w", err)
	}

	// Extract ID token from OAuth2 token
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return "", fmt.Errorf("no ID token found in OAuth2 response")
	}

	return rawIDToken, nil
}

// OIDCHandler handles configurable OIDC authentication
type OIDCHandler struct {
	config     *config.Config
	jwtManager *auth.JWTManager
	validator  GenericOIDCValidator
	sessions   map[string]OIDCSession // In-memory state storage for now
}

// OIDCSession stores OIDC flow state
type OIDCSession struct {
	State       string
	Nonce       string
	RedirectURI string
	CreatedAt   time.Time
}

// NewOIDCHandler creates a new OIDC handler
func NewOIDCHandler(cfg *config.Config) *OIDCHandler {
	if !cfg.OIDCEnabled {
		panic("OIDC is not enabled - should not create OIDC handler")
	}
	if cfg.OIDCIssuer == "" {
		panic("OIDC issuer is required when OIDC is enabled")
	}

	validator, err := NewStandardOIDCValidator(cfg.OIDCIssuer, cfg.OIDCClientID, cfg.OIDCClientSecret)
	if err != nil {
		panic(fmt.Sprintf("Failed to initialize OIDC validator: %v", err))
	}

	return &OIDCHandler{
		config:     cfg,
		jwtManager: auth.NewJWTManager(cfg),
		validator:  validator,
		sessions:   make(map[string]OIDCSession),
	}
}

// SetValidator sets a custom OIDC validator (used for testing)
func (h *OIDCHandler) SetValidator(validator GenericOIDCValidator) {
	h.validator = validator
}

// RegisterOIDCEndpoints registers all OIDC authentication endpoints
func RegisterOIDCEndpoints(api huma.API, cfg *config.Config) {
	if !cfg.OIDCEnabled {
		return // Skip registration if OIDC is not enabled
	}

	handler := NewOIDCHandler(cfg)

	// Direct token exchange endpoint
	huma.Register(api, huma.Operation{
		OperationID: "exchange-oidc-token",
		Method:      http.MethodPost,
		Path:        "/v0/auth/oidc",
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

	// Authorization start endpoint
	huma.Register(api, huma.Operation{
		OperationID: "oidc-auth-start",
		Method:      http.MethodGet,
		Path:        "/v0/auth/oidc/start",
		Summary:     "Start OIDC authorization flow",
		Description: "Redirects user to OIDC provider for authentication",
		Tags:        []string{"auth"},
	}, func(ctx context.Context, input *OIDCStartInput) (*v0.Response[map[string]string], error) {
		authURL, err := handler.StartAuth(ctx, input.RedirectURI)
		if err != nil {
			return nil, huma.Error400BadRequest("Failed to start OIDC flow", err)
		}

		return &v0.Response[map[string]string]{
			Body: map[string]string{
				"authorization_url": authURL,
				"message":           "Visit the authorization URL to complete authentication",
			},
		}, nil
	})

	// Authorization callback endpoint
	huma.Register(api, huma.Operation{
		OperationID: "oidc-auth-callback",
		Method:      http.MethodGet,
		Path:        "/v0/auth/oidc/callback",
		Summary:     "Handle OIDC authorization callback",
		Description: "Handles the callback from OIDC provider after user authorization",
		Tags:        []string{"auth"},
	}, func(ctx context.Context, input *OIDCCallbackInput) (*v0.Response[auth.TokenResponse], error) {
		response, err := handler.HandleCallback(ctx, input.Code, input.State)
		if err != nil {
			return nil, huma.Error400BadRequest("Failed to handle OIDC callback", err)
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

// StartAuth initiates the OIDC authorization flow
func (h *OIDCHandler) StartAuth(_ context.Context, redirectURI string) (string, error) {
	// Generate state and nonce for security
	state, err := generateRandomString(32)
	if err != nil {
		return "", fmt.Errorf("failed to generate state: %w", err)
	}

	nonce, err := generateRandomString(32)
	if err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Store session for callback validation
	session := OIDCSession{
		State:       state,
		Nonce:       nonce,
		RedirectURI: redirectURI,
		CreatedAt:   time.Now(),
	}
	h.sessions[state] = session

	// Build callback URI - use configured base URL or default
	callbackURI := "/v0/auth/oidc/callback"

	// Get authorization URL
	authURL := h.validator.GetAuthorizationURL(state, nonce, callbackURI)

	return authURL, nil
}

// HandleCallback handles the OIDC callback
func (h *OIDCHandler) HandleCallback(ctx context.Context, code, state string) (*auth.TokenResponse, error) {
	// Validate state and retrieve session
	session, exists := h.sessions[state]
	if !exists {
		return nil, fmt.Errorf("invalid state parameter")
	}

	// Clean up session
	delete(h.sessions, state)

	// Check session expiry (5 minutes)
	if time.Since(session.CreatedAt) > 5*time.Minute {
		return nil, fmt.Errorf("authentication session expired")
	}

	// Exchange authorization code for tokens
	idToken, err := h.validator.ExchangeCodeForToken(ctx, code, "/v0/auth/oidc/callback")
	if err != nil {
		return nil, fmt.Errorf("failed to exchange code for token: %w", err)
	}

	// Now validate the ID token and generate registry token
	return h.ExchangeToken(ctx, idToken)
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

// generateRandomString generates a cryptographically secure random string
func generateRandomString(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(bytes)[:length], nil
}
