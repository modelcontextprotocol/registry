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
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/service"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
		JWTPrivateKey:            hex.EncodeToString(testSeed),
		EnableRegistryValidation: false, // Disable for unit tests
	}

	testCases := []struct {
		name                 string
		requestBody          interface{}
		tokenClaims          *auth.JWTClaims
		authHeader           string
		setupRegistryService func(service.RegistryService)
		expectedStatus       int
		expectedError        string
	}{
		{
			name: "successful publish with GitHub auth",
			requestBody: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "io.github.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/example/test-server",
					Source: "github",
					ID:     "example/test-server",
				},
				Version: "1.0.0",
			},
			tokenClaims: &auth.JWTClaims{
				AuthMethod:        auth.MethodGitHubAT,
				AuthMethodSubject: "example",
				Permissions: []auth.Permission{
					{Action: auth.PermissionActionPublish, ResourcePattern: "io.github.example/*"},
				},
			},
			setupRegistryService: func(_ service.RegistryService) {
				// Empty registry - no setup needed
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "successful publish with no auth (AuthMethodNone)",
			requestBody: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "example/test-server",
				Description: "A test server without auth",
				Repository: model.Repository{
					URL:    "https://github.com/example/test-server",
					Source: "github",
					ID:     "example/test-server",
				},
				Version: "1.0.0",
			},
			tokenClaims: &auth.JWTClaims{
				AuthMethod: auth.MethodNone,
				Permissions: []auth.Permission{
					{Action: auth.PermissionActionPublish, ResourcePattern: "example/*"},
				},
			},
			setupRegistryService: func(_ service.RegistryService) {
				// Empty registry - no setup needed
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:        "missing authorization header",
			requestBody: apiv0.ServerJSON{},
			authHeader:  "", // Empty auth header
			setupRegistryService: func(_ service.RegistryService) {
				// Empty registry - no setup needed
			},
			expectedStatus: http.StatusUnprocessableEntity,
			expectedError:  "required header parameter is missing",
		},
		{
			name: "invalid authorization header format",
			requestBody: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "io.github.domdomegg/test-server",
				Description: "Test server",
				Version:     "1.0.0",
			},
			authHeader: "InvalidFormat",
			setupRegistryService: func(_ service.RegistryService) {
				// Empty registry - no setup needed
			},
			expectedStatus: http.StatusUnauthorized,
			expectedError:  "Invalid Authorization header format",
		},
		{
			name: "invalid token",
			requestBody: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "test-server",
				Description: "A test server",
				Version:     "1.0.0",
			},
			authHeader: "Bearer invalidToken",
			setupRegistryService: func(_ service.RegistryService) {
				// Empty registry - no setup needed
			},
			expectedStatus: http.StatusUnauthorized,
			expectedError:  "Invalid or expired Registry JWT token",
		},
		{
			name: "permission denied",
			requestBody: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "io.github.other/test-server",
				Description: "A test server",
				Version:     "1.0.0",
				Repository: model.Repository{
					URL:    "https://github.com/example/test-server",
					Source: "github",
					ID:     "example/test-server",
				},
			},
			tokenClaims: &auth.JWTClaims{
				AuthMethod: auth.MethodGitHubAT,
				Permissions: []auth.Permission{
					{Action: auth.PermissionActionPublish, ResourcePattern: "io.github.example/*"},
				},
			},
			setupRegistryService: func(_ service.RegistryService) {
				// Empty registry - no setup needed
			},
			expectedStatus: http.StatusForbidden,
			expectedError:  "You do not have permission to publish this server",
		},
		{
			name: "registry service error",
			requestBody: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "example/test-server",
				Description: "A test server",
				Version:     "1.0.0",
				Repository: model.Repository{
					URL:    "https://github.com/example/test-server",
					Source: "github",
					ID:     "example/test-server",
				},
			},
			tokenClaims: &auth.JWTClaims{
				AuthMethod: auth.MethodNone,
				Permissions: []auth.Permission{
					{Action: auth.PermissionActionPublish, ResourcePattern: "*"},
				},
			},
			setupRegistryService: func(registry service.RegistryService) {
				// Pre-publish the same server to cause duplicate version error
				existingServer := apiv0.ServerJSON{
					Schema:      model.CurrentSchemaURL,
					Name:        "example/test-server",
					Description: "Existing test server",
					Version:     "1.0.0",
					Repository: model.Repository{
						URL:    "https://github.com/example/test-server-existing",
						Source: "github",
						ID:     "example/test-server-existing",
					},
				}
				_, _ = registry.CreateServer(context.Background(), &existingServer)
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid version: cannot publish duplicate version",
		},
		{
			name: "package validation success - MCPB package",
			requestBody: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server-mcpb",
				Description: "A test server with MCPB package",
				Version:     "1.0.0",
				Packages: []model.Package{
					{
						RegistryType: model.RegistryTypeMCPB,
						Identifier:   "https://github.com/example/server/releases/download/v1.0.0/server.tar.gz",
						Version:      "1.0.0",
						FileSHA256:   "fe333e598595000ae021bd27117db32ec69af6987f507ba7a63c90638ff633ce",
						Transport: model.Transport{
							Type: model.TransportTypeStdio,
						},
					},
				},
			},
			tokenClaims: &auth.JWTClaims{
				AuthMethod: auth.MethodNone,
				Permissions: []auth.Permission{
					{Action: auth.PermissionActionPublish, ResourcePattern: "*"},
				},
			},
			setupRegistryService: func(_ service.RegistryService) {},
			expectedStatus:       http.StatusOK,
		},
		{
			name: "invalid server name - multiple slashes (two slashes)",
			requestBody: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/server/path",
				Description: "Server with multiple slashes in name",
				Version:     "1.0.0",
				Repository: model.Repository{
					URL:    "https://github.com/example/test-server",
					Source: "github",
					ID:     "example/test-server",
				},
			},
			tokenClaims: &auth.JWTClaims{
				AuthMethod: auth.MethodNone,
				Permissions: []auth.Permission{
					{Action: auth.PermissionActionPublish, ResourcePattern: "*"},
				},
			},
			setupRegistryService: func(_ service.RegistryService) {},
			expectedStatus:       http.StatusBadRequest,
			expectedError:        "server name cannot contain multiple slashes",
		},
		{
			name: "invalid server name - multiple slashes (three slashes)",
			requestBody: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "org.company/dept/team/project",
				Description: "Server with three slashes in name",
				Version:     "1.0.0",
			},
			tokenClaims: &auth.JWTClaims{
				AuthMethod: auth.MethodNone,
				Permissions: []auth.Permission{
					{Action: auth.PermissionActionPublish, ResourcePattern: "*"},
				},
			},
			setupRegistryService: func(_ service.RegistryService) {},
			expectedStatus:       http.StatusBadRequest,
			expectedError:        "server name cannot contain multiple slashes",
		},
		{
			name: "invalid server name - consecutive slashes",
			requestBody: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example//double-slash",
				Description: "Server with consecutive slashes",
				Version:     "1.0.0",
			},
			tokenClaims: &auth.JWTClaims{
				AuthMethod: auth.MethodNone,
				Permissions: []auth.Permission{
					{Action: auth.PermissionActionPublish, ResourcePattern: "*"},
				},
			},
			setupRegistryService: func(_ service.RegistryService) {},
			expectedStatus:       http.StatusBadRequest,
			expectedError:        "server name cannot contain multiple slashes",
		},
		{
			name: "invalid server name - URL-like path",
			requestBody: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/servers/v1/api",
				Description: "Server with URL-like path structure",
				Version:     "1.0.0",
			},
			tokenClaims: &auth.JWTClaims{
				AuthMethod: auth.MethodNone,
				Permissions: []auth.Permission{
					{Action: auth.PermissionActionPublish, ResourcePattern: "*"},
				},
			},
			setupRegistryService: func(_ service.RegistryService) {},
			expectedStatus:       http.StatusBadRequest,
			expectedError:        "server name cannot contain multiple slashes",
		},
		{
			name: "invalid server name - many slashes",
			requestBody: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "a/b/c/d/e/f",
				Description: "Server with many slashes",
				Version:     "1.0.0",
			},
			tokenClaims: &auth.JWTClaims{
				AuthMethod: auth.MethodNone,
				Permissions: []auth.Permission{
					{Action: auth.PermissionActionPublish, ResourcePattern: "*"},
				},
			},
			setupRegistryService: func(_ service.RegistryService) {},
			expectedStatus:       http.StatusBadRequest,
			expectedError:        "server name cannot contain multiple slashes",
		},
		{
			name: "invalid server name - with packages and remotes",
			requestBody: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test/server/v2",
				Description: "Complex server with invalid name",
				Version:     "2.0.0",
				Repository: model.Repository{
					URL:    "https://github.com/example/test-server",
					Source: "github",
					ID:     "example/test-server",
				},
				Packages: []model.Package{
					{
						RegistryType: model.RegistryTypeNPM,
						Identifier:   "test-package",
						Version:      "2.0.0",
						Transport: model.Transport{
							Type: model.TransportTypeStdio,
						},
					},
				},
				Remotes: []model.Transport{
					{
						Type: model.TransportTypeStreamableHTTP,
						URL:  "https://example.com/api",
					},
				},
			},
			tokenClaims: &auth.JWTClaims{
				AuthMethod: auth.MethodNone,
				Permissions: []auth.Permission{
					{Action: auth.PermissionActionPublish, ResourcePattern: "*"},
				},
			},
			setupRegistryService: func(_ service.RegistryService) {},
			expectedStatus:       http.StatusBadRequest,
			expectedError:        "server name cannot contain multiple slashes",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create registry service
			registryService := service.NewRegistryService(database.NewTestDB(t), testConfig)

			// Setup registry service
			tc.setupRegistryService(registryService)

			// Create a new ServeMux and Huma API
			mux := http.NewServeMux()
			api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))

			// Register the endpoint with test config
			v0.RegisterPublishEndpoint(api, "/v0", registryService, testConfig)

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

			// No mock expectations to verify
		})
	}
}

// TestPublishEndpoint_MultipleSlashesEdgeCases tests additional edge cases for multi-slash validation
func TestPublishEndpoint_MultipleSlashesEdgeCases(t *testing.T) {
	testSeed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(testSeed)
	require.NoError(t, err)
	testConfig := &config.Config{
		JWTPrivateKey:            hex.EncodeToString(testSeed),
		EnableRegistryValidation: false,
	}

	testCases := []struct {
		name           string
		serverName     string
		expectedStatus int
		description    string
	}{
		{
			name:           "valid - single slash",
			serverName:     "com.example/server",
			expectedStatus: http.StatusOK,
			description:    "Valid server name with single slash should succeed",
		},
		{
			name:           "invalid - trailing slash after valid name",
			serverName:     "com.example/server/",
			expectedStatus: http.StatusBadRequest,
			description:    "Trailing slash creates multiple slashes",
		},
		{
			name:           "invalid - leading and middle slash",
			serverName:     "/com.example/server",
			expectedStatus: http.StatusBadRequest,
			description:    "Leading slash with middle slash",
		},
		{
			name:           "invalid - file system style path",
			serverName:     "usr/local/bin/server",
			expectedStatus: http.StatusBadRequest,
			description:    "File system style paths should be rejected",
		},
		{
			name:           "invalid - version-like suffix",
			serverName:     "com.example/server/v1.0.0",
			expectedStatus: http.StatusBadRequest,
			description:    "Version suffixes with slash should be rejected",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create registry service
			registryService := service.NewRegistryService(database.NewTestDB(t), testConfig)

			// Create a new ServeMux and Huma API
			mux := http.NewServeMux()
			api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))

			// Register the endpoint
			v0.RegisterPublishEndpoint(api, "/v0", registryService, testConfig)

			// Create request body
			requestBody := apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        tc.serverName,
				Description: "Test server",
				Version:     "1.0.0",
			}

			bodyBytes, err := json.Marshal(requestBody)
			require.NoError(t, err)

			// Create request
			req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/v0/publish", bytes.NewBuffer(bodyBytes))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")

			// Set auth header with permissions
			tokenClaims := auth.JWTClaims{
				AuthMethod: auth.MethodNone,
				Permissions: []auth.Permission{
					{Action: auth.PermissionActionPublish, ResourcePattern: "*"},
				},
			}
			token, err := generateTestJWTToken(testConfig, tokenClaims)
			require.NoError(t, err)
			req.Header.Set("Authorization", "Bearer "+token)

			// Perform request
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)

			// Assertions
			assert.Equal(t, tc.expectedStatus, rr.Code,
				"%s: expected status %d, got %d", tc.description, tc.expectedStatus, rr.Code)

			if tc.expectedStatus == http.StatusBadRequest {
				assert.Contains(t, rr.Body.String(), "server name cannot contain multiple slashes",
					"%s: should contain specific error message", tc.description)
			}
		})
	}
}
