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
func generateIntegrationTestJWTToken(cfg *config.Config, claims auth.JWTClaims) (string, error) {
	jwtManager := auth.NewJWTManager(cfg)
	ctx := context.Background()
	tokenResponse, err := jwtManager.GenerateTokenResponse(ctx, claims)
	if err != nil {
		return "", err
	}
	return tokenResponse.RegistryToken, nil
}

func TestPublishIntegration(t *testing.T) {
	// Setup fake service
	registryService := service.NewRegistryServiceWithDB(database.NewMemoryDB())

	// Create test config with a valid Ed25519 seed
	testSeed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(testSeed)
	require.NoError(t, err)
	testConfig := &config.Config{
		JWTPrivateKey: hex.EncodeToString(testSeed),
	}

	// Create a new ServeMux and Huma API
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))

	// Register the endpoint
	v0.RegisterPublishEndpoint(api, registryService, testConfig)

	t.Run("successful publish with GitHub auth", func(t *testing.T) {
		publishReq := apiv0.ServerJSON{
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
		}

		// Generate valid JWT token
		claims := auth.JWTClaims{
			AuthMethod:        auth.MethodGitHubAT,
			AuthMethodSubject: "testuser",
			Permissions: []auth.Permission{
				{Action: auth.PermissionActionPublish, ResourcePattern: "io.github.testuser/*"},
			},
		}
		token, err := generateIntegrationTestJWTToken(testConfig, claims)
		require.NoError(t, err)

		body, err := json.Marshal(publishReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/v0/publish", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)

		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response apiv0.ServerJSON
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, publishReq.Name, response.Name)
		assert.Equal(t, publishReq.VersionDetail.Version, response.VersionDetail.Version)
	})

	t.Run("successful publish with none auth (no prefix)", func(t *testing.T) {
		publishReq := apiv0.ServerJSON{
			Name:        "com.example/test-mcp-server-no-auth",
			Description: "A test MCP server without authentication",
			Repository: model.Repository{
				URL:    "https://github.com/example/test-server",
				Source: "github",
				ID:     "example/test-server",
			},
			VersionDetail: model.VersionDetail{
				Version: "1.0.0",
			},
		}

		// Generate valid JWT token with wildcard permission
		claims := auth.JWTClaims{
			AuthMethod: auth.MethodNone,
			Permissions: []auth.Permission{
				{Action: auth.PermissionActionPublish, ResourcePattern: "*"},
			},
		}
		token, err := generateIntegrationTestJWTToken(testConfig, claims)
		require.NoError(t, err)

		body, err := json.Marshal(publishReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/v0/publish", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)

		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response apiv0.ServerJSON
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, publishReq.Name, response.Name)
	})

	t.Run("publish fails with missing authorization header", func(t *testing.T) {
		publishReq := apiv0.ServerJSON{
			Name: "test-server",
		}

		body, err := json.Marshal(publishReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/v0/publish", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		// No Authorization header

		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusUnprocessableEntity, rr.Code)
		assert.Contains(t, rr.Body.String(), "required header parameter is missing")
	})

	t.Run("publish fails with invalid token", func(t *testing.T) {
		publishReq := apiv0.ServerJSON{
			Name:          "io.github.domdomegg/test-server",
			Description:   "Test server",
			VersionDetail: model.VersionDetail{Version: "1.0.0"},
		}

		body, err := json.Marshal(publishReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/v0/publish", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer invalid-token")

		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusUnauthorized, rr.Code)
		assert.Contains(t, rr.Body.String(), "Invalid or expired Registry JWT token")
	})

	t.Run("publish fails when permission denied", func(t *testing.T) {
		publishReq := apiv0.ServerJSON{
			Name:        "io.github.other/test-server",
			Description: "A test server",
			VersionDetail: model.VersionDetail{
				Version: "1.0.0",
			},
			Repository: model.Repository{
				URL:    "https://github.com/example/test-server",
				Source: "github",
				ID:     "example/test-server",
			},
		}

		// Generate valid JWT token but with different permissions
		claims := auth.JWTClaims{
			AuthMethod: auth.MethodGitHubAT,
			Permissions: []auth.Permission{
				{Action: auth.PermissionActionPublish, ResourcePattern: "io.github.myuser/*"},
			},
		}
		token, err := generateIntegrationTestJWTToken(testConfig, claims)
		require.NoError(t, err)

		body, err := json.Marshal(publishReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/v0/publish", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)

		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusForbidden, rr.Code)
		assert.Contains(t, rr.Body.String(), "You do not have permission to publish this server")
	})
}
