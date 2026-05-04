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

const (
	testJWTPrivateKey = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef" // 32-byte hex test key
	testOIDCIssuer    = "https://accounts.google.com"
	testOIDCClientID  = "test-client-id"
)

// MockGenericOIDCValidator for testing
type MockGenericOIDCValidator struct {
	validateFunc func(ctx context.Context, token string) (*auth.OIDCClaims, error)
}

func (m *MockGenericOIDCValidator) ValidateToken(ctx context.Context, token string) (*auth.OIDCClaims, error) {
	if m.validateFunc != nil {
		return m.validateFunc(ctx, token)
	}
	return nil, fmt.Errorf("not implemented")
}

func testOIDCConfig(extraClaims string) *config.Config {
	return &config.Config{
		OIDCEnabled:      true,
		OIDCIssuer:       testOIDCIssuer,
		OIDCClientID:     testOIDCClientID,
		OIDCExtraClaims:  extraClaims,
		OIDCPublishPerms: "*",
		JWTPrivateKey:    testJWTPrivateKey,
	}
}

func mockValidator(claims map[string]any) *MockGenericOIDCValidator {
	return &MockGenericOIDCValidator{
		validateFunc: func(_ context.Context, _ string) (*auth.OIDCClaims, error) {
			return &auth.OIDCClaims{ExtraClaims: claims}, nil
		},
	}
}

func TestOIDCHandler_ExchangeToken(t *testing.T) {
	tests := []struct {
		name          string
		config        *config.Config
		mockValidator *MockGenericOIDCValidator
		token         string
		expectedError bool
	}{
		{ //nolint:gosec // G101: test data, not real credentials
			name:   "successful token exchange with publish permissions",
			config: testOIDCConfig(`[{"hd":"modelcontextprotocol.io"}]`),
			mockValidator: mockValidator(map[string]any{
				"email":              "admin@modelcontextprotocol.io",
				"email_verified":     true,
				"hd":                 "modelcontextprotocol.io",
				"preferred_username": "admin",
			}),
			token:         "valid-oidc-token",
			expectedError: false,
		},
		{
			name:   "failed validation with invalid hosted domain",
			config: testOIDCConfig(`[{"hd":"modelcontextprotocol.io"}]`),
			mockValidator: mockValidator(map[string]any{
				"email":              "user@example.com",
				"email_verified":     true,
				"hd":                 "example.com",
				"preferred_username": "user",
			}),
			token:         "invalid-domain-token",
			expectedError: true,
		},
		{
			name:   "scalar expected matches array claim",
			config: testOIDCConfig(`[{"groups":"admin"}]`),
			mockValidator: mockValidator(map[string]any{
				"groups": []any{"admin", "users", "developers"},
			}),
			token:         "scalar-expected-array-actual-match",
			expectedError: false,
		},
		{
			name:   "scalar expected not present in array claim",
			config: testOIDCConfig(`[{"groups":"super-admin"}]`),
			mockValidator: mockValidator(map[string]any{
				"groups": []any{"admin", "users", "developers"},
			}),
			token:         "scalar-expected-array-actual-no-match",
			expectedError: true,
		},
		{
			name:   "array expected overlaps array claim",
			config: testOIDCConfig(`[{"roles":["admin","moderator"]}]`),
			mockValidator: mockValidator(map[string]any{
				"roles": []any{"admin", "users"},
			}),
			token:         "array-array-overlap",
			expectedError: false,
		},
		{ //nolint:gosec // G101: test data, not real credentials
			name:   "array expected disjoint from array claim",
			config: testOIDCConfig(`[{"roles":["super-admin","owner"]}]`),
			mockValidator: mockValidator(map[string]any{
				"roles": []any{"admin", "users"},
			}),
			token:         "array-array-no-overlap",
			expectedError: true,
		},
		{ //nolint:gosec // G101: test data, not real credentials
			name:   "array expected matches scalar claim",
			config: testOIDCConfig(`[{"role":["admin","moderator"]}]`),
			mockValidator: mockValidator(map[string]any{
				"role": "admin",
			}),
			token:         "scalar-actual-array-expected-match",
			expectedError: false,
		},
		{ //nolint:gosec // G101: test data, not real credentials
			name:   "[]string typed claim matches scalar expected",
			config: testOIDCConfig(`[{"groups":"admin"}]`),
			mockValidator: mockValidator(map[string]any{
				"groups": []string{"admin", "users"},
			}),
			token:         "string-slice-claim",
			expectedError: false,
		},
		{
			name:   "required claim missing",
			config: testOIDCConfig(`[{"required_claim":"expected_value"}]`),
			mockValidator: mockValidator(map[string]any{
				"other_claim": "some_value",
			}),
			token:         "missing-claim",
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
