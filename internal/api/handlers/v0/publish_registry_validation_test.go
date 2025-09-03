package v0_test

import (
	"bytes"
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

func TestPublishRegistryValidation(t *testing.T) {
	// Create test config with validation ENABLED
	testSeed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(testSeed)
	require.NoError(t, err)
	testConfig := &config.Config{
		JWTPrivateKey:            hex.EncodeToString(testSeed),
		EnableRegistryValidation: true, // Enable validation for this test
	}

	// Setup fake service
	registryService := service.NewRegistryService(database.NewMemoryDB(), testConfig)

	// Create a new ServeMux and Huma API
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))

	// Register the endpoint
	v0.RegisterPublishEndpoint(api, registryService, testConfig)

	t.Run("publish fails with npm registry validation error", func(t *testing.T) {
		publishReq := apiv0.ServerJSON{
			Name:        "com.example/test-server-with-npm",
			Description: "A test server with invalid npm package reference",
			VersionDetail: model.VersionDetail{
				Version: "1.0.0",
			},
			Packages: []model.Package{
				{
					RegistryType: model.RegistryTypeNPM,
					Identifier:   "definitely-nonexistent-npm-package-xyz123",
					Version:      "1.0.0",
				},
			},
		}

		// Generate valid JWT token with wildcard permission
		claims := auth.JWTClaims{
			AuthMethod: auth.MethodNone,
			Permissions: []auth.Permission{
				{Action: auth.PermissionActionPublish, ResourcePattern: "*"},
			},
		}
		token, err := generateTestJWTToken(testConfig, claims)
		require.NoError(t, err)

		body, err := json.Marshal(publishReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/v0/publish", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)

		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusBadRequest, rr.Code)
		assert.Contains(t, rr.Body.String(), "registry validation failed")
	})

	t.Run("publish succeeds with MCPB package (registry validation enabled)", func(t *testing.T) {
		publishReq := apiv0.ServerJSON{
			Name:        "com.example/test-server-mcpb-validation",
			Description: "A test server with MCPB package and registry validation enabled",
			VersionDetail: model.VersionDetail{
				Version: "1.0.0",
			},
			Packages: []model.Package{
				{
					RegistryType: model.RegistryTypeMCPB,
					Identifier:   "https://github.com/example/server/releases/download/v1.0.0/server.tar.gz",
				},
			},
		}

		// Generate valid JWT token with wildcard permission
		claims := auth.JWTClaims{
			AuthMethod: auth.MethodNone,
			Permissions: []auth.Permission{
				{Action: auth.PermissionActionPublish, ResourcePattern: "*"},
			},
		}
		token, err := generateTestJWTToken(testConfig, claims)
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
		assert.Len(t, response.Packages, 1)
		assert.Equal(t, model.RegistryTypeMCPB, response.Packages[0].RegistryType)
	})
}
