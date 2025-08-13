package v0_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/model"
	"github.com/modelcontextprotocol/registry/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestPublishIntegration(t *testing.T) {
	// Setup fake service and auth service
	registryService := service.NewFakeRegistryService()
	authService := &MockAuthService{}
	authService.Mock.On("ValidateAuth", mock.Anything, mock.AnythingOfType("model.Authentication")).Return(true, nil)

	// Create a new ServeMux and Huma API
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	
	// Register the endpoint
	v0.RegisterPublishEndpoint(api, registryService, authService)

	t.Run("successful publish with GitHub auth", func(t *testing.T) {
		publishReq := model.PublishRequest{
			ServerDetail: model.ServerDetail{
				Server: model.Server{
					Name:        "io.github.testuser/test-mcp-server",
					Description: "A test MCP server for integration testing",
					Repository: model.Repository{
						URL:    "https://github.com/testuser/test-mcp-server",
						Source: "github",
						ID:     "testuser/test-mcp-server",
					},
					VersionDetail: model.VersionDetail{
						Version: "1.0.0",
					},
				},
				Packages: []model.Package{
					{
						RegistryName: "npm",
						Name:         "test-mcp-server",
						Version:      "1.0.0",
						RunTimeHint:  "node",
						RuntimeArguments: []model.Argument{
							{
								Type: model.ArgumentTypeNamed,
								Name: "config",
								InputWithVariables: model.InputWithVariables{
									Input: model.Input{
										Description: "Configuration file path",
										Format:      model.FormatFilePath,
										IsRequired:  true,
									},
								},
							},
						},
					},
				},
				Remotes: []model.Remote{
					{
						TransportType: "http",
						URL:           "http://localhost:3000/mcp",
					},
				},
			},
		}

		// Marshal the server detail to JSON
		jsonData, err := json.Marshal(publishReq)
		require.NoError(t, err)

		// Create a request
		req := httptest.NewRequest(http.MethodPost, "/v0/publish", bytes.NewBuffer(jsonData))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer github_test_token_123")

		// Create a response recorder
		recorder := httptest.NewRecorder()

		// Call the handler through the mux
		mux.ServeHTTP(recorder, req)

		// Check the response
		assert.Equal(t, http.StatusOK, recorder.Code)

		var response model.Server
		err = json.Unmarshal(recorder.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.NotEmpty(t, response.ID, "Server ID should be generated")
		assert.Equal(t, publishReq.Name, response.Name)
		assert.Equal(t, publishReq.Description, response.Description)

		// Verify the server was actually published by retrieving it
		publishedServer, err := registryService.GetByID(response.ID)
		require.NoError(t, err)
		assert.Equal(t, publishReq.Name, publishedServer.Name)
		assert.Equal(t, publishReq.Description, publishedServer.Description)
		assert.Equal(t, publishReq.VersionDetail.Version, publishedServer.VersionDetail.Version)
		assert.Len(t, publishedServer.Packages, 1)
		assert.Len(t, publishedServer.Remotes, 1)
	})

	t.Run("successful publish without auth (no prefix)", func(t *testing.T) {
		publishReq := model.PublishRequest{
			ServerDetail: model.ServerDetail{
				Server: model.Server{
					Name:        "custom-mcp-server",
					Description: "A custom MCP server without auth",
					Repository: model.Repository{
						URL:    "https://example.com/custom-server",
						Source: "custom",
						ID:     "custom/custom-server",
					},
					VersionDetail: model.VersionDetail{
						Version: "2.0.0",
					},
				},
			},
		}

		jsonData, err := json.Marshal(publishReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/v0/publish", bytes.NewBuffer(jsonData))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer dummy_token")

		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, req)

		assert.Equal(t, http.StatusOK, recorder.Code)

		var response model.Server
		err = json.Unmarshal(recorder.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.NotEmpty(t, response.ID, "Server ID should be generated")
	})

	t.Run("publish fails with missing name", func(t *testing.T) {
		publishReq := model.PublishRequest{
			ServerDetail: model.ServerDetail{
				Server: model.Server{
					Name:        "", // Missing name
					Description: "A test server",
					VersionDetail: model.VersionDetail{
						Version: "1.0.0",
					},
				},
			},
		}

		jsonData, err := json.Marshal(publishReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/v0/publish", bytes.NewBuffer(jsonData))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer token")

		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, req)

		assert.Equal(t, http.StatusBadRequest, recorder.Code)
		assert.Contains(t, recorder.Body.String(), "Name is required")
	})

	t.Run("publish fails with missing version", func(t *testing.T) {
		publishReq := model.PublishRequest{
			ServerDetail: model.ServerDetail{
				Server: model.Server{
					Name:        "test-server",
					Description: "A test server",
					VersionDetail: model.VersionDetail{
						Version: "", // Missing version
					},
				},
			},
		}

		jsonData, err := json.Marshal(publishReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/v0/publish", bytes.NewBuffer(jsonData))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer token")

		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, req)

		assert.Equal(t, http.StatusBadRequest, recorder.Code)
		assert.Contains(t, recorder.Body.String(), "Version is required")
	})

	t.Run("publish fails with missing authorization header", func(t *testing.T) {
		publishReq := model.PublishRequest{
			ServerDetail: model.ServerDetail{
				Server: model.Server{
					Name:        "test-server",
					Description: "A test server",
					VersionDetail: model.VersionDetail{
						Version: "1.0.0",
					},
				},
			},
		}

		jsonData, err := json.Marshal(publishReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/v0/publish", bytes.NewBuffer(jsonData))
		req.Header.Set("Content-Type", "application/json")
		// No Authorization header

		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, req)

		assert.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
		assert.Contains(t, recorder.Body.String(), "required header parameter is missing")
	})

	t.Run("publish fails with invalid JSON", func(t *testing.T) {
		invalidJSON := `{"name": "test", "version": `

		req := httptest.NewRequest(http.MethodPost, "/v0/publish", bytes.NewBufferString(invalidJSON))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer token")

		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, req)

		assert.Equal(t, http.StatusBadRequest, recorder.Code)
		assert.Contains(t, recorder.Body.String(), "unexpected end of JSON")
	})

	t.Run("publish fails with unsupported HTTP method", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v0/publish", nil)
		req.Header.Set("Authorization", "Bearer token")

		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, req)

		assert.Equal(t, http.StatusMethodNotAllowed, recorder.Code)
		assert.Contains(t, recorder.Body.String(), "Method Not Allowed")
	})

	t.Run("publish fails with duplicate name and version", func(t *testing.T) {
		// First, publish a server successfully
		firstServerDetail := model.PublishRequest{
			ServerDetail: model.ServerDetail{
				Server: model.Server{
					Name:        "io.github.duplicate/test-server",
					Description: "First server for duplicate test",
					Repository: model.Repository{
						URL:    "https://github.com/duplicate/test-server",
						Source: "github",
						ID:     "duplicate/test-server",
					},
					VersionDetail: model.VersionDetail{
						Version: "1.0.0",
					},
				},
			},
		}

		jsonData, err := json.Marshal(firstServerDetail)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/v0/publish", bytes.NewBuffer(jsonData))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer github_token_first")

		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, req)

		assert.Equal(t, http.StatusOK, recorder.Code, "First publish should succeed")

		var response model.Server
		err = json.Unmarshal(recorder.Body.Bytes(), &response)
		require.NoError(t, err)

		firstServerID := response.ID // Store the ID for later verification

		// Now try to publish another server with the same name and version
		duplicateServerDetail := model.PublishRequest{
			ServerDetail: model.ServerDetail{
				Server: model.Server{
					Name:        "io.github.duplicate/test-server", // Same name
					Description: "Duplicate server attempt",
					Repository: model.Repository{
						URL:    "https://github.com/duplicate/test-server-fork",
						Source: "github",
						ID:     "duplicate/test-server-fork",
					},
					VersionDetail: model.VersionDetail{
						Version: "1.0.0", // Same version
					},
				},
			},
		}

		duplicateJSONData, err := json.Marshal(duplicateServerDetail)
		require.NoError(t, err)

		duplicateReq := httptest.NewRequest(http.MethodPost, "/v0/publish", bytes.NewBuffer(duplicateJSONData))
		duplicateReq.Header.Set("Content-Type", "application/json")
		duplicateReq.Header.Set("Authorization", "Bearer github_token_duplicate")

		duplicateRecorder := httptest.NewRecorder()
		mux.ServeHTTP(duplicateRecorder, duplicateReq)

		// The duplicate should fail
		assert.Equal(t, http.StatusInternalServerError, duplicateRecorder.Code)
		assert.Contains(t, duplicateRecorder.Body.String(), "Failed to publish server")

		// Verify that only the first server was actually stored
		retrievedServer, err := registryService.GetByID(firstServerID)
		require.NoError(t, err)
		assert.Equal(t, firstServerDetail.Name, retrievedServer.Name)
		assert.Equal(t, firstServerDetail.Description, retrievedServer.Description)
	})

	t.Run("publish succeeds with same name but different version", func(t *testing.T) {
		// Publish first version
		firstVersionDetail := model.PublishRequest{
			ServerDetail: model.ServerDetail{
				Server: model.Server{
					Name:        "io.github.versioned/test-server",
					Description: "First version of the server",
					Repository: model.Repository{
						URL:    "https://github.com/versioned/test-server",
						Source: "github",
						ID:     "versioned/test-server",
					},
					VersionDetail: model.VersionDetail{
						Version: "1.0.0",
					},
				},
			},
		}

		jsonData, err := json.Marshal(firstVersionDetail)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/v0/publish", bytes.NewBuffer(jsonData))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer github_token_v1")

		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, req)

		assert.Equal(t, http.StatusOK, recorder.Code, "First version should succeed")

		var response model.Server
		err = json.Unmarshal(recorder.Body.Bytes(), &response)
		require.NoError(t, err)
		firstVersionID := response.ID // Store the ID for later verification
		require.NotEmpty(t, firstVersionID, "Server ID should be generated")

		// Publish second version with same name but different version
		secondVersionDetail := model.PublishRequest{
			ServerDetail: model.ServerDetail{
				Server: model.Server{
					Name:        "io.github.versioned/test-server", // Same name
					Description: "Second version of the server",
					Repository: model.Repository{
						URL:    "https://github.com/versioned/test-server",
						Source: "github",
						ID:     "versioned/test-server",
					},
					VersionDetail: model.VersionDetail{
						Version: "2.0.0", // Different version
					},
				},
			},
		}

		secondJSONData, err := json.Marshal(secondVersionDetail)
		require.NoError(t, err)

		secondReq := httptest.NewRequest(http.MethodPost, "/v0/publish", bytes.NewBuffer(secondJSONData))
		secondReq.Header.Set("Content-Type", "application/json")
		secondReq.Header.Set("Authorization", "Bearer github_token_v2")

		secondRecorder := httptest.NewRecorder()
		mux.ServeHTTP(secondRecorder, secondReq)

		// The second version should succeed
		assert.Equal(t, http.StatusOK, secondRecorder.Code)

		var secondResponse model.Server
		err = json.Unmarshal(secondRecorder.Body.Bytes(), &secondResponse)
		require.NoError(t, err)
		secondVersionID := secondResponse.ID // Store the ID for later verification
		require.NotEmpty(t, secondVersionID, "Server ID for second version should be generated")

		// Verify both versions exist
		firstRetrieved, err := registryService.GetByID(firstVersionID)
		require.NoError(t, err)
		assert.Equal(t, "1.0.0", firstRetrieved.VersionDetail.Version)

		secondRetrieved, err := registryService.GetByID(secondVersionID)
		require.NoError(t, err)
		assert.Equal(t, "2.0.0", secondRetrieved.VersionDetail.Version)
	})

	t.Run("publish fails when trying to publish older version after newer version", func(t *testing.T) {
		// First, publish a newer version (2.0.0)
		newerVersionDetail := model.PublishRequest{
			ServerDetail: model.ServerDetail{
				Server: model.Server{
					Name:        "io.github.versioning/version-order-test",
					Description: "Newer version published first",
					Repository: model.Repository{
						URL:    "https://github.com/versioning/version-order-test",
						Source: "github",
						ID:     "versioning/version-order-test",
					},
					VersionDetail: model.VersionDetail{
						Version: "2.0.0",
					},
				},
			},
		}

		jsonData, err := json.Marshal(newerVersionDetail)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/v0/publish", bytes.NewBuffer(jsonData))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer github_token_newer")

		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, req)

		assert.Equal(t, http.StatusOK, recorder.Code, "Newer version should be published successfully")

		var response model.Server
		err = json.Unmarshal(recorder.Body.Bytes(), &response)
		require.NoError(t, err)
		newerVersionID := response.ID // Store the ID for later verification
		require.NotEmpty(t, newerVersionID, "Server ID for newer version should be generated")

		// Now try to publish an older version (1.0.0) of the same package
		olderVersionDetail := model.PublishRequest{
			ServerDetail: model.ServerDetail{
				Server: model.Server{
					Name:        "io.github.versioning/version-order-test", // Same name
					Description: "Older version published after newer",
					Repository: model.Repository{
						URL:    "https://github.com/versioning/version-order-test",
						Source: "github",
						ID:     "versioning/version-order-test",
					},
					VersionDetail: model.VersionDetail{
						Version: "1.0.0", // Older version
					},
				},
			},
		}

		olderJSONData, err := json.Marshal(olderVersionDetail)
		require.NoError(t, err)

		olderReq := httptest.NewRequest(http.MethodPost, "/v0/publish", bytes.NewBuffer(olderJSONData))
		olderReq.Header.Set("Content-Type", "application/json")
		olderReq.Header.Set("Authorization", "Bearer github_token_older")

		olderRecorder := httptest.NewRecorder()
		mux.ServeHTTP(olderRecorder, olderReq)

		// This should fail - we shouldn't allow publishing older versions after newer ones
		assert.Equal(t, http.StatusInternalServerError, olderRecorder.Code, "Publishing older version should fail")
		assert.Contains(t, olderRecorder.Body.String(), "Failed to publish server", "Error message should mention version")

		// Verify that only the newer version exists
		newerRetrieved, err := registryService.GetByID(newerVersionID)
		require.NoError(t, err)
		assert.Equal(t, "2.0.0", newerRetrieved.VersionDetail.Version)
	})
}

func TestPublishIntegrationEndToEnd(t *testing.T) {
	registryService := service.NewFakeRegistryService()
	authService := &MockAuthService{}
	authService.Mock.On("ValidateAuth", mock.Anything, mock.AnythingOfType("model.Authentication")).Return(true, nil)

	// Create a new ServeMux and Huma API
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	
	// Register the endpoint
	v0.RegisterPublishEndpoint(api, registryService, authService)

	t.Run("end-to-end publish and retrieve flow", func(t *testing.T) {
		// Step 1: Get initial count of servers
		initialServers, _, err := registryService.List("", 100)
		require.NoError(t, err)
		initialCount := len(initialServers)

		// Step 2: Publish a new server
		publishReq := model.PublishRequest{
			ServerDetail: model.ServerDetail{
				Server: model.Server{
					Name:        "io.github.e2e/end-to-end-server",
					Description: "End-to-end test server",
					Repository: model.Repository{
						URL:    "https://github.com/e2e/end-to-end-server",
						Source: "github",
						ID:     "e2e/end-to-end-server",
					},
					VersionDetail: model.VersionDetail{
						Version: "1.0.0",
					},
				},
			},
		}

		jsonData, err := json.Marshal(publishReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/v0/publish", bytes.NewBuffer(jsonData))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer github_e2e_token")

		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, req)

		var response model.Server
		err = json.Unmarshal(recorder.Body.Bytes(), &response)
		require.NoError(t, err)
		serverID := response.ID

		require.Equal(t, http.StatusOK, recorder.Code)

		// Step 3: Verify the count increased
		updatedServers, _, err := registryService.List("", 100)
		require.NoError(t, err)
		assert.Equal(t, initialCount+1, len(updatedServers))

		// Step 4: Verify the server can be retrieved by ID
		retrievedServer, err := registryService.GetByID(serverID)
		require.NoError(t, err)
		assert.Equal(t, publishReq.Name, retrievedServer.Name)
		assert.Equal(t, publishReq.Description, retrievedServer.Description)

		// Step 5: Verify the server appears in the list
		found := false
		for _, server := range updatedServers {
			if server.ID == serverID {
				found = true
				assert.Equal(t, publishReq.Name, server.Name)
				break
			}
		}
		assert.True(t, found, "Published server should appear in the list")
	})
}

func TestPublishIntegrationWithComplexPackages(t *testing.T) {
	registryService := service.NewFakeRegistryService()
	authService := &MockAuthService{}
	authService.Mock.On("ValidateAuth", mock.Anything, mock.AnythingOfType("model.Authentication")).Return(true, nil)

	// Create a new ServeMux and Huma API
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	
	// Register the endpoint
	v0.RegisterPublishEndpoint(api, registryService, authService)

	t.Run("publish with complex package configuration", func(t *testing.T) {
		publishReq := model.PublishRequest{
			ServerDetail: model.ServerDetail{
				Server: model.Server{
					Name:        "io.github.complex/advanced-mcp-server",
					Description: "An advanced MCP server with complex configuration",
					Repository: model.Repository{
						URL:    "https://github.com/complex/advanced-mcp-server",
						Source: "github",
						ID:     "complex/advanced-mcp-server",
					},
					VersionDetail: model.VersionDetail{
						Version: "2.1.0",
					},
				},
				Packages: []model.Package{
					{
						RegistryName: "npm",
						Name:         "@example/advanced-mcp-server",
						Version:      "43.1.0",
						RunTimeHint:  "node",
						RuntimeArguments: []model.Argument{
							{
								Type: model.ArgumentTypeNamed,
								Name: "experimental-modules",
							},
							{
								Type: model.ArgumentTypeNamed,
								Name: "config",
								InputWithVariables: model.InputWithVariables{
									Input: model.Input{
										Description: "Main configuration file",
										Format:      model.FormatFilePath,
										IsRequired:  true,
										Default:     "./config.json",
									},
								},
							},
							{
								Type: model.ArgumentTypePositional,
								Name: "mode",
								InputWithVariables: model.InputWithVariables{
									Input: model.Input{
										Description: "Operation mode",
										Format:      model.FormatString,
										IsRequired:  false,
										Default:     "production",
										Choices:     []string{"development", "staging", "production"},
									},
								},
							},
						},
						PackageArguments: []model.Argument{
							{
								Type: model.ArgumentTypeNamed,
								Name: "install-deps",
								InputWithVariables: model.InputWithVariables{
									Input: model.Input{
										Description: "Install dependencies",
										Format:      model.FormatBoolean,
										Default:     "true",
									},
								},
							},
						},
						EnvironmentVariables: []model.KeyValueInput{
							{
								Name: "LOG_LEVEL",
								InputWithVariables: model.InputWithVariables{
									Input: model.Input{
										Description: "Logging level",
										Format:      model.FormatString,
										Default:     "info",
										Choices:     []string{"debug", "info", "warn", "error"},
									},
								},
							},
							{
								Name: "API_KEY",
								InputWithVariables: model.InputWithVariables{
									Input: model.Input{
										Description: "API key for external service",
										Format:      model.FormatString,
										IsRequired:  true,
										IsSecret:    true,
									},
								},
							},
						},
					},
				},
				Remotes: []model.Remote{
					{
						TransportType: "http",
						URL:           "http://localhost:8080/mcp",
						Headers: []model.KeyValueInput{
							{
								Name: "API-Version",
								InputWithVariables: model.InputWithVariables{
									Input: model.Input{
										Description: "API Version Header",
										Format:      model.FormatString,
										Value:       "v1",
									},
								},
							},
						},
					},
				},
			},
		}

		jsonData, err := json.Marshal(publishReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/v0/publish", bytes.NewBuffer(jsonData))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer github_complex_token")

		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, req)

		assert.Equal(t, http.StatusOK, recorder.Code)

		var response model.Server
		err = json.Unmarshal(recorder.Body.Bytes(), &response)
		require.NoError(t, err)

		serverID := response.ID
		assert.NotEmpty(t, serverID, "Server ID should be generated")

		// Verify the complex server was published correctly
		publishedServer, err := registryService.GetByID(serverID)
		require.NoError(t, err)

		// Verify package details
		require.Len(t, publishedServer.Packages, 1)
		pkg := publishedServer.Packages[0]
		assert.Equal(t, "npm", pkg.RegistryName)
		assert.Equal(t, "@example/advanced-mcp-server", pkg.Name)
		assert.Len(t, pkg.RuntimeArguments, 3)
		assert.Len(t, pkg.PackageArguments, 1)
		assert.Len(t, pkg.EnvironmentVariables, 2)

		// Verify remotes
		require.Len(t, publishedServer.Remotes, 1)
		assert.Equal(t, "http", publishedServer.Remotes[0].TransportType)
		assert.Len(t, publishedServer.Remotes[0].Headers, 1)
	})
}