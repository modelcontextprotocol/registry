package validators_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/registry/internal/validators"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestObjectValidator_Validate(t *testing.T) {
	validator := validators.NewObjectValidator()

	tests := []struct {
		name          string
		serverDetail  model.ServerJSON
		expectedError string
	}{
		{
			name: "valid server detail with all fields",
			serverDetail: model.ServerJSON{
				Name:        "test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
					ID:     "owner/repo",
				},
				VersionDetail: model.VersionDetail{
					Version: "1.0.0",
				},
				Packages: []model.Package{
					{
						Identifier:      "test-package",
						RegistryType:    "npm",
						RegistryBaseURL: "https://registry.npmjs.org",
					},
				},
				Remotes: []model.Remote{
					{
						URL: "https://example.com/remote",
					},
				},
			},
			expectedError: "",
		},
		{
			name: "server with invalid repository source",
			serverDetail: model.ServerJSON{
				Name:        "test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://bitbucket.org/owner/repo",
					Source: "bitbucket", // Not in validSources
				},
				VersionDetail: model.VersionDetail{
					Version: "1.0.0",
				},
			},
			expectedError: validators.ErrInvalidRepositoryURL.Error(),
		},
		{
			name: "server with invalid GitHub URL format",
			serverDetail: model.ServerJSON{
				Name:        "test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner", // Missing repo name
					Source: "github",
				},
				VersionDetail: model.VersionDetail{
					Version: "1.0.0",
				},
			},
			expectedError: validators.ErrInvalidRepositoryURL.Error(),
		},
		{
			name: "server with invalid GitLab URL format",
			serverDetail: model.ServerJSON{
				Name:        "test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://gitlab.com", // Missing owner and repo
					Source: "gitlab",
				},
				VersionDetail: model.VersionDetail{
					Version: "1.0.0",
				},
			},
			expectedError: validators.ErrInvalidRepositoryURL.Error(),
		},
		{
			name: "package with spaces in name",
			serverDetail: model.ServerJSON{
				Name:        "test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				VersionDetail: model.VersionDetail{
					Version: "1.0.0",
				},
				Packages: []model.Package{
					{
						Identifier:      "test package with spaces",
						RegistryType:    "npm",
						RegistryBaseURL: "https://registry.npmjs.org",
					},
				},
			},
			expectedError: validators.ErrPackageNameHasSpaces.Error(),
		},
		{
			name: "multiple packages with one invalid",
			serverDetail: model.ServerJSON{
				Name:        "test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				VersionDetail: model.VersionDetail{
					Version: "1.0.0",
				},
				Packages: []model.Package{
					{
						Identifier:      "valid-package",
						RegistryType:    "npm",
						RegistryBaseURL: "https://registry.npmjs.org",
					},
					{
						Identifier:      "invalid package", // Has space
						RegistryType:    "pypi",
						RegistryBaseURL: "https://pypi.org",
					},
				},
			},
			expectedError: validators.ErrPackageNameHasSpaces.Error(),
		},
		{
			name: "remote with invalid URL",
			serverDetail: model.ServerJSON{
				Name:        "test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				VersionDetail: model.VersionDetail{
					Version: "1.0.0",
				},
				Remotes: []model.Remote{
					{
						URL: "not-a-valid-url",
					},
				},
			},
			expectedError: validators.ErrInvalidRemoteURL.Error(),
		},
		{
			name: "remote with missing scheme",
			serverDetail: model.ServerJSON{
				Name:        "test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				VersionDetail: model.VersionDetail{
					Version: "1.0.0",
				},
				Remotes: []model.Remote{
					{
						URL: "example.com/remote",
					},
				},
			},
			expectedError: validators.ErrInvalidRemoteURL.Error(),
		},
		{
			name: "multiple remotes with one invalid",
			serverDetail: model.ServerJSON{
				Name:        "test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				VersionDetail: model.VersionDetail{
					Version: "1.0.0",
				},
				Remotes: []model.Remote{
					{
						URL: "https://valid.com/remote",
					},
					{
						URL: "invalid-url",
					},
				},
			},
			expectedError: validators.ErrInvalidRemoteURL.Error(),
		},
		{
			name: "server detail with nil packages and remotes",
			serverDetail: model.ServerJSON{
				Name:        "test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				VersionDetail: model.VersionDetail{
					Version: "1.0.0",
				},
				Packages: nil,
				Remotes:  nil,
			},
			expectedError: "",
		},
		{
			name: "server detail with empty packages and remotes slices",
			serverDetail: model.ServerJSON{
				Name:        "test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				VersionDetail: model.VersionDetail{
					Version: "1.0.0",
				},
				Packages: []model.Package{},
				Remotes:  []model.Remote{},
			},
			expectedError: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.Validate(&tt.serverDetail)

			if tt.expectedError == "" {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			}
		})
	}
}

func TestExtractPublisherExtensions(t *testing.T) {
	tests := []struct {
		name     string
		request  apiv0.PublishRequest
		expected map[string]interface{}
	}{
		{
			name: "nil publisher extensions",
			request: apiv0.PublishRequest{
				Server: model.ServerJSON{Name: "test"},
			},
			expected: map[string]interface{}{},
		},
		{
			name: "empty publisher extensions",
			request: apiv0.PublishRequest{
				Server:     model.ServerJSON{Name: "test"},
				XPublisher: map[string]interface{}{},
			},
			expected: map[string]interface{}{},
		},
		{
			name: "simple publisher extensions",
			request: apiv0.PublishRequest{
				Server: model.ServerJSON{Name: "test"},
				XPublisher: map[string]interface{}{
					"build_info": map[string]interface{}{
						"version": "1.2.3",
						"commit":  "abc123",
					},
					"publisher_metadata": "test-publisher",
				},
			},
			expected: map[string]interface{}{
				"build_info": map[string]interface{}{
					"version": "1.2.3",
					"commit":  "abc123",
				},
				"publisher_metadata": "test-publisher",
			},
		},
		{
			name: "nested publisher extensions",
			request: apiv0.PublishRequest{
				Server: model.ServerJSON{Name: "test"},
				XPublisher: map[string]interface{}{
					"ci": map[string]interface{}{
						"pipeline": map[string]interface{}{
							"id":     "12345",
							"branch": "main",
						},
						"artifacts": []string{"binary", "docs"},
					},
				},
			},
			expected: map[string]interface{}{
				"ci": map[string]interface{}{
					"pipeline": map[string]interface{}{
						"id":     "12345",
						"branch": "main",
					},
					"artifacts": []string{"binary", "docs"},
				},
			},
		},
		{
			name: "nil publisher extensions (should be ignored)",
			request: apiv0.PublishRequest{
				Server:     model.ServerJSON{Name: "test"},
				XPublisher: nil,
			},
			expected: map[string]interface{}{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validators.ExtractPublisherExtensions(tt.request)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractPublisherExtensions_DoesNotDoubleNest(t *testing.T) {
	// This test specifically verifies the bug we fixed - no double nesting
	request := apiv0.PublishRequest{
		Server: model.ServerJSON{Name: "test"},
		XPublisher: map[string]interface{}{
			"tool":    "publisher-cli",
			"version": "1.0.0",
		},
	}

	result := validators.ExtractPublisherExtensions(request)

	// Verify we get the data directly, not wrapped in another "x-publisher" key
	assert.Equal(t, "publisher-cli", result["tool"])
	assert.Equal(t, "1.0.0", result["version"])

	// Verify we don't have double nesting
	assert.NotContains(t, result, "x-publisher")
}

func TestServerResponse_JSONSerialization(t *testing.T) {
	// Test that ServerResponse properly serializes to extension wrapper format
	publishedTime := time.Date(2023, 12, 1, 10, 30, 0, 0, time.UTC)

	response := apiv0.ServerRecord{
		Server: model.ServerJSON{
			Name:        "test-server",
			Description: "A test server",
			Repository: model.Repository{
				URL:    "https://github.com/test/server",
				Source: "github",
				ID:     "test/server",
			},
			VersionDetail: model.VersionDetail{
				Version: "1.0.0",
			},
		},
		XIOModelContextProtocolRegistry: apiv0.RegistryExtensions{
			ID:          "registry-id-123",
			PublishedAt: publishedTime,
			IsLatest:    true,
			ReleaseDate: publishedTime.Format(time.RFC3339),
		},
		XPublisher: map[string]interface{}{
			"build_tool": "ci-pipeline",
			"commit":     "abc123def",
		},
	}

	// Serialize to JSON
	jsonData, err := json.Marshal(response)
	require.NoError(t, err)

	// Parse back to verify structure
	var parsed map[string]interface{}
	err = json.Unmarshal(jsonData, &parsed)
	require.NoError(t, err)

	// Verify three-layer structure exists
	assert.Contains(t, parsed, "server")
	assert.Contains(t, parsed, "x-io.modelcontextprotocol.registry")
	assert.Contains(t, parsed, "x-publisher")

	// Verify server data
	serverData := parsed["server"].(map[string]interface{})
	assert.Equal(t, "test-server", serverData["name"])

	// Verify registry metadata
	registryData := parsed["x-io.modelcontextprotocol.registry"].(map[string]interface{})
	assert.Equal(t, "registry-id-123", registryData["id"])

	// Verify publisher extensions
	publisherData := parsed["x-publisher"].(map[string]interface{})
	assert.Equal(t, "ci-pipeline", publisherData["build_tool"])
	assert.Equal(t, "abc123def", publisherData["commit"])
}

func TestPublishRequest_WithPublisherExtensions(t *testing.T) {
	// Test that PublishRequest properly deserializes publisher extensions
	jsonData := `{
		"server": {
			"name": "test-server",
			"description": "Test server",
			"version_detail": {
				"version": "1.0.0"
			}
		},
		"x-publisher": {
			"tool": "publisher-cli",
			"metadata": {
				"build_date": "2023-12-01",
				"commit": "abc123"
			}
		}
	}`

	var request apiv0.PublishRequest
	err := json.Unmarshal([]byte(jsonData), &request)
	require.NoError(t, err)

	// Verify server data
	assert.Equal(t, "test-server", request.Server.Name)
	assert.Equal(t, "Test server", request.Server.Description)

	// Verify publisher extensions
	require.NotNil(t, request.XPublisher)
	assert.Equal(t, "publisher-cli", request.XPublisher["tool"])

	metadata := request.XPublisher["metadata"].(map[string]interface{})
	assert.Equal(t, "2023-12-01", metadata["build_date"])
	assert.Equal(t, "abc123", metadata["commit"])
}

func TestServerResponse_EmptyExtensions(t *testing.T) {
	// Test behavior with empty/nil extensions
	response := apiv0.ServerRecord{
		Server: model.ServerJSON{
			Name:        "minimal-server",
			Description: "Minimal test",
		},
		XIOModelContextProtocolRegistry: apiv0.RegistryExtensions{
			ID: "min-id",
		},
		XPublisher: nil,
	}

	jsonData, err := json.Marshal(response)
	require.NoError(t, err)

	var parsed map[string]interface{}
	err = json.Unmarshal(jsonData, &parsed)
	require.NoError(t, err)

	// Should still have the structure, even with nil publisher extensions
	assert.Contains(t, parsed, "server")
	assert.Contains(t, parsed, "x-io.modelcontextprotocol.registry")

	// x-publisher should be null/nil in JSON when empty
	publisherValue, exists := parsed["x-publisher"]
	if exists {
		assert.Nil(t, publisherValue)
	}
}

func TestValidateRemoteNamespaceMatch(t *testing.T) {
	tests := []struct {
		name         string
		serverDetail model.ServerJSON
		expectError  bool
		errorMsg     string
	}{
		{
			name: "valid match - example.com domain",
			serverDetail: model.ServerJSON{
				Name: "com.example/test-server",
				Remotes: []model.Remote{
					{URL: "https://example.com/mcp"},
				},
			},
			expectError: false,
		},
		{
			name: "valid match - subdomain mcp.example.com",
			serverDetail: model.ServerJSON{
				Name: "com.example/test-server",
				Remotes: []model.Remote{
					{URL: "https://mcp.example.com/endpoint"},
				},
			},
			expectError: false,
		},
		{
			name: "valid match - api subdomain",
			serverDetail: model.ServerJSON{
				Name: "com.example/api-server",
				Remotes: []model.Remote{
					{URL: "https://api.example.com/mcp"},
				},
			},
			expectError: false,
		},
		{
			name: "invalid - wrong domain",
			serverDetail: model.ServerJSON{
				Name: "com.example/test-server",
				Remotes: []model.Remote{
					{URL: "https://google.com/mcp"},
				},
			},
			expectError: true,
			errorMsg:    "remote URL host google.com does not match publisher domain example.com",
		},
		{
			name: "invalid - different domain entirely",
			serverDetail: model.ServerJSON{
				Name: "com.microsoft/server",
				Remotes: []model.Remote{
					{URL: "https://api.github.com/endpoint"},
				},
			},
			expectError: true,
			errorMsg:    "remote URL host api.github.com does not match publisher domain microsoft.com",
		},
		{
			name: "localhost URLs allowed with any namespace",
			serverDetail: model.ServerJSON{
				Name: "com.example/test-server",
				Remotes: []model.Remote{
					{URL: "http://localhost:3000/sse"},
				},
			},
			expectError: false,
		},
		{
			name: "invalid URL format",
			serverDetail: model.ServerJSON{
				Name: "com.example/test",
				Remotes: []model.Remote{
					{URL: "not-a-valid-url"},
				},
			},
			expectError: true,
			errorMsg:    "URL must have a valid hostname",
		},
		{
			name: "empty remotes array",
			serverDetail: model.ServerJSON{
				Name:    "com.example/test",
				Remotes: []model.Remote{},
			},
			expectError: false,
		},
		{
			name: "multiple valid remotes - different subdomains",
			serverDetail: model.ServerJSON{
				Name: "com.example/server",
				Remotes: []model.Remote{
					{URL: "https://api.example.com/sse"},
					{URL: "https://mcp.example.com/websocket"},
				},
			},
			expectError: false,
		},
		{
			name: "one valid, one invalid remote",
			serverDetail: model.ServerJSON{
				Name: "com.example/server",
				Remotes: []model.Remote{
					{URL: "https://example.com/sse"},
					{URL: "https://google.com/websocket"},
				},
			},
			expectError: true,
			errorMsg:    "remote URL host google.com does not match publisher domain example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validators.ValidateRemoteNamespaceMatch(tt.serverDetail)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestParseServerName(t *testing.T) {
	tests := []struct {
		name         string
		serverDetail model.ServerJSON
		expectError  bool
		errorMsg     string
	}{
		{
			name: "valid namespace/name format",
			serverDetail: model.ServerJSON{
				Name: "com.example.api/server",
			},
			expectError: false,
		},
		{
			name: "valid complex namespace",
			serverDetail: model.ServerJSON{
				Name: "com.github.microsoft.azure/webapp-server",
			},
			expectError: false,
		},
		{
			name: "empty server name",
			serverDetail: model.ServerJSON{
				Name: "",
			},
			expectError: true,
			errorMsg:    "server name is required",
		},
		{
			name: "missing slash separator",
			serverDetail: model.ServerJSON{
				Name: "com.example.server",
			},
			expectError: true,
			errorMsg:    "server name must be in format 'dns-namespace/name'",
		},
		{
			name: "empty namespace part",
			serverDetail: model.ServerJSON{
				Name: "/server-name",
			},
			expectError: true,
			errorMsg:    "non-empty namespace and name parts",
		},
		{
			name: "empty name part",
			serverDetail: model.ServerJSON{
				Name: "com.example/",
			},
			expectError: true,
			errorMsg:    "non-empty namespace and name parts",
		},
		{
			name: "multiple slashes - uses first as separator",
			serverDetail: model.ServerJSON{
				Name: "com.example/server/path",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validators.ParseServerName(tt.serverDetail)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
