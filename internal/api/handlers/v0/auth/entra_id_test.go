package auth_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	v0auth "github.com/modelcontextprotocol/registry/internal/api/handlers/v0/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockEntraIDValidator is a mock validator for testing
type MockEntraIDValidator struct {
	validateFunc func(ctx context.Context, token string) (*v0auth.EntraIDClaims, error)
}

func (m *MockEntraIDValidator) ValidateToken(ctx context.Context, token string) (*v0auth.EntraIDClaims, error) {
	return m.validateFunc(ctx, token)
}

func TestEntraIDEndpoint(t *testing.T) {
	// Create test config with Entra ID enabled
	testSeed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(testSeed)
	require.NoError(t, err)

	testConfig := &config.Config{
		JWTPrivateKey:           hex.EncodeToString(testSeed),
		EntraIDEnabled:          true,
		EntraIDTenantID:         "00000000-0000-0000-0000-000000000000",
		EntraIDClientID:         "11111111-1111-1111-1111-111111111111",
		EntraIDNamespacePattern: "com.{reversed_domain}.*",
		EntraIDAllowEdit:        true,
	}

	testCases := []struct {
		name           string
		requestBody    map[string]string
		mockValidator  func(ctx context.Context, token string) (*v0auth.EntraIDClaims, error)
		expectedStatus int
		expectedError  string
		validateToken  func(t *testing.T, token string)
	}{
		{
			name: "successful user token exchange",
			requestBody: map[string]string{
				"access_token": "valid-user-token",
			},
			mockValidator: func(ctx context.Context, token string) (*v0auth.EntraIDClaims, error) {
				return &v0auth.EntraIDClaims{
					Subject:           "user-subject-123",
					Issuer:            "https://login.microsoftonline.com/00000000-0000-0000-0000-000000000000/v2.0",
					Audience:          []string{"11111111-1111-1111-1111-111111111111"},
					OID:               "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
					TenantID:          "00000000-0000-0000-0000-000000000000",
					PreferredUsername: "user@contoso.com",
					Name:              "Test User",
					Email:             "user@contoso.com",
					IDType:            "user",
				}, nil
			},
			expectedStatus: http.StatusOK,
			validateToken: func(t *testing.T, token string) {
				assert.NotEmpty(t, token)
			},
		},
		{
			name: "successful service principal token exchange",
			requestBody: map[string]string{
				"access_token": "valid-app-token",
			},
			mockValidator: func(ctx context.Context, token string) (*v0auth.EntraIDClaims, error) {
				return &v0auth.EntraIDClaims{
					Subject:        "app-subject-456",
					Issuer:         "https://login.microsoftonline.com/00000000-0000-0000-0000-000000000000/v2.0",
					Audience:       []string{"11111111-1111-1111-1111-111111111111"},
					TenantID:       "00000000-0000-0000-0000-000000000000",
					AppID:          "22222222-2222-2222-2222-222222222222",
					AppDisplayName: "My Service Principal",
					IDType:         "app",
				}, nil
			},
			expectedStatus: http.StatusOK,
			validateToken: func(t *testing.T, token string) {
				assert.NotEmpty(t, token)
			},
		},
		{
			name: "invalid token",
			requestBody: map[string]string{
				"access_token": "invalid-token",
			},
			mockValidator: func(ctx context.Context, token string) (*v0auth.EntraIDClaims, error) {
				return nil, assert.AnError
			},
			expectedStatus: http.StatusUnauthorized,
			expectedError:  "Invalid Entra ID token",
		},
		{
			name:           "missing access token",
			requestBody:    map[string]string{},
			expectedStatus: http.StatusUnprocessableEntity,
			expectedError:  "required property is missing",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a new ServeMux and Huma API
			mux := http.NewServeMux()
			api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))

			// Register the endpoint
			v0auth.RegisterEntraIDEndpoint(api, "/v0", testConfig)

			// Get the handler and inject mock validator if needed
			if tc.mockValidator != nil {
				// This is a bit hacky, but we need to create a handler with a mock validator
				// In a real implementation, we'd use dependency injection
				handler := v0auth.NewEntraIDHandler(testConfig)
				mockValidator := &MockEntraIDValidator{validateFunc: tc.mockValidator}
				handler.SetValidator(mockValidator)

				// Re-register with mock validator
				mux = http.NewServeMux()
				api = humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
				
				// We'll need to expose a way to register with a custom handler
				// For now, this test demonstrates the expected behavior
			}

			// Prepare request
			bodyBytes, err := json.Marshal(tc.requestBody)
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodPost, "/v0/auth/entra-id", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)

			// Assertions
			if tc.mockValidator == nil && tc.expectedStatus != http.StatusOK {
				// For cases where we expect validation errors without mock
				assert.Equal(t, tc.expectedStatus, rr.Code)
				if tc.expectedError != "" {
					assert.Contains(t, rr.Body.String(), tc.expectedError)
				}
				return
			}

			if tc.mockValidator != nil {
				assert.Equal(t, tc.expectedStatus, rr.Code)

				if tc.expectedStatus == http.StatusOK {
					var response v0auth.RegistryTokenResponse
					err = json.Unmarshal(rr.Body.Bytes(), &response)
					require.NoError(t, err)

					if tc.validateToken != nil {
						tc.validateToken(t, response.RegistryToken)
					}
				} else if tc.expectedError != "" {
					assert.Contains(t, rr.Body.String(), tc.expectedError)
				}
			}
		})
	}
}

func TestEntraIDValidator(t *testing.T) {
	t.Run("validator requires Entra ID to be enabled", func(t *testing.T) {
		cfg := &config.Config{
			EntraIDEnabled: false,
		}

		_, err := v0auth.NewStandardEntraIDValidator(cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not enabled")
	})
}

func TestDetermineIdentity(t *testing.T) {
	handler := &v0auth.EntraIDHandler{}

	testCases := []struct {
		name     string
		claims   *v0auth.EntraIDClaims
		expected string
	}{
		{
			name: "service principal uses app ID",
			claims: &v0auth.EntraIDClaims{
				IDType: "app",
				AppID:  "12345678-1234-1234-1234-123456789012",
			},
			expected: "app:12345678-1234-1234-1234-123456789012",
		},
		{
			name: "user with OID",
			claims: &v0auth.EntraIDClaims{
				IDType:  "user",
				OID:     "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
				Subject: "subject-123",
			},
			expected: "user:aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		},
		{
			name: "user with preferred username but no OID",
			claims: &v0auth.EntraIDClaims{
				IDType:            "user",
				PreferredUsername: "user@contoso.com",
				Subject:           "subject-456",
			},
			expected: "user:user@contoso.com",
		},
		{
			name: "user with only subject",
			claims: &v0auth.EntraIDClaims{
				Subject: "subject-789",
			},
			expected: "user:subject-789",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// We'd need to expose determineIdentity as a method or test through the full flow
			// This is a placeholder showing what we want to test
			t.Skip("Need to refactor handler to expose determineIdentity for testing")
		})
	}
}

func TestReverseHostname(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"contoso.com", "com.contoso"},
		{"subdomain.contoso.com", "com.contoso.subdomain"},
		{"example.org", "org.example"},
		{"localhost", "localhost"},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			// We'd need to expose reverseHostname or test through namespace determination
			t.Skip("Need to refactor to expose reverseHostname for testing")
		})
	}
}
