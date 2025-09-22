package v0_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/stretchr/testify/assert"

	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/service"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

func TestEditServerEndpoint(t *testing.T) {
	// Create registry service and insert a common test server
	registryService := service.NewRegistryService(database.NewMemoryDB(), config.NewConfig())

	// Publish a test server that will be used across test cases
	testServer := apiv0.ServerJSON{
		Name:        "io.github.domdomegg/test-server",
		Description: "Original test server",
		Status:      model.StatusActive,
		Repository: model.Repository{
			URL:    "https://github.com/domdomegg/test-server",
			Source: "github",
			ID:     "domdomegg/test-server",
		},
		Version: "1.0.0",
	}
	published, err := registryService.Publish(testServer)
	assert.NoError(t, err)
	assert.NotNil(t, published)
	assert.NotNil(t, published.Meta)
	assert.NotNil(t, published.Meta.Official)

	testServerID := published.Meta.Official.ServerID

	// Publish a second server for permission testing
	otherServer := apiv0.ServerJSON{
		Name:        "io.github.other/test-server",
		Description: "Other test server",
		Status:      model.StatusActive,
		Repository: model.Repository{
			URL:    "https://github.com/other/test-server",
			Source: "github",
			ID:     "other/test-server",
		},
		Version: "1.0.0",
	}
	otherPublished, err := registryService.Publish(otherServer)
	assert.NoError(t, err)
	assert.NotNil(t, otherPublished)
	assert.NotNil(t, otherPublished.Meta)
	assert.NotNil(t, otherPublished.Meta.Official)

	otherServerID := otherPublished.Meta.Official.ServerID

	// Publish a deleted server for undelete testing
	deletedServer := apiv0.ServerJSON{
		Name:        "io.github.domdomegg/deleted-server",
		Description: "Deleted test server",
		Status:      model.StatusDeleted,
		Repository: model.Repository{
			URL:    "https://github.com/domdomegg/deleted-server",
			Source: "github",
			ID:     "domdomegg/deleted-server",
		},
		Version: "1.0.0",
	}
	deletedPublished, err := registryService.Publish(deletedServer)
	assert.NoError(t, err)
	assert.NotNil(t, deletedPublished)
	assert.NotNil(t, deletedPublished.Meta)
	assert.NotNil(t, deletedPublished.Meta.Official)

	deletedServerID := deletedPublished.Meta.Official.ServerID

	testCases := []struct {
		name           string
		authHeader     string
		requestBody    interface{}
		serverID       string
		version        string
		expectedStatus int
		expectedError  string
	}{
		{
			name: "successful edit with valid token and permissions",
			authHeader: func() string {
				cfg := &config.Config{JWTPrivateKey: "bb2c6b424005acd5df47a9e2c87f446def86dd740c888ea3efb825b23f7ef47c"}
				token, _ := generateTestJWTToken(cfg, auth.JWTClaims{
					AuthMethod:        auth.MethodGitHubAT,
					AuthMethodSubject: "domdomegg",
					Permissions: []auth.Permission{
						{Action: auth.PermissionActionEdit, ResourcePattern: "io.github.domdomegg/*"},
					},
				})
				return "Bearer " + token
			}(),
			requestBody: apiv0.ServerJSON{
				Name:        "io.github.domdomegg/test-server",
				Description: "Updated test server",
				Status:      model.StatusDeprecated,
				Repository: model.Repository{
					URL:    "https://github.com/domdomegg/test-server",
					Source: "github",
					ID:     "domdomegg/test-server",
				},
				Version: "1.0.0",
			},
			serverID:       testServerID,
			version:        "1.0.0",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing authorization header",
			authHeader:     "",
			requestBody:    apiv0.ServerJSON{},
			serverID:       testServerID,
			version:        "1.0.0",
			expectedStatus: 422,
			expectedError:  "required header parameter is missing",
		},
		{
			name:       "invalid authorization header format",
			authHeader: "InvalidFormat token123",
			requestBody: apiv0.ServerJSON{
				Name:        "io.github.domdomegg/test-server",
				Description: "Test server",
				Version:     "1.0.0",
			},
			serverID:       testServerID,
			version:        "1.0.0",
			expectedStatus: http.StatusUnauthorized,
			expectedError:  "Unauthorized",
		},
		{
			name:       "invalid token",
			authHeader: "Bearer invalid-token",
			requestBody: apiv0.ServerJSON{
				Name:        "io.github.domdomegg/test-server",
				Description: "Test server",
				Version:     "1.0.0",
			},
			serverID:       testServerID,
			version:        "1.0.0",
			expectedStatus: http.StatusUnauthorized,
			expectedError:  "Unauthorized",
		},
		{
			name: "permission denied - no edit permissions",
			authHeader: func() string {
				cfg := &config.Config{JWTPrivateKey: "bb2c6b424005acd5df47a9e2c87f446def86dd740c888ea3efb825b23f7ef47c"}
				token, _ := generateTestJWTToken(cfg, auth.JWTClaims{
					AuthMethod:        auth.MethodGitHubAT,
					AuthMethodSubject: "domdomegg",
					Permissions: []auth.Permission{
						{Action: auth.PermissionActionPublish, ResourcePattern: "io.github.domdomegg/*"},
					},
				})
				return "Bearer " + token
			}(),
			requestBody: apiv0.ServerJSON{
				Name:        "io.github.domdomegg/test-server",
				Description: "Updated test server",
				Version:     "1.0.0",
			},
			serverID:       testServerID,
			version:        "1.0.0",
			expectedStatus: http.StatusForbidden,
			expectedError:  "Forbidden",
		},
		{
			name: "permission denied - wrong resource",
			authHeader: func() string {
				cfg := &config.Config{JWTPrivateKey: "bb2c6b424005acd5df47a9e2c87f446def86dd740c888ea3efb825b23f7ef47c"}
				token, _ := generateTestJWTToken(cfg, auth.JWTClaims{
					AuthMethod:        auth.MethodGitHubAT,
					AuthMethodSubject: "domdomegg",
					Permissions: []auth.Permission{
						{Action: auth.PermissionActionEdit, ResourcePattern: "io.github.domdomegg/*"},
					},
				})
				return "Bearer " + token
			}(),
			requestBody: apiv0.ServerJSON{
				Name:        "io.github.other/test-server",
				Description: "Updated test server",
				Version:     "1.0.0",
			},
			serverID:       otherServerID,
			version:        "1.0.0",
			expectedStatus: http.StatusForbidden,
			expectedError:  "Forbidden",
		},
		{
			name: "server not found",
			authHeader: func() string {
				cfg := &config.Config{JWTPrivateKey: "bb2c6b424005acd5df47a9e2c87f446def86dd740c888ea3efb825b23f7ef47c"}
				token, _ := generateTestJWTToken(cfg, auth.JWTClaims{
					AuthMethod:        auth.MethodGitHubAT,
					AuthMethodSubject: "domdomegg",
					Permissions: []auth.Permission{
						{Action: auth.PermissionActionEdit, ResourcePattern: "io.github.domdomegg/*"},
					},
				})
				return "Bearer " + token
			}(),
			requestBody: apiv0.ServerJSON{
				Name:        "io.github.domdomegg/nonexistent-server",
				Description: "Updated test server",
				Version:     "1.0.0",
			},
			serverID:       "550e8400-e29b-41d4-a716-446655440999", // Non-existent ID
			version:        "1.0.0",
			expectedStatus: http.StatusNotFound,
			expectedError:  "Not Found",
		},
		{
			name: "validation error - invalid server name",
			authHeader: func() string {
				cfg := &config.Config{JWTPrivateKey: "bb2c6b424005acd5df47a9e2c87f446def86dd740c888ea3efb825b23f7ef47c"}
				token, _ := generateTestJWTToken(cfg, auth.JWTClaims{
					AuthMethod:        auth.MethodGitHubAT,
					AuthMethodSubject: "domdomegg",
					Permissions: []auth.Permission{
						{Action: auth.PermissionActionEdit, ResourcePattern: "*"},
					},
				})
				return "Bearer " + token
			}(),
			requestBody: apiv0.ServerJSON{
				Name:        "invalid-name-format", // Missing namespace/name format
				Description: "Test server",
				Version:     "1.0.0",
			},
			serverID:       testServerID,
			version:        "1.0.0",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Bad Request",
		},
		{
			name: "cannot undelete server",
			authHeader: func() string {
				cfg := &config.Config{JWTPrivateKey: "bb2c6b424005acd5df47a9e2c87f446def86dd740c888ea3efb825b23f7ef47c"}
				token, _ := generateTestJWTToken(cfg, auth.JWTClaims{
					AuthMethod:        auth.MethodGitHubAT,
					AuthMethodSubject: "domdomegg",
					Permissions: []auth.Permission{
						{Action: auth.PermissionActionEdit, ResourcePattern: "*"},
					},
				})
				return "Bearer " + token
			}(),
			requestBody: apiv0.ServerJSON{
				Name:        "io.github.domdomegg/deleted-server",
				Description: "Trying to undelete server",
				Status:      model.StatusActive,
				Repository: model.Repository{
					URL:    "https://github.com/domdomegg/deleted-server",
					Source: "github",
					ID:     "domdomegg/deleted-server",
				},
				Version: "1.0.1",
			},
			serverID:       deletedServerID,
			version:        "1.0.0",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Cannot change status of deleted server",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create Huma API
			mux := http.NewServeMux()
			humaConfig := huma.DefaultConfig("Test API", "1.0.0")
			api := humago.New(mux, humaConfig)

			// Register edit endpoints
			cfg := &config.Config{
				JWTPrivateKey: "bb2c6b424005acd5df47a9e2c87f446def86dd740c888ea3efb825b23f7ef47c",
			}
			v0.RegisterEditEndpoints(api, registryService, cfg)

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
			url := "/v0/servers/" + tc.serverID
			if tc.version != "" {
				url += "?version=" + tc.version
			}
			req := httptest.NewRequest(http.MethodPut, url, bytes.NewReader(requestBody))
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

			// No mock assertions needed with real service
		})
	}
}
