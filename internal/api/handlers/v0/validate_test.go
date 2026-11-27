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
	"strings"
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

type issueStruct struct {
	Type      string `json:"type"`
	Path      string `json:"path"`
	Message   string `json:"message"`
	Severity  string `json:"severity"`
	Reference string `json:"reference"`
}

func TestValidateEndpoint(t *testing.T) {
	// Create a new ServeMux and Huma API
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))

	// Register the endpoint
	v0.RegisterValidateEndpoint(api, "/v0")

	testCases := []struct {
		name           string
		serverJSON     apiv0.ServerJSON
		expectedValid  bool
		expectedStatus int
		description    string
		validateIssues func(t *testing.T, issues []issueStruct)
	}{
		{
			name: "valid server json",
			serverJSON: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Version:     "1.0.0",
			},
			expectedValid:  true,
			expectedStatus: http.StatusOK,
			description:    "Should return valid for a properly formatted server JSON",
			validateIssues: func(t *testing.T, issues []issueStruct) {
				t.Helper()
				assert.Empty(t, issues, "Valid server JSON should have no issues")
			},
		},
		{
			name: "version range should be invalid",
			serverJSON: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Version:     "^1.0.0", // Version range, not allowed
			},
			expectedValid:  false,
			expectedStatus: http.StatusOK,
			description:    "Should return invalid when version is a range instead of specific version",
			validateIssues: func(t *testing.T, issues []issueStruct) {
				t.Helper()
				require.Greater(t, len(issues), 0, "Should have at least one issue")
				issue := issues[0]
				assert.Equal(t, "semantic", issue.Type, "Issue type should be semantic")
				assert.Equal(t, "version", issue.Path, "Issue path should be 'version'")
				assert.Equal(t, "error", issue.Severity, "Issue severity should be error")
				assert.NotEmpty(t, issue.Message, "Issue message should not be empty")
				assert.Contains(t, issue.Message, "^1.0.0", "Issue message should contain the version range")
				assert.NotEmpty(t, issue.Reference, "Issue reference should not be empty")
			},
		},
		{
			name: "latest version should be invalid",
			serverJSON: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Version:     "latest", // "latest" is reserved
			},
			expectedValid:  false,
			expectedStatus: http.StatusOK,
			description:    "Should return invalid when version is 'latest' (reserved word)",
			validateIssues: func(t *testing.T, issues []issueStruct) {
				t.Helper()
				require.Greater(t, len(issues), 0, "Should have at least one issue")
				issue := issues[0]
				assert.Equal(t, "semantic", issue.Type, "Issue type should be semantic")
				assert.Equal(t, "version", issue.Path, "Issue path should be 'version'")
				assert.Equal(t, "error", issue.Severity, "Issue severity should be error")
				assert.NotEmpty(t, issue.Message, "Issue message should not be empty")
				assert.Contains(t, strings.ToLower(issue.Message), "latest", "Issue message should mention 'latest'")
				assert.NotEmpty(t, issue.Reference, "Issue reference should not be empty")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create request body
			bodyBytes, err := json.Marshal(tc.serverJSON)
			require.NoError(t, err)

			// Create request
			req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/v0/validate", bytes.NewBuffer(bodyBytes))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")

			// Perform request
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)

			// Assert status code
			assert.Equal(t, tc.expectedStatus, rr.Code, "%s: expected status %d, got %d", tc.description, tc.expectedStatus, rr.Code)

			// Parse response - Huma returns ValidationResult directly
			var result struct {
				Valid  bool          `json:"valid"`
				Issues []issueStruct `json:"issues"`
			}
			err = json.Unmarshal(rr.Body.Bytes(), &result)
			require.NoError(t, err, "Failed to parse response: %s", rr.Body.String())

			// Debug output for failed tests
			if tc.expectedValid != result.Valid {
				t.Logf("Response body: %s", rr.Body.String())
				t.Logf("Validation issues: %+v", result.Issues)
			}

			// Always log response for first test case to show structure
			if tc.name == "valid server json" {
				var prettyJSON bytes.Buffer
				if err := json.Indent(&prettyJSON, rr.Body.Bytes(), "", "  "); err == nil {
					t.Logf("Example valid response:\n%s", prettyJSON.String())
				}
			}
			if tc.name == "version range should be invalid" {
				var prettyJSON bytes.Buffer
				if err := json.Indent(&prettyJSON, rr.Body.Bytes(), "", "  "); err == nil {
					t.Logf("Example invalid response:\n%s", prettyJSON.String())
				}
			}

			// Assert validity
			assert.Equal(t, tc.expectedValid, result.Valid, "%s: expected valid=%v, got %v. Issues: %+v", tc.description, tc.expectedValid, result.Valid, result.Issues)

			// Validate issues structure and content
			if tc.validateIssues != nil {
				tc.validateIssues(t, result.Issues)
			} else if !tc.expectedValid {
				// Default validation: if expected invalid, verify there are issues
				assert.Greater(t, len(result.Issues), 0, "%s: expected validation issues but got none. Response: %s", tc.description, rr.Body.String())
				// Validate all issues have required fields
				for i, issue := range result.Issues {
					assert.NotEmpty(t, issue.Type, "Issue %d: type should not be empty", i)
					assert.NotEmpty(t, issue.Severity, "Issue %d: severity should not be empty", i)
					assert.NotEmpty(t, issue.Message, "Issue %d: message should not be empty", i)
				}
			}
		})
	}
}

func TestValidateEndpoint_AllowPrivateRemoteURLs(t *testing.T) {
	privateServer := apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.acme/intranet-mcp",
		Description: "Self-hosted intranet MCP server",
		Version:     "1.0.0",
		Remotes: []model.Transport{
			{
				Type: model.TransportTypeStreamableHTTP,
				URL:  "https://mcp.acme.internal/mcp",
			},
		},
	}
	bodyBytes, err := json.Marshal(privateServer)
	require.NoError(t, err)

	for _, tc := range []struct {
		name      string
		cfg       *config.Config
		wantValid bool
	}{
		{
			name:      "default rejects private remote URL",
			cfg:       &config.Config{AllowPrivateRemoteURLs: false},
			wantValid: false,
		},
		{
			name:      "AllowPrivateRemoteURLs=true accepts private remote URL",
			cfg:       &config.Config{AllowPrivateRemoteURLs: true},
			wantValid: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
			v0.RegisterValidateEndpoint(api, "/v0", tc.cfg)

			req := httptest.NewRequest(http.MethodPost, "/v0/validate", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)

			require.Equal(t, http.StatusOK, rr.Code)
			var res struct {
				Valid bool `json:"valid"`
			}
			require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &res))
			assert.Equal(t, tc.wantValid, res.Valid)
		})
	}
}

func TestPublishAndEditEndpoints_AllowPrivateRemoteURLs(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(seed)
	require.NoError(t, err)
	jwtKey := hex.EncodeToString(seed)

	privateServer := apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.acme/intranet-mcp",
		Description: "Self-hosted intranet MCP server",
		Version:     "1.0.0",
		Remotes: []model.Transport{
			{
				Type: model.TransportTypeStreamableHTTP,
				URL:  "https://mcp.acme.internal/mcp",
			},
		},
	}

	t.Run("default rejects private remote URL on publish", func(t *testing.T) {
		cfg := &config.Config{JWTPrivateKey: jwtKey, EnableRegistryValidation: true, AllowPrivateRemoteURLs: false}
		mux := http.NewServeMux()
		api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
		svc := service.NewRegistryService(database.NewTestDB(t), cfg)
		v0.RegisterPublishEndpoint(api, "/v0", svc, cfg)

		token, err := generateTestJWTToken(cfg, auth.JWTClaims{
			AuthMethod: auth.MethodNone,
			Permissions: []auth.Permission{
				{Action: auth.PermissionActionPublish, ResourcePattern: "*"},
			},
		})
		require.NoError(t, err)

		body, err := json.Marshal(privateServer)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/v0/publish", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)

		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusUnprocessableEntity, rr.Code)
	})

	t.Run("AllowPrivateRemoteURLs=true accepts private remote URL on publish and edit", func(t *testing.T) {
		cfg := &config.Config{JWTPrivateKey: jwtKey, EnableRegistryValidation: true, AllowPrivateRemoteURLs: true}
		mux := http.NewServeMux()
		api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
		svc := service.NewRegistryService(database.NewTestDB(t), cfg)
		v0.RegisterPublishEndpoint(api, "/v0", svc, cfg)
		v0.RegisterEditEndpoints(api, "/v0", svc, cfg)

		token, err := generateTestJWTToken(cfg, auth.JWTClaims{
			AuthMethod: auth.MethodNone,
			Permissions: []auth.Permission{
				{Action: auth.PermissionActionPublish, ResourcePattern: "*"},
				{Action: auth.PermissionActionEdit, ResourcePattern: "*"},
			},
		})
		require.NoError(t, err)

		body, err := json.Marshal(privateServer)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/v0/publish", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code, "%s", rr.Body.String())

		edited := privateServer
		edited.Remotes = []model.Transport{{
			Type: model.TransportTypeStreamableHTTP,
			URL:  "https://10.20.30.40:8443/mcp",
		}}
		editBody, err := json.Marshal(edited)
		require.NoError(t, err)
		editPath := "/v0/servers/" + url.PathEscape(edited.Name) + "/versions/" + url.PathEscape(edited.Version)
		editReq := httptest.NewRequest(http.MethodPut, editPath, bytes.NewReader(editBody))
		editReq.Header.Set("Content-Type", "application/json")
		editReq.Header.Set("Authorization", "Bearer "+token)
		editRR := httptest.NewRecorder()
		mux.ServeHTTP(editRR, editReq)
		require.Equal(t, http.StatusOK, editRR.Code, "%s", editRR.Body.String())
	})
}
