package auth_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/modelcontextprotocol/registry/internal/api/handlers/v0/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockGenericOIDCValidator for testing
type MockGenericOIDCValidator struct {
	validateFunc     func(ctx context.Context, token string) (*auth.OIDCClaims, error)
	authURLFunc      func(state, nonce, redirectURI string) string
	exchangeCodeFunc func(ctx context.Context, code, redirectURI string) (string, error)
}

func (m *MockGenericOIDCValidator) ValidateToken(ctx context.Context, token string) (*auth.OIDCClaims, error) {
	if m.validateFunc != nil {
		return m.validateFunc(ctx, token)
	}
	return nil, fmt.Errorf("not implemented")
}

func (m *MockGenericOIDCValidator) GetAuthorizationURL(state, nonce, redirectURI string) string {
	if m.authURLFunc != nil {
		return m.authURLFunc(state, nonce, redirectURI)
	}
	return ""
}

func (m *MockGenericOIDCValidator) ExchangeCodeForToken(ctx context.Context, code, redirectURI string) (string, error) {
	if m.exchangeCodeFunc != nil {
		return m.exchangeCodeFunc(ctx, code, redirectURI)
	}
	return "", fmt.Errorf("not implemented")
}

func TestOIDCHandler_ExchangeToken(t *testing.T) {
	tests := []struct {
		name          string
		config        *config.Config
		mockValidator *MockGenericOIDCValidator
		token         string
		expectedError bool
	}{
		{
			name: "successful token exchange with publish permissions",
			config: &config.Config{
				OIDCEnabled:      true,
				OIDCIssuer:       "https://accounts.google.com",
				OIDCClientID:     "test-client-id",
				OIDCExtraClaims:  `[{"hd":"modelcontextprotocol.io"}]`,
				OIDCPublishPerms: "*",
				JWTPrivateKey:    "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef", // 32 byte hex
			},
			mockValidator: &MockGenericOIDCValidator{
				validateFunc: func(_ context.Context, _ string) (*auth.OIDCClaims, error) {
					return &auth.OIDCClaims{
						ExtraClaims: map[string]any{
							"email":              "admin@modelcontextprotocol.io",
							"email_verified":     true,
							"hd":                 "modelcontextprotocol.io",
							"preferred_username": "admin",
						},
					}, nil
				},
			},
			token:         "valid-oidc-token",
			expectedError: false,
		},
		{
			name: "failed validation with invalid hosted domain",
			config: &config.Config{
				OIDCEnabled:      true,
				OIDCIssuer:       "https://accounts.google.com",
				OIDCClientID:     "test-client-id",
				OIDCExtraClaims:  `[{"hd":"modelcontextprotocol.io"}]`,
				OIDCPublishPerms: "*",
				JWTPrivateKey:    "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
			},
			mockValidator: &MockGenericOIDCValidator{
				validateFunc: func(_ context.Context, _ string) (*auth.OIDCClaims, error) {
					return &auth.OIDCClaims{
						ExtraClaims: map[string]any{
							"email":              "user@example.com",
							"email_verified":     true,
							"hd":                 "example.com", // Wrong domain
							"preferred_username": "user",
						},
					}, nil
				},
			},
			token:         "invalid-domain-token",
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := auth.NewOIDCHandler(tt.config)
			if tt.mockValidator != nil {
				handler.SetValidator(tt.mockValidator)
			}

			ctx := context.Background()
			response, err := handler.ExchangeToken(ctx, tt.token)

			if tt.expectedError {
				assert.Error(t, err)
				assert.Nil(t, response)
			} else {
				require.NoError(t, err)
				require.NotNil(t, response)
				assert.NotEmpty(t, response.RegistryToken)
				assert.Greater(t, response.ExpiresAt, 0)
			}
		})
	}
}

func TestOIDCHandler_StartAuth(t *testing.T) {
	config := &config.Config{
		OIDCEnabled:      true,
		OIDCIssuer:       "https://accounts.google.com",
		OIDCClientID:     "test-client-id",
		OIDCClientSecret: "test-secret",
		JWTPrivateKey:    "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
	}

	mockValidator := &MockGenericOIDCValidator{
		authURLFunc: func(state, nonce, redirectURI string) string {
			return fmt.Sprintf("https://accounts.google.com/oauth/authorize?client_id=%s&state=%s&nonce=%s&redirect_uri=%s",
				config.OIDCClientID, state, nonce, redirectURI)
		},
	}

	handler := auth.NewOIDCHandler(config)
	handler.SetValidator(mockValidator)

	ctx := context.Background()
	authURL, err := handler.StartAuth(ctx, "http://localhost:3000/callback")

	require.NoError(t, err)
	assert.Contains(t, authURL, "https://accounts.google.com/oauth/authorize")
	assert.Contains(t, authURL, "client_id=test-client-id")
	assert.Contains(t, authURL, "state=")
	assert.Contains(t, authURL, "nonce=")
}

// Note: validateExtraClaims and buildPermissions are tested through ExchangeToken integration tests
