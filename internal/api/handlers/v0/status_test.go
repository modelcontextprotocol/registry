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
	"github.com/stretchr/testify/require"

	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/service"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

func TestUpdateServerStatusEndpoint(t *testing.T) {
	// Create registry service and insert test servers
	registryService := service.NewRegistryService(database.NewMemoryDB(), config.NewConfig())

	// Publish a test server
	testServer := apiv0.ServerJSON{
		Name:        "io.github.testuser/test-server",
		Description: "Test server for status updates",
		Repository: model.Repository{
			URL:    "https://github.com/testuser/test-server",
			Source: "github",
			ID:     "testuser/test-server",
		},
		Version: "1.0.0",
	}
	published, err := registryService.Publish(testServer)
	require.NoError(t, err)
	require.NotNil(t, published)
	require.NotNil(t, published.Meta)
	require.NotNil(t, published.Meta.Official)

	testServerID := published.Meta.Official.ServerID

	// Publish another server for permission testing
	otherServer := apiv0.ServerJSON{
		Name:        "io.github.otheruser/other-server",
		Description: "Other test server",
		Repository: model.Repository{
			URL:    "https://github.com/otheruser/other-server",
			Source: "github",
			ID:     "otheruser/other-server",
		},
		Version: "1.0.0",
	}
	otherPublished, err := registryService.Publish(otherServer)
	require.NoError(t, err)
	require.NotNil(t, otherPublished)
	require.NotNil(t, otherPublished.Meta)
	require.NotNil(t, otherPublished.Meta.Official)

	otherServerID := otherPublished.Meta.Official.ServerID

	testCases := []struct {
		name           string
		authHeader     string
		requestBody    interface{}
		serverID       string
		expectedStatus int
		expectedError  string
	}{
		{
			name: "successful status update by publisher",
			authHeader: func() string {
				cfg := &config.Config{JWTPrivateKey: "bb2c6b424005acd5df47a9e2c87f446def86dd740c888ea3efb825b23f7ef47c"}
				token, _ := generateTestJWTToken(cfg, auth.JWTClaims{
					AuthMethod:        auth.MethodGitHubAT,
					AuthMethodSubject: "testuser",
					Permissions: []auth.Permission{
						{Action: auth.PermissionActionEdit, ResourcePattern: "io.github.testuser/*"},
					},
				})
				return "Bearer " + token
			}(),
			requestBody: map[string]interface{}{
				"status": "deprecated",
			},
			serverID:       testServerID,
			expectedStatus: http.StatusOK,
		},
		{
			name: "successful status update by admin",
			authHeader: func() string {
				cfg := &config.Config{JWTPrivateKey: "bb2c6b424005acd5df47a9e2c87f446def86dd740c888ea3efb825b23f7ef47c"}
				token, _ := generateTestJWTToken(cfg, auth.JWTClaims{
					AuthMethod:        auth.MethodGitHubAT,
					AuthMethodSubject: "admin",
					Permissions: []auth.Permission{
						{Action: auth.PermissionActionEdit, ResourcePattern: "*"},
					},
				})
				return "Bearer " + token
			}(),
			requestBody: map[string]interface{}{
				"status": "deleted",
			},
			serverID:       otherServerID,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing authorization header",
			authHeader:     "",
			requestBody:    map[string]interface{}{"status": "deprecated"},
			serverID:       testServerID,
			expectedStatus: 422,
			expectedError:  "required header parameter is missing",
		},
		{
			name:       "invalid authorization header format",
			authHeader: "InvalidFormat token123",
			requestBody: map[string]interface{}{
				"status": "deprecated",
			},
			serverID:       testServerID,
			expectedStatus: http.StatusUnauthorized,
			expectedError:  "Unauthorized",
		},
		{
			name:       "invalid token",
			authHeader: "Bearer invalid-token",
			requestBody: map[string]interface{}{
				"status": "deprecated",
			},
			serverID:       testServerID,
			expectedStatus: http.StatusUnauthorized,
			expectedError:  "Unauthorized",
		},
		{
			name: "permission denied - wrong user",
			authHeader: func() string {
				cfg := &config.Config{JWTPrivateKey: "bb2c6b424005acd5df47a9e2c87f446def86dd740c888ea3efb825b23f7ef47c"}
				token, _ := generateTestJWTToken(cfg, auth.JWTClaims{
					AuthMethod:        auth.MethodGitHubAT,
					AuthMethodSubject: "wronguser",
					Permissions: []auth.Permission{
						{Action: auth.PermissionActionEdit, ResourcePattern: "io.github.wronguser/*"},
					},
				})
				return "Bearer " + token
			}(),
			requestBody: map[string]interface{}{
				"status": "deprecated",
			},
			serverID:       testServerID,
			expectedStatus: http.StatusForbidden,
			expectedError:  "Forbidden",
		},
		{
			name: "server not found",
			authHeader: func() string {
				cfg := &config.Config{JWTPrivateKey: "bb2c6b424005acd5df47a9e2c87f446def86dd740c888ea3efb825b23f7ef47c"}
				token, _ := generateTestJWTToken(cfg, auth.JWTClaims{
					AuthMethod: auth.MethodGitHubAT,
					Permissions: []auth.Permission{
						{Action: auth.PermissionActionEdit, ResourcePattern: "*"},
					},
				})
				return "Bearer " + token
			}(),
			requestBody: map[string]interface{}{
				"status": "deprecated",
			},
			serverID:       "550e8400-e29b-41d4-a716-446655440999", // Non-existent ID
			expectedStatus: http.StatusNotFound,
			expectedError:  "Not Found",
		},
		{
			name: "invalid status value",
			authHeader: func() string {
				cfg := &config.Config{JWTPrivateKey: "bb2c6b424005acd5df47a9e2c87f446def86dd740c888ea3efb825b23f7ef47c"}
				token, _ := generateTestJWTToken(cfg, auth.JWTClaims{
					AuthMethod: auth.MethodGitHubAT,
					Permissions: []auth.Permission{
						{Action: auth.PermissionActionEdit, ResourcePattern: "*"},
					},
				})
				return "Bearer " + token
			}(),
			requestBody: map[string]interface{}{
				"status": "invalid-status",
			},
			serverID:       testServerID,
			expectedStatus: 422, // Huma returns 422 for validation errors
			expectedError:  "validation failed",
		},
		{
			name: "cannot restore deleted server (anti-undelete protection)",
			authHeader: func() string {
				cfg := &config.Config{JWTPrivateKey: "bb2c6b424005acd5df47a9e2c87f446def86dd740c888ea3efb825b23f7ef47c"}
				token, _ := generateTestJWTToken(cfg, auth.JWTClaims{
					AuthMethod: auth.MethodGitHubAT,
					Permissions: []auth.Permission{
						{Action: auth.PermissionActionEdit, ResourcePattern: "*"},
					},
				})
				return "Bearer " + token
			}(),
			requestBody: map[string]interface{}{
				"status": "active", // Try to restore a deleted server
			},
			serverID: func() string {
				// First delete a server to test restoration
				deleteServer := apiv0.ServerJSON{
					Name:        "io.github.testuser/deleted-server",
					Description: "Server to be deleted for testing",
					Repository: model.Repository{
						URL:    "https://github.com/testuser/deleted-server",
						Source: "github",
						ID:     "testuser/deleted-server",
					},
					Version: "1.0.0",
				}
				published, _ := registryService.Publish(deleteServer)

				// Delete it using the status API
				_, _ = registryService.UpdateServerStatus(published.Meta.Official.ServerID, "deleted")

				return published.Meta.Official.ServerID
			}(),
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

			// Register status endpoints
			cfg := &config.Config{
				JWTPrivateKey: "bb2c6b424005acd5df47a9e2c87f446def86dd740c888ea3efb825b23f7ef47c",
			}
			v0.RegisterStatusEndpoints(api, registryService, cfg)

			// Create request body
			var requestBody []byte
			var err error
			if str, ok := tc.requestBody.(string); ok {
				requestBody = []byte(str)
			} else {
				requestBody, err = json.Marshal(tc.requestBody)
				require.NoError(t, err)
			}

			// Create request
			url := "/v0/servers/" + tc.serverID + "/status"
			req := httptest.NewRequest(http.MethodPatch, url, bytes.NewReader(requestBody))
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

			// If successful, verify the response contains the server with updated status
			if tc.expectedStatus == http.StatusOK {
				var response apiv0.ServerResponse
				err := json.Unmarshal(w.Body.Bytes(), &response)
				require.NoError(t, err)

				// Check that status was updated
				expectedStatus := tc.requestBody.(map[string]interface{})["status"].(string)
				assert.Equal(t, expectedStatus, string(response.Meta.Official.Status))
			}
		})
	}
}