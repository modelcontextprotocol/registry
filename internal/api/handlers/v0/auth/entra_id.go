package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/danielgtaylor/huma/v2"
	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
)

// EntraIDTokenExchangeInput represents the input for Entra ID token exchange
type EntraIDTokenExchangeInput struct {
	Body struct {
		AccessToken string `json:"access_token" doc:"Azure Entra ID access token or ID token" required:"true"`
	}
}

// EntraIDClaims represents the claims we extract from Entra ID tokens
type EntraIDClaims struct {
	Subject          string   `json:"sub"`
	Issuer           string   `json:"iss"`
	Audience         []string `json:"aud"`
	OID              string   `json:"oid"`               // Object ID - unique identifier for the user
	TenantID         string   `json:"tid"`               // Tenant ID
	PreferredUsername string  `json:"preferred_username"` // Usually the UPN (user@domain.com)
	Name             string   `json:"name"`              // Display name
	Email            string   `json:"email"`             // Email address
	AppID            string   `json:"appid"`             // Application ID (for app-only tokens)
	AppDisplayName   string   `json:"app_displayname"`   // Application display name
	IDType           string   `json:"idtyp"`             // Token type: "user" or "app"
}

// EntraIDValidator defines the interface for Entra ID token validation
type EntraIDValidator interface {
	ValidateToken(ctx context.Context, token string) (*EntraIDClaims, error)
}

// StandardEntraIDValidator validates Entra ID tokens using go-oidc library
type StandardEntraIDValidator struct {
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	config   *config.Config
}

// NewStandardEntraIDValidator creates a new Entra ID validator
func NewStandardEntraIDValidator(cfg *config.Config) (*StandardEntraIDValidator, error) {
	if !cfg.EntraIDEnabled {
		return nil, fmt.Errorf("Entra ID authentication is not enabled")
	}

	ctx := context.Background()

	// Construct the issuer URL for the specific tenant
	// Format: https://login.microsoftonline.com/{tenant}/v2.0
	issuer := fmt.Sprintf("https://login.microsoftonline.com/%s/v2.0", cfg.EntraIDTenantID)

	// Initialize the OIDC provider
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Entra ID OIDC provider: %w", err)
	}

	// Create ID token verifier
	verifierConfig := &oidc.Config{
		ClientID:          cfg.EntraIDClientID,
		SkipClientIDCheck: false,
		SkipExpiryCheck:   false,
	}
	verifier := provider.Verifier(verifierConfig)

	return &StandardEntraIDValidator{
		provider: provider,
		verifier: verifier,
		config:   cfg,
	}, nil
}

// ValidateToken validates an Entra ID token
func (v *StandardEntraIDValidator) ValidateToken(ctx context.Context, tokenString string) (*EntraIDClaims, error) {
	// Verify and parse the ID token
	idToken, err := v.verifier.Verify(ctx, tokenString)
	if err != nil {
		return nil, fmt.Errorf("failed to verify Entra ID token: %w", err)
	}

	// Extract all claims
	var allClaims map[string]any
	if err := idToken.Claims(&allClaims); err != nil {
		return nil, fmt.Errorf("failed to extract claims: %w", err)
	}

	// Build our claims structure
	entraClaims := &EntraIDClaims{
		Subject: idToken.Subject,
		Issuer:  idToken.Issuer,
	}

	// Extract audience
	if aud, ok := allClaims["aud"]; ok {
		switch v := aud.(type) {
		case string:
			entraClaims.Audience = []string{v}
		case []any:
			for _, a := range v {
				if s, ok := a.(string); ok {
					entraClaims.Audience = append(entraClaims.Audience, s)
				}
			}
		}
	}

	// Extract Entra ID specific claims
	if oid, ok := allClaims["oid"].(string); ok {
		entraClaims.OID = oid
	}
	if tid, ok := allClaims["tid"].(string); ok {
		entraClaims.TenantID = tid
	}
	if preferred, ok := allClaims["preferred_username"].(string); ok {
		entraClaims.PreferredUsername = preferred
	}
	if name, ok := allClaims["name"].(string); ok {
		entraClaims.Name = name
	}
	if email, ok := allClaims["email"].(string); ok {
		entraClaims.Email = email
	}
	if appid, ok := allClaims["appid"].(string); ok {
		entraClaims.AppID = appid
	}
	if appDisplayName, ok := allClaims["app_displayname"].(string); ok {
		entraClaims.AppDisplayName = appDisplayName
	}
	if idtyp, ok := allClaims["idtyp"].(string); ok {
		entraClaims.IDType = idtyp
	}

	// Validate tenant ID matches configuration
	if v.config.EntraIDTenantID != "" && entraClaims.TenantID != v.config.EntraIDTenantID {
		return nil, fmt.Errorf("token is from unexpected tenant: expected %s, got %s", 
			v.config.EntraIDTenantID, entraClaims.TenantID)
	}

	return entraClaims, nil
}

// EntraIDHandler handles Azure Entra ID authentication
type EntraIDHandler struct {
	config     *config.Config
	jwtManager *auth.JWTManager
	validator  EntraIDValidator
}

// NewEntraIDHandler creates a new Entra ID handler
func NewEntraIDHandler(cfg *config.Config) *EntraIDHandler {
	if !cfg.EntraIDEnabled {
		panic("Entra ID is not enabled - should not create Entra ID handler")
	}

	validator, err := NewStandardEntraIDValidator(cfg)
	if err != nil {
		panic(fmt.Sprintf("Failed to initialize Entra ID validator: %v", err))
	}

	return &EntraIDHandler{
		config:     cfg,
		jwtManager: auth.NewJWTManager(cfg),
		validator:  validator,
	}
}

// SetValidator sets a custom Entra ID validator (used for testing)
func (h *EntraIDHandler) SetValidator(validator EntraIDValidator) {
	h.validator = validator
}

// RegisterEntraIDEndpoint registers the Entra ID authentication endpoint
func RegisterEntraIDEndpoint(api huma.API, pathPrefix string, cfg *config.Config) {
	if !cfg.EntraIDEnabled {
		return // Skip registration if Entra ID is not enabled
	}

	handler := NewEntraIDHandler(cfg)

	huma.Register(api, huma.Operation{
		OperationID: "entra-id-auth" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPost,
		Path:        pathPrefix + "/auth/entra-id",
		Summary:     "Exchange Entra ID token for registry token",
		Description: "Authenticate using Azure Entra ID (Azure AD) access token or ID token and receive a registry JWT token",
		Tags:        []string{"auth"},
	}, handler.handleTokenExchange)
}

// handleTokenExchange handles the token exchange logic
func (h *EntraIDHandler) handleTokenExchange(ctx context.Context, input *EntraIDTokenExchangeInput) (*v0.Response[auth.TokenResponse], error) {
	// Validate the Entra ID token
	claims, err := h.validator.ValidateT$tokenoken(ctx, input.Body.AccessToken)
	if err != nil {
		return nil, huma.Error401Unauthorized("Invalid Entra ID token", err)
	}

	// Determine the identity and namespace
	identity := h.determineIdentity(claims)
	namespace := h.determineNamespace(claims)

	// Generate permissions based on configuration
	permissions := h.generatePermissions(claims, namespace)

	// Generate registry JWT token
	jwtClaims := auth.JWTClaims{
		AuthMethod:        auth.MethodEntraID,
		AuthMethodSubject: identity,
		Permissions:       permissions,
	}

	tokenResponse, err := h.jwtManager.GenerateTokenResponse(ctx, jwtClaims)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to generate registry token", err)
	}

	return &v0.Response[auth.TokenResponse]{
		Body: *tokenResponse,
	}, nil
}

// determineIdentity extracts a stable identity from Entra ID claims
func (h *EntraIDHandler) determineIdentity(claims *EntraIDClaims) string {
	// For service principals (app-only tokens), use the app ID
	if claims.IDType == "app" && claims.AppID != "" {
		return fmt.Sprintf("app:%s", claims.AppID)
	}

	// For user tokens, prefer OID (most stable), then preferred_username, then subject
	if claims.OID != "" {
		return fmt.Sprintf("user:%s", claims.OID)
	}
	if claims.PreferredUsername != "" {
		return fmt.Sprintf("user:%s", claims.PreferredUsername)
	}
	return fmt.Sprintf("user:%s", claims.Subject)
}

// determineNamespace determines the namespace pattern based on configuration
func (h *EntraIDHandler) determineNamespace(claims *EntraIDClaims) string {
	// If a namespace pattern is configured, use it
	if h.config.EntraIDNamespacePattern != "" {
		// Check for wildcard - grants access to everything
		if h.config.EntraIDNamespacePattern == "*" {
			return "*"
		}
		
		// Replace placeholders with actual values
		namespace := h.config.EntraIDNamespacePattern
		namespace = strings.ReplaceAll(namespace, "{tenant_id}", claims.TenantID)
		namespace = strings.ReplaceAll(namespace, "{app_id}", claims.AppID)
		
		// Extract domain from preferred_username (e.g., user@contoso.com -> contoso.com)
		if claims.PreferredUsername != "" {
			parts := strings.Split(claims.PreferredUsername, "@")
			if len(parts) == 2 {
				domain := parts[1]
				namespace = strings.ReplaceAll(namespace, "{domain}", domain)
				// Also support com.contoso.* format
				reversedDomain := reverseHostname(domain)
				namespace = strings.ReplaceAll(namespace, "{reversed_domain}", reversedDomain)
			}
		}
		
		return namespace
	}

	// Default: use tenant ID in a namespace pattern
	return fmt.Sprintf("com.microsoft.entra.%s.*", claims.TenantID)
}

// reverseHostname converts "contoso.com" to "com.contoso"
func reverseHostname(hostname string) string {
	parts := strings.Split(hostname, ".")
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, ".")
}

// generatePermissions generates permissions based on claims and configuration
func (h *EntraIDHandler) generatePermissions(claims *EntraIDClaims, namespace string) []auth.Permission {
	// If wildcard is granted, return single wildcard permission
	if namespace == "*" {
		permissions := []auth.Permission{
			{
				Action:          auth.PermissionActionPublish,
				ResourcePattern: "*",
			},
		}
		if h.config.EntraIDAllowEdit {
			permissions = append(permissions, auth.Permission{
				Action:          auth.PermissionActionEdit,
				ResourcePattern: "*",
			})
		}
		return permissions
	}

	// Standard namespace-based permissions
	permissions := []auth.Permission{
		{
			Action:          auth.PermissionActionPublish,
			ResourcePattern: namespace,
		},
	}

	// Also add simple namespace format for compatibility with server names like "microsoft/server"
	// Extract the simple namespace from the full pattern
	simpleNamespace := h.extractSimpleNamespace(namespace, claims)
	if simpleNamespace != "" && simpleNamespace != namespace {
		permissions = append(permissions, auth.Permission{
			Action:          auth.PermissionActionPublish,
			ResourcePattern: simpleNamespace,
		})
	}

	// If the configuration allows edit permissions, add them
	if h.config.EntraIDAllowEdit {
		permissions = append(permissions, auth.Permission{
			Action:          auth.PermissionActionEdit,
			ResourcePattern: namespace,
		})
		if simpleNamespace != "" && simpleNamespace != namespace {
			permissions = append(permissions, auth.Permission{
				Action:          auth.PermissionActionEdit,
				ResourcePattern: simpleNamespace,
			})
		}
	}

	return permissions
}

// extractSimpleNamespace extracts a simple namespace from the full reverse-DNS pattern
// For example: "com.microsoft.*" -> "microsoft/*"
//              "io.github.username.*" -> "username/*" or "io.github.username/*"
func (h *EntraIDHandler) extractSimpleNamespace(namespace string, claims *EntraIDClaims) string {
	// If there's an explicit simple namespace pattern configured, use it
	if h.config.EntraIDSimpleNamespace != "" {
		simple := h.config.EntraIDSimpleNamespace
		simple = strings.ReplaceAll(simple, "{tenant_id}", claims.TenantID)
		simple = strings.ReplaceAll(simple, "{app_id}", claims.AppID)
		
		if claims.PreferredUsername != "" {
			parts := strings.Split(claims.PreferredUsername, "@")
			if len(parts) == 2 {
				domain := parts[1]
				// Extract company name from domain (e.g., contoso.com -> contoso)
				company := strings.Split(domain, ".")[0]
				simple = strings.ReplaceAll(simple, "{company}", company)
				simple = strings.ReplaceAll(simple, "{domain}", domain)
			}
		}
		
		// Extract app display name parts for service principals
		if claims.AppDisplayName != "" {
			// Clean up app display name (e.g., "Azure DevOps" -> "azure-devops")
			appName := strings.ToLower(claims.AppDisplayName)
			appName = strings.ReplaceAll(appName, " ", "-")
			simple = strings.ReplaceAll(simple, "{app_name}", appName)
		}
		
		return simple
	}

	// Auto-extract from reverse-DNS pattern
	// "com.microsoft.*" -> "microsoft/*"
	// "io.github.username.*" -> "username/*"
	if strings.HasPrefix(namespace, "com.") && strings.HasSuffix(namespace, ".*") {
		// com.contoso.* -> contoso/*
		parts := strings.Split(namespace, ".")
		if len(parts) >= 3 {
			return parts[1] + "/*"
		}
	}
	
	if strings.HasPrefix(namespace, "io.github.") && strings.HasSuffix(namespace, ".*") {
		// io.github.username.* -> username/* OR io.github.username/*
		parts := strings.Split(namespace, ".")
		if len(parts) >= 4 {
			// Allow both formats for GitHub
			return "io.github." + parts[2] + "/*"
		}
	}

	// If we can't extract a simple namespace, return empty
	// This means only the full pattern will be used
	return ""
}
