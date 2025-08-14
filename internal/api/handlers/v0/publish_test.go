package v0_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockRegistryService is a mock implementation of the RegistryService interface
type MockRegistryService struct {
	mock.Mock
}

func (m *MockRegistryService) List(cursor string, limit int) ([]model.Server, string, error) {
	args := m.Called(cursor, limit)
	return args.Get(0).([]model.Server), args.String(1), args.Error(2)
}

func (m *MockRegistryService) GetByID(id string) (*model.ServerDetail, error) {
	args := m.Called(id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.ServerDetail), args.Error(1)
}

func (m *MockRegistryService) Publish(serverDetail *model.ServerDetail) error {
	args := m.Called(serverDetail)
	return args.Error(0)
}

// Helper function to generate a valid JWT token for testing
func generateTestJWTToken(cfg *config.Config, claims auth.JWTClaims) (string, error) {
	jwtManager := auth.NewJWTManager(cfg)
	ctx := context.Background()
	tokenResponse, err := jwtManager.GenerateTokenResponse(ctx, claims)
	if err != nil {
		return "", err
	}
	return tokenResponse.RegistryToken, nil
}

func TestPublishEndpoint(t *testing.T) {
	testSeed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(testSeed)
	require.NoError(t, err)
	testConfig := &config.Config{
		JWTPrivateKey: hex.EncodeToString(testSeed),
	}

	testCases := []struct {
		name           string
		requestBody    interface{}
		tokenClaims    *auth.JWTClaims
		authHeader     string
		setupMocks     func(*MockRegistryService)
		expectedStatus int
		expectedError  string
	}{
		{
			name: "successful publish with GitHub auth",
			requestBody: model.PublishRequest{
				ServerDetail: model.ServerDetail{
					Server: model.Server{
						ID:          "test-id",
						Name:        "io.github.example/test-server",
						Description: "A test server",
						Repository: model.Repository{
							URL:    "https://github.com/example/test-server",
							Source: "github",
							ID:     "example/test-server",
						},
						VersionDetail: model.VersionDetail{
							Version:     "1.0.0",
							ReleaseDate: "2025-05-25T00:00:00Z",
							IsLatest:    true,
						},
					},
				},
			},
			tokenClaims: &auth.JWTClaims{
				AuthMethod:        model.AuthMethodGitHub,
				AuthMethodSubject: "example",
				Permissions: []auth.Permission{
					{Action: auth.PermissionActionPublish, ResourcePattern: "io.github.example/*"},
				},
			},
			setupMocks: func(registry *MockRegistryService) {
				registry.On("Publish", mock.AnythingOfType("*model.ServerDetail")).Return(nil)
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "successful publish with no auth (AuthMethodNone)",
			requestBody: model.PublishRequest{
				ServerDetail: model.ServerDetail{
					Server: model.Server{
						ID:          "test-id-2",
						Name:        "example/test-server",
						Description: "A test server without auth",
						Repository: model.Repository{
							URL:    "https://example.com/test-server",
							Source: "example",
							ID:     "example/test-server",
						},
						VersionDetail: model.VersionDetail{
							Version:     "1.0.0",
							ReleaseDate: "2025-05-25T00:00:00Z",
							IsLatest:    true,
						},
					},
				},
			},
			tokenClaims: &auth.JWTClaims{
				AuthMethod: model.AuthMethodNone,
				Permissions: []auth.Permission{
					{Action: auth.PermissionActionPublish, ResourcePattern: "example/*"},
				},
			},
			setupMocks: func(registry *MockRegistryService) {
				registry.On("Publish", mock.AnythingOfType("*model.ServerDetail")).Return(nil)
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing authorization header",
			requestBody:    model.PublishRequest{},
			authHeader:     "", // Empty auth header
			setupMocks:     func(_ *MockRegistryService) {},
			expectedStatus: http.StatusUnprocessableEntity,
			expectedError:  "required header parameter is missing",
		},
		{
			name:           "invalid authorization header format",
			requestBody:    model.PublishRequest{},
			authHeader:     "InvalidFormat",
			setupMocks:     func(_ *MockRegistryService) {},
			expectedStatus: http.StatusUnauthorized,
			expectedError:  "Invalid Authorization header format",
		},
		{
			name: "invalid token",
			requestBody: model.PublishRequest{
				ServerDetail: model.ServerDetail{
					Server: model.Server{
						Name:        "test-server",
						Description: "A test server",
						VersionDetail: model.VersionDetail{
							Version: "1.0.0",
						},
					},
				},
			},
			authHeader:     "Bearer invalidToken",
			setupMocks:     func(_ *MockRegistryService) {},
			expectedStatus: http.StatusUnauthorized,
			expectedError:  "Invalid or expired Registry JWT token",
		},
		{
			name: "permission denied",
			requestBody: model.PublishRequest{
				ServerDetail: model.ServerDetail{
					Server: model.Server{
						Name:        "io.github.other/test-server",
						Description: "A test server",
						VersionDetail: model.VersionDetail{
							Version: "1.0.0",
						},
					},
				},
			},
			tokenClaims: &auth.JWTClaims{
				AuthMethod: model.AuthMethodGitHub,
				Permissions: []auth.Permission{
					{Action: auth.PermissionActionPublish, ResourcePattern: "io.github.example/*"},
				},
			},
			setupMocks:     func(_ *MockRegistryService) {},
			expectedStatus: http.StatusForbidden,
			expectedError:  "You do not have permission to publish this server",
		},
		{
			name: "registry service error",
			requestBody: model.PublishRequest{
				ServerDetail: model.ServerDetail{
					Server: model.Server{
						Name:        "example/test-server",
						Description: "A test server",
						VersionDetail: model.VersionDetail{
							Version: "1.0.0",
						},
					},
				},
			},
			tokenClaims: &auth.JWTClaims{
				AuthMethod: model.AuthMethodNone,
				Permissions: []auth.Permission{
					{Action: auth.PermissionActionPublish, ResourcePattern: "*"},
				},
			},
			setupMocks: func(registry *MockRegistryService) {
				registry.On("Publish", mock.AnythingOfType("*model.ServerDetail")).Return(errors.New("database error"))
			},
			expectedStatus: http.StatusInternalServerError,
			expectedError:  "Failed to publish server",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create mocks
			mockRegistry := new(MockRegistryService)

			// Setup mocks
			tc.setupMocks(mockRegistry)

			// Create a new ServeMux and Huma API
			mux := http.NewServeMux()
			api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))

			// Register the endpoint with test config
			v0.RegisterPublishEndpoint(api, mockRegistry, testConfig)

			// Prepare request body
			var requestBody []byte
			if tc.requestBody != nil {
				var err error
				requestBody, err = json.Marshal(tc.requestBody)
				assert.NoError(t, err)
			}

			// Create request
			req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/v0/publish", bytes.NewBuffer(requestBody))
			assert.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")

			// Set auth header
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			} else if tc.tokenClaims != nil {
				// Generate a valid JWT token
				token, err := generateTestJWTToken(testConfig, *tc.tokenClaims)
				assert.NoError(t, err)
				req.Header.Set("Authorization", "Bearer "+token)
			}

			// Perform request
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)

			// Assertions
			assert.Equal(t, tc.expectedStatus, rr.Code, "status code mismatch")

			if tc.expectedError != "" {
				assert.Contains(t, rr.Body.String(), tc.expectedError)
			}

			// Verify mock expectations
			mockRegistry.AssertExpectations(t)
		})
	}
}
