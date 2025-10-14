package v0_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestEditServerEndpoint(t *testing.T) {
	// Create test config
	testSeed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(testSeed)
	require.NoError(t, err)
	cfg := &config.Config{
		JWTPrivateKey:            hex.EncodeToString(testSeed),
		EnableRegistryValidation: false,
	}

	// Create registry service and test data
	registryService := service.NewRegistryService(database.NewTestDB(t), cfg)

	// Create test servers for different scenarios
	testServers := map[string]*apiv0.ServerJSON{
		"editable": {
			Schema:      model.CurrentSchemaURL,
			Name:        "io.github.testuser/editable-server",
			Description: "Server that can be edited",
			Version:     "1.0.0",
			Repository: model.Repository{
				URL:    "https://github.com/testuser/editable-server",
				Source: "github",
				ID:     "testuser/editable-server",
			},
		},
		"other": {
			Schema:      model.CurrentSchemaURL,
			Name:        "io.github.otheruser/other-server",
			Description: "Server owned by another user",
			Version:     "1.0.0",
			Repository: model.Repository{
				URL:    "https://github.com/otheruser/other-server",
				Source: "github",
				ID:     "otheruser/other-server",
			},
		},
	}

	// Create the test servers
	for _, server := range testServers {
		_, err := registryService.CreateServer(context.Background(), server)
		require.NoError(t, err)
	}

	// Create a deleted server for undelete testing
	deletedServer := &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "io.github.testuser/deleted-server",
		Description: "Server that was deleted",
		Version:     "1.0.0",
		Repository: model.Repository{
			URL:    "https://github.com/testuser/deleted-server",
			Source: "github",
			ID:     "testuser/deleted-server",
		},
	}
	_, err = registryService.CreateServer(context.Background(), deletedServer)
	require.NoError(t, err)

	// Set the server to deleted status
	_, err = registryService.UpdateServer(context.Background(), deletedServer.Name, deletedServer.Version, deletedServer, stringPtr(string(model.StatusDeleted)))
	require.NoError(t, err)

	// Create a server with build metadata for URL encoding test
	buildMetadataServer := &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "io.github.testuser/build-metadata-server",
		Description: "Server with build metadata version",
		Version:     "1.0.0+20130313144700",
		Repository: model.Repository{
			URL:    "https://github.com/testuser/build-metadata-server",
			Source: "github",
			ID:     "testuser/build-metadata-server",
		},
	}
	_, err = registryService.CreateServer(context.Background(), buildMetadataServer)
	require.NoError(t, err)

	testCases := []struct {
		name           string
		serverName     string
		version        string
		authClaims     *auth.JWTClaims
		authHeader     string
		requestBody    apiv0.ServerJSON
		statusParam    string
		expectedStatus int
		expectedError  string
		checkResult    func(*testing.T, *apiv0.ServerResponse)
	}{
		{
			name:       "successful edit with valid permissions",
			serverName: "io.github.testuser/editable-server",
			version:    "1.0.0",
			authClaims: &auth.JWTClaims{
				AuthMethod:        auth.MethodGitHubAT,
				AuthMethodSubject: "testuser",
				Permissions: []auth.Permission{
					{Action: auth.PermissionActionEdit, ResourcePattern: "io.github.testuser/*"},
				},
			},
			requestBody: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "io.github.testuser/editable-server",
				Description: "Updated server description",
				Version:     "1.0.0",
				Repository: model.Repository{
					URL:    "https://github.com/testuser/editable-server",
					Source: "github",
					ID:     "testuser/editable-server",
				},
			},
			expectedStatus: http.StatusOK,
			checkResult: func(t *testing.T, resp *apiv0.ServerResponse) {
				t.Helper()
				assert.Equal(t, "Updated server description", resp.Server.Description)
				assert.Equal(t, "io.github.testuser/editable-server", resp.Server.Name)
				assert.Equal(t, "1.0.0", resp.Server.Version)
				assert.NotNil(t, resp.Meta.Official)
			},
		},
		{
			name:       "successful edit with status change",
			serverName: "io.github.testuser/editable-server",
			version:    "1.0.0",
			authClaims: &auth.JWTClaims{
				AuthMethod:        auth.MethodGitHubAT,
				AuthMethodSubject: "testuser",
				Permissions: []auth.Permission{
					{Action: auth.PermissionActionEdit, ResourcePattern: "io.github.testuser/*"},
				},
			},
			requestBody: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "io.github.testuser/editable-server",
				Description: "Server with status change",
				Version:     "1.0.0",
			},
			statusParam:    "deprecated",
			expectedStatus: http.StatusOK,
			checkResult: func(t *testing.T, resp *apiv0.ServerResponse) {
				t.Helper()
				assert.Equal(t, "Server with status change", resp.Server.Description)
				assert.Equal(t, model.StatusDeprecated, resp.Meta.Official.Status)
			},
		},
		{
			name:           "missing authorization header",
			serverName:     "io.github.testuser/editable-server",
			version:        "1.0.0",
			authHeader:     "", // No auth header
			requestBody:    apiv0.ServerJSON{},
			expectedStatus: http.StatusUnprocessableEntity,
			expectedError:  "required header parameter is missing",
		},
		{
			name:       "invalid authorization header format",
			serverName: "io.github.testuser/editable-server",
			version:    "1.0.0",
			authHeader: "InvalidFormat token123",
			requestBody: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "io.github.testuser/editable-server",
				Description: "Test server",
				Version:     "1.0.0",
			},
			expectedStatus: http.StatusUnauthorized,
			expectedError:  "Invalid Authorization header format",
		},
		{
			name:       "invalid token",
			serverName: "io.github.testuser/editable-server",
			version:    "1.0.0",
			authHeader: "Bearer invalid-token",
			requestBody: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "io.github.testuser/editable-server",
				Description: "Test server",
				Version:     "1.0.0",
			},
			expectedStatus: http.StatusUnauthorized,
			expectedError:  "Invalid or expired Registry JWT token",
		},
		{
			name:       "permission denied - no edit permissions",
			serverName: "io.github.testuser/editable-server",
			version:    "1.0.0",
			authClaims: &auth.JWTClaims{
				AuthMethod:        auth.MethodGitHubAT,
				AuthMethodSubject: "testuser",
				Permissions: []auth.Permission{
					{Action: auth.PermissionActionPublish, ResourcePattern: "io.github.testuser/*"}, // Only publish, not edit
				},
			},
			requestBody: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "io.github.testuser/editable-server",
				Description: "Updated test server",
				Version:     "1.0.0",
			},
			expectedStatus: http.StatusForbidden,
			expectedError:  "You do not have edit permissions",
		},
		{
			name:       "permission denied - wrong namespace",
			serverName: "io.github.otheruser/other-server",
			version:    "1.0.0",
			authClaims: &auth.JWTClaims{
				AuthMethod:        auth.MethodGitHubAT,
				AuthMethodSubject: "testuser",
				Permissions: []auth.Permission{
					{Action: auth.PermissionActionEdit, ResourcePattern: "io.github.testuser/*"}, // Wrong namespace
				},
			},
			requestBody: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "io.github.otheruser/other-server",
				Description: "Updated test server",
				Version:     "1.0.0",
			},
			expectedStatus: http.StatusForbidden,
			expectedError:  "You do not have edit permissions",
		},
		{
			name:       "server not found",
			serverName: "io.github.testuser/non-existent",
			version:    "1.0.0",
			authClaims: &auth.JWTClaims{
				AuthMethod:        auth.MethodGitHubAT,
				AuthMethodSubject: "testuser",
				Permissions: []auth.Permission{
					{Action: auth.PermissionActionEdit, ResourcePattern: "io.github.testuser/*"},
				},
			},
			requestBody: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "io.github.testuser/non-existent",
				Description: "Non-existent server",
				Version:     "1.0.0",
			},
			expectedStatus: http.StatusNotFound,
			expectedError:  "Server not found",
		},
		{
			name:       "attempt to rename server should fail",
			serverName: "io.github.testuser/editable-server",
			version:    "1.0.0",
			authClaims: &auth.JWTClaims{
				AuthMethod:        auth.MethodGitHubAT,
				AuthMethodSubject: "testuser",
				Permissions: []auth.Permission{
					{Action: auth.PermissionActionEdit, ResourcePattern: "io.github.testuser/*"},
				},
			},
			requestBody: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "io.github.testuser/renamed-server", // Different name
				Description: "Trying to rename server",
				Version:     "1.0.0",
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Cannot rename server",
		},
		{
			name:       "version in body must match URL parameter",
			serverName: "io.github.testuser/editable-server",
			version:    "1.0.0",
			authClaims: &auth.JWTClaims{
				AuthMethod:        auth.MethodGitHubAT,
				AuthMethodSubject: "testuser",
				Permissions: []auth.Permission{
					{Action: auth.PermissionActionEdit, ResourcePattern: "io.github.testuser/*"},
				},
			},
			requestBody: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "io.github.testuser/editable-server",
				Description: "Version mismatch test",
				Version:     "2.0.0", // Different version from URL
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Version in request body must match URL path parameter",
		},
		{
			name:       "attempt to undelete server should fail",
			serverName: "io.github.testuser/deleted-server",
			version:    "1.0.0",
			authClaims: &auth.JWTClaims{
				AuthMethod:        auth.MethodGitHubAT,
				AuthMethodSubject: "testuser",
				Permissions: []auth.Permission{
					{Action: auth.PermissionActionEdit, ResourcePattern: "io.github.testuser/*"},
				},
			},
			requestBody: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "io.github.testuser/deleted-server",
				Description: "Trying to undelete server",
				Version:     "1.0.0",
			},
			statusParam:    "active", // Trying to change from deleted to active
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Cannot change status of deleted server",
		},
		{
			name:       "successful edit of version with build metadata (URL encoded)",
			serverName: "io.github.testuser/build-metadata-server",
			version:    "1.0.0+20130313144700",
			authClaims: &auth.JWTClaims{
				AuthMethod:        auth.MethodGitHubAT,
				AuthMethodSubject: "testuser",
				Permissions: []auth.Permission{
					{Action: auth.PermissionActionEdit, ResourcePattern: "io.github.testuser/*"},
				},
			},
			requestBody: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "io.github.testuser/build-metadata-server",
				Description: "Updated server with build metadata",
				Version:     "1.0.0+20130313144700",
				Repository: model.Repository{
					URL:    "https://github.com/testuser/build-metadata-server",
					Source: "github",
					ID:     "testuser/build-metadata-server",
				},
			},
			expectedStatus: http.StatusOK,
			checkResult: func(t *testing.T, resp *apiv0.ServerResponse) {
				t.Helper()
				assert.Equal(t, "Updated server with build metadata", resp.Server.Description)
				assert.Equal(t, "io.github.testuser/build-metadata-server", resp.Server.Name)
				assert.Equal(t, "1.0.0+20130313144700", resp.Server.Version)
				assert.NotNil(t, resp.Meta.Official)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create Huma API
			mux := http.NewServeMux()
			api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))

			// Register edit endpoints
			v0.RegisterEditEndpoints(api, "/v0", registryService, cfg)

			// Create request body
			requestBody, err := json.Marshal(tc.requestBody)
			require.NoError(t, err)

			// Create request URL with proper encoding
			encodedServerName := url.PathEscape(tc.serverName)
			encodedVersion := url.PathEscape(tc.version)
			requestURL := "/v0/servers/" + encodedServerName + "/versions/" + encodedVersion
			if tc.statusParam != "" {
				requestURL += "?status=" + tc.statusParam
			}

			req := httptest.NewRequest(http.MethodPut, requestURL, bytes.NewReader(requestBody))
			req.Header.Set("Content-Type", "application/json")

			// Set authorization header
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			} else if tc.authClaims != nil {
				// Generate valid JWT token
				jwtManager := auth.NewJWTManager(cfg)
				tokenResponse, err := jwtManager.GenerateTokenResponse(context.Background(), *tc.authClaims)
				require.NoError(t, err)
				req.Header.Set("Authorization", "Bearer "+tokenResponse.RegistryToken)
			}

			// Create response recorder and execute request
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			// Check response
			assert.Equal(t, tc.expectedStatus, w.Code)

			if tc.expectedError != "" {
				assert.Contains(t, w.Body.String(), tc.expectedError)
			}

			if tc.expectedStatus == http.StatusOK && tc.checkResult != nil {
				var response apiv0.ServerResponse
				err := json.NewDecoder(w.Body).Decode(&response)
				require.NoError(t, err)
				tc.checkResult(t, &response)
			}
		})
	}
}

func TestEditServerEndpointEdgeCases(t *testing.T) {
	// Create test config
	testSeed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(testSeed)
	require.NoError(t, err)
	cfg := &config.Config{
		JWTPrivateKey:            hex.EncodeToString(testSeed),
		EnableRegistryValidation: false,
	}

	// Create registry service
	registryService := service.NewRegistryService(database.NewTestDB(t), cfg)

	// Setup test servers with different characteristics
	testServers := []struct {
		name    string
		version string
		status  model.Status
	}{
		{"com.example/active-server", "1.0.0", model.StatusActive},
		{"com.example/deprecated-server", "1.0.0", model.StatusDeprecated},
		{"com.example/multi-version-server", "1.0.0", model.StatusActive},
		{"com.example/multi-version-server", "2.0.0", model.StatusActive},
	}

	for _, server := range testServers {
		_, err := registryService.CreateServer(context.Background(), &apiv0.ServerJSON{
			Schema:      model.CurrentSchemaURL,
			Name:        server.name,
			Description: "Test server for editing",
			Version:     server.version,
		})
		require.NoError(t, err)

		// Set specific status if not active
		if server.status != model.StatusActive {
			_, err = registryService.UpdateServer(context.Background(), server.name, server.version, &apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        server.name,
				Description: "Test server for editing",
				Version:     server.version,
			}, stringPtr(string(server.status)))
			require.NoError(t, err)
		}
	}

	// Create API
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	v0.RegisterEditEndpoints(api, "/v0", registryService, cfg)

	t.Run("status transitions", func(t *testing.T) {
		tests := []struct {
			name           string
			serverName     string
			version        string
			fromStatus     string
			toStatus       string
			expectedStatus int
			expectedError  string
		}{
			{
				name:           "active to deprecated",
				serverName:     "com.example/active-server",
				version:        "1.0.0",
				toStatus:       "deprecated",
				expectedStatus: http.StatusOK,
			},
			{
				name:           "deprecated to active",
				serverName:     "com.example/deprecated-server",
				version:        "1.0.0",
				toStatus:       "active",
				expectedStatus: http.StatusOK,
			},
			{
				name:           "active to deleted",
				serverName:     "com.example/active-server",
				version:        "1.0.0",
				toStatus:       "deleted",
				expectedStatus: http.StatusOK,
			},
			{
				name:           "invalid status",
				serverName:     "com.example/active-server",
				version:        "1.0.0",
				toStatus:       "invalid_status",
				expectedStatus: http.StatusUnprocessableEntity,
				expectedError:  "validation failed",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				requestBody := apiv0.ServerJSON{
					Schema:      model.CurrentSchemaURL,
					Name:        tt.serverName,
					Description: "Status transition test",
					Version:     tt.version,
				}

				bodyBytes, err := json.Marshal(requestBody)
				require.NoError(t, err)

				encodedName := url.PathEscape(tt.serverName)
				requestURL := "/v0/servers/" + encodedName + "/versions/" + tt.version + "?status=" + tt.toStatus

				req := httptest.NewRequest(http.MethodPut, requestURL, bytes.NewReader(bodyBytes))
				req.Header.Set("Content-Type", "application/json")

				// Generate admin token
				jwtManager := auth.NewJWTManager(cfg)
				tokenResponse, err := jwtManager.GenerateTokenResponse(context.Background(), auth.JWTClaims{
					AuthMethod: auth.MethodNone,
					Permissions: []auth.Permission{
						{Action: auth.PermissionActionEdit, ResourcePattern: "*"},
					},
				})
				require.NoError(t, err)
				req.Header.Set("Authorization", "Bearer "+tokenResponse.RegistryToken)

				w := httptest.NewRecorder()
				mux.ServeHTTP(w, req)

				assert.Equal(t, tt.expectedStatus, w.Code)

				if tt.expectedError != "" {
					assert.Contains(t, w.Body.String(), tt.expectedError)
				}

				if tt.expectedStatus == http.StatusOK {
					var response apiv0.ServerResponse
					err := json.NewDecoder(w.Body).Decode(&response)
					require.NoError(t, err)
					assert.Equal(t, model.Status(tt.toStatus), response.Meta.Official.Status)
				}
			})
		}
	})

	t.Run("URL encoding edge cases", func(t *testing.T) {
		// Create server with special characters
		specialServerName := "io.dots.and-dashes/server_with_underscores"
		_, err := registryService.CreateServer(context.Background(), &apiv0.ServerJSON{
			Schema:      model.CurrentSchemaURL,
			Name:        specialServerName,
			Description: "Server with special characters",
			Version:     "1.0.0",
		})
		require.NoError(t, err)

		requestBody := apiv0.ServerJSON{
			Schema:      model.CurrentSchemaURL,
			Name:        specialServerName,
			Description: "Updated server with special chars",
			Version:     "1.0.0",
		}

		bodyBytes, err := json.Marshal(requestBody)
		require.NoError(t, err)

		encodedName := url.PathEscape(specialServerName)
		requestURL := "/v0/servers/" + encodedName + "/versions/1.0.0"

		req := httptest.NewRequest(http.MethodPut, requestURL, bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		// Generate admin token
		jwtManager := auth.NewJWTManager(cfg)
		tokenResponse, err := jwtManager.GenerateTokenResponse(context.Background(), auth.JWTClaims{
			AuthMethod: auth.MethodNone,
			Permissions: []auth.Permission{
				{Action: auth.PermissionActionEdit, ResourcePattern: "*"},
			},
		})
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+tokenResponse.RegistryToken)

		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response apiv0.ServerResponse
		err = json.NewDecoder(w.Body).Decode(&response)
		require.NoError(t, err)
		assert.Equal(t, specialServerName, response.Server.Name)
		assert.Equal(t, "Updated server with special chars", response.Server.Description)
	})

	t.Run("version-specific editing", func(t *testing.T) {
		// Test editing a specific version of a multi-version server
		requestBody := apiv0.ServerJSON{
			Schema:      model.CurrentSchemaURL,
			Name:        "com.example/multi-version-server",
			Description: "Updated v1.0.0 specifically",
			Version:     "1.0.0",
		}

		bodyBytes, err := json.Marshal(requestBody)
		require.NoError(t, err)

		encodedName := url.PathEscape("com.example/multi-version-server")
		requestURL := "/v0/servers/" + encodedName + "/versions/1.0.0"

		req := httptest.NewRequest(http.MethodPut, requestURL, bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		// Generate admin token
		jwtManager := auth.NewJWTManager(cfg)
		tokenResponse, err := jwtManager.GenerateTokenResponse(context.Background(), auth.JWTClaims{
			AuthMethod: auth.MethodNone,
			Permissions: []auth.Permission{
				{Action: auth.PermissionActionEdit, ResourcePattern: "*"},
			},
		})
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+tokenResponse.RegistryToken)

		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response apiv0.ServerResponse
		err = json.NewDecoder(w.Body).Decode(&response)
		require.NoError(t, err)
		assert.Equal(t, "Updated v1.0.0 specifically", response.Server.Description)
		assert.Equal(t, "1.0.0", response.Server.Version)

		// Verify the other version wasn't affected
		otherVersion, err := registryService.GetServerByNameAndVersion(context.Background(), "com.example/multi-version-server", "2.0.0")
		require.NoError(t, err)
		assert.NotEqual(t, "Updated v1.0.0 specifically", otherVersion.Server.Description)
	})
}

// Helper function
func stringPtr(s string) *string {
	return &s
}
