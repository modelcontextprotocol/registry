package v0_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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

// MockAuthService is a mock implementation of the auth.Service interface
type MockAuthService struct {
	mock.Mock
}

func (m *MockAuthService) StartAuthFlow(ctx context.Context, method model.AuthMethod, repoRef string) (map[string]string, string, error) {
	args := m.Called(ctx, method, repoRef)
	return args.Get(0).(map[string]string), args.String(1), args.Error(2)
}

func (m *MockAuthService) CheckAuthStatus(ctx context.Context, statusToken string) (string, error) {
	args := m.Called(ctx, statusToken)
	return args.String(0), args.Error(1)
}

func (m *MockAuthService) ValidateAuth(ctx context.Context, authentication model.Authentication) (bool, error) {
	args := m.Called(ctx, authentication)
	return args.Bool(0), args.Error(1)
}

func TestPublishEndpoint(t *testing.T) {
	testCases := []struct {
		name             string
		requestBody      interface{}
		authHeader       string
		setupMocks       func(*MockRegistryService, *MockAuthService)
		expectedStatus   int
		expectedResponse string
		expectedError    string
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
			authHeader: "Bearer github_token_123",
			setupMocks: func(registry *MockRegistryService, authSvc *MockAuthService) {
				authSvc.On("ValidateAuth", mock.Anything, model.Authentication{
					Method:  model.AuthMethodGitHub,
					Token:   "github_token_123",
					RepoRef: "io.github.example/test-server",
				}).Return(true, nil)
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
			authHeader: "Bearer some_token",
			setupMocks: func(registry *MockRegistryService, authSvc *MockAuthService) {
				authSvc.On("ValidateAuth", mock.Anything, model.Authentication{
					Method:  model.AuthMethodNone,
					Token:   "some_token",
					RepoRef: "example/test-server",
				}).Return(true, nil)
				registry.On("Publish", mock.AnythingOfType("*model.ServerDetail")).Return(nil)
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing authorization header",
			requestBody:    model.PublishRequest{},
			authHeader:     "",
			setupMocks:     func(_ *MockRegistryService, _ *MockAuthService) {},
			expectedStatus: http.StatusUnprocessableEntity,
			expectedError:  "required header parameter is missing",
		},
		{
			name:           "invalid authorization header format",
			requestBody:    model.PublishRequest{},
			authHeader:     "InvalidFormat",
			setupMocks:     func(_ *MockRegistryService, _ *MockAuthService) {},
			expectedStatus: http.StatusUnauthorized,
			expectedError:  "Invalid Authorization header format",
		},
		{
			name: "missing server name",
			requestBody: model.PublishRequest{
				ServerDetail: model.ServerDetail{
					Server: model.Server{
						ID:          "test-id",
						Name:        "", // Missing name
						Description: "A test server",
						VersionDetail: model.VersionDetail{
							Version:     "1.0.0",
							ReleaseDate: "2025-05-25T00:00:00Z",
							IsLatest:    true,
						},
					},
				},
			},
			authHeader:     "Bearer token",
			setupMocks:     func(_ *MockRegistryService, _ *MockAuthService) {},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Name is required",
		},
		{
			name: "missing version",
			requestBody: model.PublishRequest{
				ServerDetail: model.ServerDetail{
					Server: model.Server{
						ID:          "test-id",
						Name:        "test-server",
						Description: "A test server",
						VersionDetail: model.VersionDetail{
							Version:     "", // Missing version
							ReleaseDate: "2025-05-25T00:00:00Z",
							IsLatest:    true,
						},
					},
				},
			},
			authHeader:     "Bearer token",
			setupMocks:     func(_ *MockRegistryService, _ *MockAuthService) {},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Version is required",
		},
		{
			name: "authentication required error",
			requestBody: model.PublishRequest{
				ServerDetail: model.ServerDetail{
					Server: model.Server{
						ID:          "test-id",
						Name:        "test-server",
						Description: "A test server",
						VersionDetail: model.VersionDetail{
							Version:     "1.0.0",
							ReleaseDate: "2025-05-25T00:00:00Z",
							IsLatest:    true,
						},
					},
				},
			},
			authHeader: "Bearer token",
			setupMocks: func(_ *MockRegistryService, authSvc *MockAuthService) {
				authSvc.On("ValidateAuth", mock.Anything, mock.Anything).Return(false, auth.ErrAuthRequired)
			},
			expectedStatus: http.StatusUnauthorized,
			expectedError:  "Authentication failed",
		},
		{
			name: "authentication failed",
			requestBody: model.PublishRequest{
				ServerDetail: model.ServerDetail{
					Server: model.Server{
						ID:          "test-id",
						Name:        "test-server",
						Description: "A test server",
						VersionDetail: model.VersionDetail{
							Version:     "1.0.0",
							ReleaseDate: "2025-05-25T00:00:00Z",
							IsLatest:    true,
						},
					},
				},
			},
			authHeader: "Bearer invalid_token",
			setupMocks: func(_ *MockRegistryService, authSvc *MockAuthService) {
				authSvc.On("ValidateAuth", mock.Anything, mock.Anything).Return(false, nil)
			},
			expectedStatus: http.StatusUnauthorized,
			expectedError:  "Invalid authentication credentials",
		},
		{
			name: "registry service error",
			requestBody: model.PublishRequest{
				ServerDetail: model.ServerDetail{
					Server: model.Server{
						ID:          "test-id",
						Name:        "test-server",
						Description: "A test server",
						VersionDetail: model.VersionDetail{
							Version:     "1.0.0",
							ReleaseDate: "2025-05-25T00:00:00Z",
							IsLatest:    true,
						},
					},
				},
			},
			authHeader: "Bearer token",
			setupMocks: func(registry *MockRegistryService, authSvc *MockAuthService) {
				authSvc.On("ValidateAuth", mock.Anything, mock.Anything).Return(true, nil)
				registry.On("Publish", mock.AnythingOfType("*model.ServerDetail")).Return(assert.AnError)
			},
			expectedStatus: http.StatusInternalServerError,
			expectedError:  "Failed to publish server",
		},
		{
			name: "method not allowed",
			requestBody: nil,
			authHeader: "",
			setupMocks: func(_ *MockRegistryService, _ *MockAuthService) {},
			expectedStatus: http.StatusMethodNotAllowed,
			expectedError: "Method Not Allowed",
		},
		{
			name: "HTML injection attack in name field",
			requestBody: model.PublishRequest{
				ServerDetail: model.ServerDetail{
					Server: model.Server{
						ID:          "test-id-html",
						Name:        "io.github.malicious/<script>alert('XSS')</script>test-server",
						Description: "A test server with HTML injection attempt",
						Repository: model.Repository{
							URL:    "https://github.com/malicious/test-server",
							Source: "github",
							ID:     "malicious/test-server",
						},
						VersionDetail: model.VersionDetail{
							Version:     "1.0.0",
							ReleaseDate: "2025-05-25T00:00:00Z",
							IsLatest:    true,
						},
					},
				},
			},
			authHeader: "Bearer github_token_123",
			setupMocks: func(registry *MockRegistryService, authSvc *MockAuthService) {
				// The implementation should escape HTML
				authSvc.On("ValidateAuth", mock.Anything, mock.MatchedBy(func(auth model.Authentication) bool {
					return auth.Method == model.AuthMethodGitHub &&
						auth.Token == "github_token_123" &&
						auth.RepoRef == "io.github.malicious/<script>alert('XSS')</script>test-server"
				})).Return(true, nil)
				registry.On("Publish", mock.AnythingOfType("*model.ServerDetail")).Return(nil)
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "HTML injection attack in name field with non-GitHub prefix",
			requestBody: model.PublishRequest{
				ServerDetail: model.ServerDetail{
					Server: model.Server{
						ID:          "test-id-html-non-github",
						Name:        "malicious.com/<script>alert('XSS')</script>test-server",
						Description: "A test server with HTML injection attempt (non-GitHub)",
						Repository: model.Repository{
							URL:    "https://malicious.com/test-server",
							Source: "custom",
							ID:     "malicious/test-server",
						},
						VersionDetail: model.VersionDetail{
							Version:     "1.0.0",
							ReleaseDate: "2025-05-25T00:00:00Z",
							IsLatest:    true,
						},
					},
				},
			},
			authHeader: "Bearer some_token",
			setupMocks: func(registry *MockRegistryService, authSvc *MockAuthService) {
				authSvc.On("ValidateAuth", mock.Anything, mock.MatchedBy(func(auth model.Authentication) bool {
					return auth.Method == model.AuthMethodNone &&
						auth.Token == "some_token" &&
						auth.RepoRef == "malicious.com/<script>alert('XSS')</script>test-server"
				})).Return(true, nil)
				registry.On("Publish", mock.AnythingOfType("*model.ServerDetail")).Return(nil)
			},
			expectedStatus: http.StatusOK,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create mocks
			mockRegistry := new(MockRegistryService)
			mockAuthService := new(MockAuthService)

			// Setup mocks
			tc.setupMocks(mockRegistry, mockAuthService)

			// Create a new ServeMux and Huma API
			mux := http.NewServeMux()
			api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))

			// Register the endpoint
			v0.RegisterPublishEndpoint(api, mockRegistry, mockAuthService)

			// Prepare request body
			var requestBody []byte
			if tc.requestBody != nil {
				var err error
				requestBody, err = json.Marshal(tc.requestBody)
				assert.NoError(t, err)
			}

			// Create request
			method := http.MethodPost
			if tc.name == "method not allowed" {
				method = http.MethodGet
			}
			req, err := http.NewRequestWithContext(context.Background(), method, "/v0/publish", bytes.NewBuffer(requestBody))
			assert.NoError(t, err)
			if method == http.MethodPost {
				req.Header.Set("Content-Type", "application/json")
			}

			// Set auth header if provided
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}

			// Create response recorder
			rr := httptest.NewRecorder()

			// Call the handler through the mux
			mux.ServeHTTP(rr, req)

			// Check status code
			assert.Equal(t, tc.expectedStatus, rr.Code)

			if tc.expectedError != "" {
				// Check that the error message is contained in the response
				assert.Contains(t, rr.Body.String(), tc.expectedError)
			}

			// Assert that all expectations were met
			mockRegistry.AssertExpectations(t)
			mockAuthService.AssertExpectations(t)
		})
	}
}

func TestPublishEndpointBearerTokenParsing(t *testing.T) {
	testCases := []struct {
		name          string
		authHeader    string
		expectedToken string
	}{
		{
			name:          "bearer token with Bearer prefix",
			authHeader:    "Bearer github_token_123",
			expectedToken: "github_token_123",
		},
		{
			name:          "bearer token with bearer prefix (lowercase)",
			authHeader:    "bearer github_token_123",
			expectedToken: "github_token_123",
		},
		{
			name:          "mixed case Bearer prefix",
			authHeader:    "BeArEr github_token_123",
			expectedToken: "github_token_123",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockRegistry := new(MockRegistryService)
			mockAuthService := new(MockAuthService)

			// Setup mock to capture the actual token passed
			mockAuthService.On("ValidateAuth", mock.Anything, mock.MatchedBy(func(auth model.Authentication) bool {
				return auth.Token == tc.expectedToken
			})).Return(true, nil)
			mockRegistry.On("Publish", mock.AnythingOfType("*model.ServerDetail")).Return(nil)

			mux := http.NewServeMux()
			api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
			v0.RegisterPublishEndpoint(api, mockRegistry, mockAuthService)

			serverDetail := model.PublishRequest{
				ServerDetail: model.ServerDetail{
					Server: model.Server{
						ID:          "test-id",
						Name:        "test-server",
						Description: "A test server",
						VersionDetail: model.VersionDetail{
							Version:     "1.0.0",
							ReleaseDate: "2025-05-25T00:00:00Z",
							IsLatest:    true,
						},
					},
				},
			}

			requestBody, err := json.Marshal(serverDetail)
			assert.NoError(t, err)

			req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/v0/publish", bytes.NewBuffer(requestBody))
			assert.NoError(t, err)
			req.Header.Set("Authorization", tc.authHeader)
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusOK, rr.Code)
			mockAuthService.AssertExpectations(t)
		})
	}
}

func TestPublishEndpointAuthMethodSelection(t *testing.T) {
	testCases := []struct {
		name               string
		serverName         string
		expectedAuthMethod model.AuthMethod
	}{
		{
			name:               "GitHub prefix triggers GitHub auth",
			serverName:         "io.github.example/test-server",
			expectedAuthMethod: model.AuthMethodGitHub,
		},
		{
			name:               "non-GitHub prefix uses no auth",
			serverName:         "example.com/test-server",
			expectedAuthMethod: model.AuthMethodNone,
		},
		{
			name:               "empty prefix uses no auth",
			serverName:         "test-server",
			expectedAuthMethod: model.AuthMethodNone,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockRegistry := new(MockRegistryService)
			mockAuthService := new(MockAuthService)

			// Setup mock to capture the auth method
			mockAuthService.On("ValidateAuth", mock.Anything, mock.MatchedBy(func(auth model.Authentication) bool {
				return auth.Method == tc.expectedAuthMethod
			})).Return(true, nil)
			mockRegistry.On("Publish", mock.AnythingOfType("*model.ServerDetail")).Return(nil)

			mux := http.NewServeMux()
			api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
			v0.RegisterPublishEndpoint(api, mockRegistry, mockAuthService)

			serverDetail := model.PublishRequest{
				ServerDetail: model.ServerDetail{
					Server: model.Server{
						ID:          "test-id",
						Name:        tc.serverName,
						Description: "A test server",
						VersionDetail: model.VersionDetail{
							Version:     "1.0.0",
							ReleaseDate: "2025-05-25T00:00:00Z",
							IsLatest:    true,
						},
					},
				},
			}

			requestBody, err := json.Marshal(serverDetail)
			assert.NoError(t, err)

			req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/v0/publish", bytes.NewBuffer(requestBody))
			assert.NoError(t, err)
			req.Header.Set("Authorization", "Bearer test_token")
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusOK, rr.Code)
			mockAuthService.AssertExpectations(t)
		})
	}
}