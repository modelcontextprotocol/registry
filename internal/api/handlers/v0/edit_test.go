package v0_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/model"
)

func TestEditServerEndpoint(t *testing.T) {
	testCases := []struct {
		name           string
		serverID       string
		authHeader     string
		requestBody    interface{}
		setupMocks     func(*MockRegistryService)
		expectedStatus int
		expectedError  string
	}{
		{
			name:     "successful edit with valid token and permissions",
			serverID: "550e8400-e29b-41d4-a716-446655440001",
			authHeader: func() string {
				cfg := &config.Config{JWTPrivateKey: "bb2c6b424005acd5df47a9e2c87f446def86dd740c888ea3efb825b23f7ef47c"}
				token, _ := generateTestJWTToken(cfg, auth.JWTClaims{
					AuthMethod:        model.AuthMethodGitHubAT,
					AuthMethodSubject: "domdomegg",
					Permissions: []auth.Permission{
						{Action: auth.PermissionActionEdit, ResourcePattern: "io.github.domdomegg/*"},
					},
				})
				return "Bearer " + token
			}(),
			requestBody: model.PublishRequest{
				Server: model.ServerDetail{
					Name:        "io.github.domdomegg/test-server",
					Description: "Updated test server",
					Status:      model.ServerStatusDeprecated,
					Repository: model.Repository{
						URL:    "https://github.com/domdomegg/test-server",
						Source: "github",
						ID:     "domdomegg/test-server",
					},
					VersionDetail: model.VersionDetail{
						Version: "1.0.1",
					},
				},
			},
			setupMocks: func(registry *MockRegistryService) {
				// Current server (not deleted)
				currentServer := &model.ServerResponse{
					Server: model.ServerDetail{
						Name:        "io.github.domdomegg/test-server",
						Description: "Original server",
						Status:      model.ServerStatusActive,
					},
				}
				registry.On("GetByID", "550e8400-e29b-41d4-a716-446655440001").Return(currentServer, nil)
				
				expectedResponse := &model.ServerResponse{
					Server: model.ServerDetail{
						Name:        "io.github.domdomegg/test-server",
						Description: "Updated test server",
						Status:      model.ServerStatusDeprecated,
					},
				}
				registry.On("EditServer", "550e8400-e29b-41d4-a716-446655440001", mock.AnythingOfType("model.PublishRequest")).Return(expectedResponse, nil)
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing authorization header",
			serverID:       "550e8400-e29b-41d4-a716-446655440001",
			authHeader:     "",
			requestBody:    model.PublishRequest{},
			setupMocks:     func(_ *MockRegistryService) {},
			expectedStatus: 422,
			expectedError:  "required header parameter is missing",
		},
		{
			name:           "invalid authorization header format",
			serverID:       "550e8400-e29b-41d4-a716-446655440001",
			authHeader:     "InvalidFormat token123",
			requestBody:    model.PublishRequest{},
			setupMocks:     func(_ *MockRegistryService) {},
			expectedStatus: http.StatusUnauthorized,
			expectedError:  "Unauthorized",
		},
		{
			name:           "invalid token",
			serverID:       "550e8400-e29b-41d4-a716-446655440001",
			authHeader:     "Bearer invalid-token",
			requestBody:    model.PublishRequest{},
			setupMocks:     func(_ *MockRegistryService) {},
			expectedStatus: http.StatusUnauthorized,
			expectedError:  "Unauthorized",
		},
		{
			name:     "permission denied - no edit permissions",
			serverID: "550e8400-e29b-41d4-a716-446655440001",
			authHeader: func() string {
				cfg := &config.Config{JWTPrivateKey: "bb2c6b424005acd5df47a9e2c87f446def86dd740c888ea3efb825b23f7ef47c"}
				token, _ := generateTestJWTToken(cfg, auth.JWTClaims{
					AuthMethod:        model.AuthMethodGitHubAT,
					AuthMethodSubject: "domdomegg",
					Permissions: []auth.Permission{
						{Action: auth.PermissionActionPublish, ResourcePattern: "io.github.domdomegg/*"},
					},
				})
				return "Bearer " + token
			}(),
			requestBody: model.PublishRequest{
				Server: model.ServerDetail{
					Name:        "io.github.domdomegg/test-server",
					Description: "Updated test server",
				},
			},
			setupMocks: func(registry *MockRegistryService) {
				// Need to mock GetByID since we check permissions against existing server name
				currentServer := &model.ServerResponse{
					Server: model.ServerDetail{
						Name:        "io.github.domdomegg/test-server",
						Description: "Original server",
						Status:      model.ServerStatusActive,
					},
				}
				registry.On("GetByID", "550e8400-e29b-41d4-a716-446655440001").Return(currentServer, nil)
			},
			expectedStatus: http.StatusForbidden,
			expectedError:  "Forbidden",
		},
		{
			name:     "permission denied - wrong resource",
			serverID: "550e8400-e29b-41d4-a716-446655440001",
			authHeader: func() string {
				cfg := &config.Config{JWTPrivateKey: "bb2c6b424005acd5df47a9e2c87f446def86dd740c888ea3efb825b23f7ef47c"}
				token, _ := generateTestJWTToken(cfg, auth.JWTClaims{
					AuthMethod:        model.AuthMethodGitHubAT,
					AuthMethodSubject: "domdomegg",
					Permissions: []auth.Permission{
						{Action: auth.PermissionActionEdit, ResourcePattern: "io.github.domdomegg/*"},
					},
				})
				return "Bearer " + token
			}(),
			requestBody: model.PublishRequest{
				Server: model.ServerDetail{
					Name:        "io.github.other/test-server",
					Description: "Updated test server",
				},
			},
			setupMocks: func(registry *MockRegistryService) {
				// Need to mock GetByID since we check permissions against existing server name
				// This test case shows a different scenario: existing server is "other" but user only has perms for "domdomegg"
				currentServer := &model.ServerResponse{
					Server: model.ServerDetail{
						Name:        "io.github.other/test-server",
						Description: "Original server",
						Status:      model.ServerStatusActive,
					},
				}
				registry.On("GetByID", "550e8400-e29b-41d4-a716-446655440001").Return(currentServer, nil)
			},
			expectedStatus: http.StatusForbidden,
			expectedError:  "Forbidden",
		},
		{
			name:     "server not found",
			serverID: "550e8400-e29b-41d4-a716-446655440001",
			authHeader: func() string {
				cfg := &config.Config{JWTPrivateKey: "bb2c6b424005acd5df47a9e2c87f446def86dd740c888ea3efb825b23f7ef47c"}
				token, _ := generateTestJWTToken(cfg, auth.JWTClaims{
					AuthMethod:        model.AuthMethodGitHubAT,
					AuthMethodSubject: "domdomegg",
					Permissions: []auth.Permission{
						{Action: auth.PermissionActionEdit, ResourcePattern: "io.github.domdomegg/*"},
					},
				})
				return "Bearer " + token
			}(),
			requestBody: model.PublishRequest{
				Server: model.ServerDetail{
					Name:        "io.github.domdomegg/test-server",
					Description: "Updated test server",
				},
			},
			setupMocks: func(registry *MockRegistryService) {
				registry.On("GetByID", "550e8400-e29b-41d4-a716-446655440001").Return(nil, database.ErrNotFound)
			},
			expectedStatus: http.StatusNotFound,
			expectedError:  "Not Found",
		},
		{
			name:     "service error",
			serverID: "550e8400-e29b-41d4-a716-446655440001",
			authHeader: func() string {
				cfg := &config.Config{JWTPrivateKey: "bb2c6b424005acd5df47a9e2c87f446def86dd740c888ea3efb825b23f7ef47c"}
				token, _ := generateTestJWTToken(cfg, auth.JWTClaims{
					AuthMethod:        model.AuthMethodGitHubAT,
					AuthMethodSubject: "domdomegg",
					Permissions: []auth.Permission{
						{Action: auth.PermissionActionEdit, ResourcePattern: "*"},
					},
				})
				return "Bearer " + token
			}(),
			requestBody: model.PublishRequest{
				Server: model.ServerDetail{
					Name:        "io.github.domdomegg/test-server",
					Description: "Updated test server",
				},
			},
			setupMocks: func(registry *MockRegistryService) {
				// Current server (not deleted)
				currentServer := &model.ServerResponse{
					Server: model.ServerDetail{
						Name:        "io.github.domdomegg/test-server",
						Description: "Original server",
						Status:      model.ServerStatusActive,
					},
				}
				registry.On("GetByID", "550e8400-e29b-41d4-a716-446655440001").Return(currentServer, nil)
				registry.On("EditServer", "550e8400-e29b-41d4-a716-446655440001", mock.AnythingOfType("model.PublishRequest")).Return(nil, errors.New("database error"))
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Bad Request",
		},
		{
			name:     "invalid request body",
			serverID: "550e8400-e29b-41d4-a716-446655440001",
			authHeader: func() string {
				cfg := &config.Config{JWTPrivateKey: "bb2c6b424005acd5df47a9e2c87f446def86dd740c888ea3efb825b23f7ef47c"}
				token, _ := generateTestJWTToken(cfg, auth.JWTClaims{
					AuthMethod:        model.AuthMethodGitHubAT,
					AuthMethodSubject: "domdomegg",
					Permissions: []auth.Permission{
						{Action: auth.PermissionActionEdit, ResourcePattern: "*"},
					},
				})
				return "Bearer " + token
			}(),
			requestBody:    "invalid json",
			setupMocks:     func(_ *MockRegistryService) {},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Bad Request",
		},
		{
			name:     "cannot undelete server",
			serverID: "550e8400-e29b-41d4-a716-446655440001",
			authHeader: func() string {
				cfg := &config.Config{JWTPrivateKey: "bb2c6b424005acd5df47a9e2c87f446def86dd740c888ea3efb825b23f7ef47c"}
				token, _ := generateTestJWTToken(cfg, auth.JWTClaims{
					AuthMethod:        model.AuthMethodGitHubAT,
					AuthMethodSubject: "domdomegg",
					Permissions: []auth.Permission{
						{Action: auth.PermissionActionEdit, ResourcePattern: "*"},
					},
				})
				return "Bearer " + token
			}(),
			requestBody: model.PublishRequest{
				Server: model.ServerDetail{
					Name:        "io.github.domdomegg/test-server",
					Description: "Trying to undelete server",
					Status:      model.ServerStatusActive,
				},
			},
			setupMocks: func(registry *MockRegistryService) {
				// Current server is deleted
				currentServer := &model.ServerResponse{
					Server: model.ServerDetail{
						Name:        "io.github.domdomegg/test-server",
						Description: "Original server",
						Status:      model.ServerStatusDeleted,
					},
				}
				registry.On("GetByID", "550e8400-e29b-41d4-a716-446655440001").Return(currentServer, nil)
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Cannot change status of deleted server",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create mock registry
			mockRegistry := new(MockRegistryService)
			tc.setupMocks(mockRegistry)

			// Create Huma API
			mux := http.NewServeMux()
			humaConfig := huma.DefaultConfig("Test API", "1.0.0")
			api := humago.New(mux, humaConfig)

			// Register edit endpoints
			cfg := &config.Config{
				JWTPrivateKey: "bb2c6b424005acd5df47a9e2c87f446def86dd740c888ea3efb825b23f7ef47c",
			}
			v0.RegisterEditEndpoints(api, mockRegistry, cfg)

			// Create request body
			var requestBody []byte
			var err error
			if str, ok := tc.requestBody.(string); ok {
				requestBody = []byte(str)
			} else {
				requestBody, err = json.Marshal(tc.requestBody)
				assert.NoError(t, err)
			}

			// Create request
			req := httptest.NewRequest(http.MethodPut, "/v0/servers/"+tc.serverID, bytes.NewReader(requestBody))
			req.Header.Set("Content-Type", "application/json")
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}

			// Create response recorder
			w := httptest.NewRecorder()

			// Call the endpoint
			mux.ServeHTTP(w, req)

			// Check status code
			assert.Equal(t, tc.expectedStatus, w.Code)

			// Check error message if expected
			if tc.expectedError != "" {
				assert.Contains(t, w.Body.String(), tc.expectedError)
			}

			mockRegistry.AssertExpectations(t)
		})
	}
}