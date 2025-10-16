package api_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/api/router"
	"github.com/modelcontextprotocol/registry/internal/config"
)

// OpenAPISpec represents the minimal structure we need to compare paths
type OpenAPISpec struct {
	Paths map[string]interface{} `yaml:"paths"`
}

func TestOpenAPIEndpointCompliance(t *testing.T) {
	// Load reference schema from docs
	referenceSchemaPath := filepath.Join("..", "..", "docs", "reference", "api", "openapi.yaml")
	referenceData, err := os.ReadFile(referenceSchemaPath)
	require.NoError(t, err, "Failed to read reference OpenAPI schema at %s", referenceSchemaPath)

	var referenceSpec OpenAPISpec
	err = yaml.Unmarshal(referenceData, &referenceSpec)
	require.NoError(t, err, "Failed to parse reference OpenAPI schema")

	// Create test API using the same pattern as other tests
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))

	// Create minimal config for testing
	cfg := &config.Config{
		JWTPrivateKey: "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20", // 32-byte hex key
	}

	// Create version info for testing
	versionInfo := &v0.VersionBody{
		Version:   "test",
		GitCommit: "test",
		BuildTime: "test",
	}

	// Register V0 routes exactly like production does
	router.RegisterV0Routes(api, cfg, nil, nil, versionInfo) // nil service and metrics for schema testing

	// Get the OpenAPI schema
	req := httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "OpenAPI endpoint should return 200")

	var servedSpec OpenAPISpec
	err = yaml.Unmarshal(w.Body.Bytes(), &servedSpec)
	require.NoError(t, err, "Failed to parse served OpenAPI schema")

	// Extract and sort paths for comparison
	referencePaths := extractAndSortPaths(referenceSpec.Paths)
	servedPaths := extractAndSortPaths(servedSpec.Paths)

	// Find missing paths
	missing := []string{}
	for _, refPath := range referencePaths {
		found := false
		for _, servedPath := range servedPaths {
			if refPath == servedPath {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, refPath)
		}
	}

	// Assert all reference paths are implemented
	assert.Empty(t, missing, "All reference endpoints should be implemented. Missing: %v", missing)

	// Log success and additional endpoints for visibility
	if len(missing) == 0 {
		t.Logf("✅ All %d reference endpoints are implemented", len(referencePaths))

		// Count extra endpoints
		extra := []string{}
		for _, servedPath := range servedPaths {
			found := false
			for _, refPath := range referencePaths {
				if servedPath == refPath {
					found = true
					break
				}
			}
			if !found {
				extra = append(extra, servedPath)
			}
		}

		if len(extra) > 0 {
			t.Logf("+ %d additional endpoints in served API: %v", len(extra), extra)
		}
	}
}

func extractAndSortPaths(paths map[string]interface{}) []string {
	keys := make([]string, 0, len(paths))
	for path := range paths {
		keys = append(keys, path)
	}
	sort.Strings(keys)
	return keys
}
