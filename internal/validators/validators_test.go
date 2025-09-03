package validators_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/registry/internal/validators"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name          string
		serverDetail  apiv0.ServerJSON
		expectedError string
	}{
		{
			name: "valid server detail with all fields",
			serverDetail: apiv0.ServerJSON{
				Name:        "com.example/test-server",
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
			serverDetail: apiv0.ServerJSON{
				Name:        "com.example/test-server",
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
			serverDetail: apiv0.ServerJSON{
				Name:        "com.example/test-server",
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
			serverDetail: apiv0.ServerJSON{
				Name:        "com.example/test-server",
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
			serverDetail: apiv0.ServerJSON{
				Name:        "com.example/test-server",
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
			serverDetail: apiv0.ServerJSON{
				Name:        "com.example/test-server",
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
			serverDetail: apiv0.ServerJSON{
				Name:        "com.example/test-server",
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
			serverDetail: apiv0.ServerJSON{
				Name:        "com.example/test-server",
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
			serverDetail: apiv0.ServerJSON{
				Name:        "com.example/test-server",
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
			serverDetail: apiv0.ServerJSON{
				Name:        "com.example/test-server",
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
			serverDetail: apiv0.ServerJSON{
				Name:        "com.example/test-server",
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
			err := validators.ValidateServerJSON(&tt.serverDetail)

			if tt.expectedError == "" {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			}
		})
	}
}

func TestValidate_RemoteNamespaceMatch(t *testing.T) {
	tests := []struct {
		name         string
		serverDetail apiv0.ServerJSON
		expectError  bool
		errorMsg     string
	}{
		{
			name: "valid match - example.com domain",
			serverDetail: apiv0.ServerJSON{
				Name: "com.example/test-server",
				Remotes: []model.Remote{
					{URL: "https://example.com/mcp"},
				},
			},
			expectError: false,
		},
		{
			name: "valid match - subdomain mcp.example.com",
			serverDetail: apiv0.ServerJSON{
				Name: "com.example/test-server",
				Remotes: []model.Remote{
					{URL: "https://mcp.example.com/endpoint"},
				},
			},
			expectError: false,
		},
		{
			name: "valid match - api subdomain",
			serverDetail: apiv0.ServerJSON{
				Name: "com.example/api-server",
				Remotes: []model.Remote{
					{URL: "https://api.example.com/mcp"},
				},
			},
			expectError: false,
		},
		{
			name: "invalid - wrong domain",
			serverDetail: apiv0.ServerJSON{
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
			serverDetail: apiv0.ServerJSON{
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
			serverDetail: apiv0.ServerJSON{
				Name: "com.example/test-server",
				Remotes: []model.Remote{
					{URL: "http://localhost:3000/sse"},
				},
			},
			expectError: false,
		},
		{
			name: "invalid URL format",
			serverDetail: apiv0.ServerJSON{
				Name: "com.example/test",
				Remotes: []model.Remote{
					{URL: "not-a-valid-url"},
				},
			},
			expectError: true,
			errorMsg:    "invalid remote URL",
		},
		{
			name: "empty remotes array",
			serverDetail: apiv0.ServerJSON{
				Name:    "com.example/test",
				Remotes: []model.Remote{},
			},
			expectError: false,
		},
		{
			name: "multiple valid remotes - different subdomains",
			serverDetail: apiv0.ServerJSON{
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
			serverDetail: apiv0.ServerJSON{
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
			err := validators.ValidateServerJSON(&tt.serverDetail)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidate_ServerNameFormat(t *testing.T) {
	tests := []struct {
		name         string
		serverDetail apiv0.ServerJSON
		expectError  bool
		errorMsg     string
	}{
		{
			name: "valid namespace/name format",
			serverDetail: apiv0.ServerJSON{
				Name: "com.example.api/server",
			},
			expectError: false,
		},
		{
			name: "valid complex namespace",
			serverDetail: apiv0.ServerJSON{
				Name: "com.microsoft.azure.service/webapp-server",
			},
			expectError: false,
		},
		{
			name: "empty server name",
			serverDetail: apiv0.ServerJSON{
				Name: "",
			},
			expectError: true,
			errorMsg:    "server name is required",
		},
		{
			name: "missing slash separator",
			serverDetail: apiv0.ServerJSON{
				Name: "com.example.server",
			},
			expectError: true,
			errorMsg:    "server name must be in format 'dns-namespace/name'",
		},
		{
			name: "empty namespace part",
			serverDetail: apiv0.ServerJSON{
				Name: "/server-name",
			},
			expectError: true,
			errorMsg:    "non-empty namespace and name parts",
		},
		{
			name: "empty name part",
			serverDetail: apiv0.ServerJSON{
				Name: "com.example/",
			},
			expectError: true,
			errorMsg:    "non-empty namespace and name parts",
		},
		{
			name: "multiple slashes - uses first as separator",
			serverDetail: apiv0.ServerJSON{
				Name: "com.example/server/path",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validators.ValidateServerJSON(&tt.serverDetail)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateArgument_ValidNamedArguments(t *testing.T) {
	validCases := []model.Argument{
		{
			InputWithVariables: model.InputWithVariables{Input: model.Input{Value: "/path/to/dir"}},
			Type:               model.ArgumentTypeNamed,
			Name:               "--directory",
		},
		{
			InputWithVariables: model.InputWithVariables{Input: model.Input{Default: "8080"}},
			Type:               model.ArgumentTypeNamed,
			Name:               "--port",
		},
		{
			InputWithVariables: model.InputWithVariables{Input: model.Input{Value: "true"}},
			Type:               model.ArgumentTypeNamed,
			Name:               "-v",
		},
		{
			Type: model.ArgumentTypeNamed,
			Name: "-p",
		},
		{
			InputWithVariables: model.InputWithVariables{Input: model.Input{Value: "/etc/config.json"}},
			Type:               model.ArgumentTypeNamed,
			Name:               "config",
		},
		{
			InputWithVariables: model.InputWithVariables{Input: model.Input{Default: "false"}},
			Type:               model.ArgumentTypeNamed,
			Name:               "verbose",
		},
		// No dash prefix requirement as per modification #1
		{
			InputWithVariables: model.InputWithVariables{Input: model.Input{Value: "json"}},
			Type:               model.ArgumentTypeNamed,
			Name:               "output-format",
		},
	}

	for _, arg := range validCases {
		t.Run("Valid_"+arg.Name, func(t *testing.T) {
			server := createValidServerWithArgument(arg)
			err := validators.ValidateServerJSON(&server)
			assert.NoError(t, err, "Expected valid argument %+v", arg)
		})
	}
}

func TestValidateArgument_ValidPositionalArguments(t *testing.T) {
	positionalCases := []model.Argument{
		{Type: model.ArgumentTypePositional, Name: "anything with spaces"},
		{Type: model.ArgumentTypePositional, Name: "anything<with>brackets"},
		{
			InputWithVariables: model.InputWithVariables{Input: model.Input{Value: "--port 8080"}},
			Type:               model.ArgumentTypePositional,
		}, // Can contain flags in value for positional
	}

	for i, arg := range positionalCases {
		t.Run(fmt.Sprintf("ValidPositional_%d", i), func(t *testing.T) {
			server := createValidServerWithArgument(arg)
			err := validators.ValidateServerJSON(&server)
			assert.NoError(t, err, "Expected valid positional argument %+v", arg)
		})
	}
}

func TestValidateArgument_InvalidNamedArgumentNames(t *testing.T) {
	invalidNameCases := []struct {
		name string
		arg  model.Argument
	}{
		{"contains_description", model.Argument{Type: model.ArgumentTypeNamed, Name: "--directory <absolute_path_to_adfin_mcp_folder>"}},
		{"contains_value", model.Argument{Type: model.ArgumentTypeNamed, Name: "--port 8080"}},
		{"contains_dollar", model.Argument{Type: model.ArgumentTypeNamed, Name: "--config $CONFIG_FILE"}},
		{"contains_brackets", model.Argument{Type: model.ArgumentTypeNamed, Name: "--file <path>"}},
		{"empty_name", model.Argument{Type: model.ArgumentTypeNamed, Name: ""}},
		{"has_spaces", model.Argument{Type: model.ArgumentTypeNamed, Name: "name with spaces"}},
	}

	for _, tc := range invalidNameCases {
		t.Run("Invalid_"+tc.name, func(t *testing.T) {
			server := createValidServerWithArgument(tc.arg)
			err := validators.ValidateServerJSON(&server)
			assert.Error(t, err, "Expected error for invalid named argument name: %+v", tc.arg)
		})
	}
}

func TestValidateArgument_InvalidValueFields(t *testing.T) {
	invalidValueCases := []struct {
		name string
		arg  model.Argument
	}{
		{
			"value_starts_with_name",
			model.Argument{
				InputWithVariables: model.InputWithVariables{Input: model.Input{Value: "--port 8080"}},
				Type:               model.ArgumentTypeNamed,
				Name:               "--port",
			},
		},
		{
			"default_starts_with_name",
			model.Argument{
				InputWithVariables: model.InputWithVariables{Input: model.Input{Default: "--config /etc/app.conf"}},
				Type:               model.ArgumentTypeNamed,
				Name:               "--config",
			},
		},
		{
			"value_starts_with_name_complex",
			model.Argument{
				InputWithVariables: model.InputWithVariables{Input: model.Input{Value: "--with-editable $REPOSITORY_DIRECTORY"}},
				Type:               model.ArgumentTypeNamed,
				Name:               "--with-editable",
			},
		},
		{
			"default_starts_with_name_complex",
			model.Argument{
				InputWithVariables: model.InputWithVariables{Input: model.Input{Default: "--with-editable $REPOSITORY_DIRECTORY"}},
				Type:               model.ArgumentTypeNamed,
				Name:               "--with-editable",
			},
		},
	}

	for _, tc := range invalidValueCases {
		t.Run("Invalid_"+tc.name, func(t *testing.T) {
			server := createValidServerWithArgument(tc.arg)
			err := validators.ValidateServerJSON(&server)
			assert.Error(t, err, "Expected error for argument with value starting with name: %+v", tc.arg)
		})
	}
}

func TestValidateArgument_ValidValueFields(t *testing.T) {
	validValueCases := []struct {
		name string
		arg  model.Argument
	}{
		{
			"value_without_name",
			model.Argument{
				InputWithVariables: model.InputWithVariables{Input: model.Input{Value: "8080"}},
				Type:               model.ArgumentTypeNamed,
				Name:               "--port",
			},
		},
		{
			"default_without_name",
			model.Argument{
				InputWithVariables: model.InputWithVariables{Input: model.Input{Default: "/etc/app.conf"}},
				Type:               model.ArgumentTypeNamed,
				Name:               "--config",
			},
		},
		{
			"value_with_var",
			model.Argument{
				InputWithVariables: model.InputWithVariables{Input: model.Input{Value: "$REPOSITORY_DIRECTORY"}},
				Type:               model.ArgumentTypeNamed,
				Name:               "--with-editable",
			},
		},
		{
			"absolute_path",
			model.Argument{
				InputWithVariables: model.InputWithVariables{Input: model.Input{Value: "/absolute/path/to/directory"}},
				Type:               model.ArgumentTypeNamed,
				Name:               "--directory",
			},
		},
		{
			"contains_but_not_starts_with_name",
			model.Argument{
				InputWithVariables: model.InputWithVariables{Input: model.Input{Value: "use --port for configuration"}},
				Type:               model.ArgumentTypeNamed,
				Name:               "--port",
			},
		},
	}

	for _, tc := range validValueCases {
		t.Run("Valid_"+tc.name, func(t *testing.T) {
			server := createValidServerWithArgument(tc.arg)
			err := validators.ValidateServerJSON(&server)
			assert.NoError(t, err, "Expected valid argument %+v", tc.arg)
		})
	}
}

// Helper function to create a valid server with a specific argument for testing
func TestValidate_RegistryTypes(t *testing.T) {
	testCases := []struct {
		name         string
		registryType string
		baseURL      string
		identifier   string
		expectError  bool
	}{
		// Valid registry types (should pass)
		{"valid_npm", model.RegistryTypeNPM, model.RegistryURLNPM, "test-package", false},
		{"valid_pypi", model.RegistryTypePyPI, model.RegistryURLPyPI, "test-package", false},
		{"valid_oci", model.RegistryTypeOCI, model.RegistryURLDocker, "test-package", false},
		{"valid_nuget", model.RegistryTypeNuGet, model.RegistryURLNuGet, "test-package", false},
		{"valid_mcpb_github", model.RegistryTypeMCPB, model.RegistryURLGitHub, "https://github.com/owner/repo/releases/download/v1.0.0/package.mcpb", false},
		{"valid_mcpb_gitlab", model.RegistryTypeMCPB, model.RegistryURLGitLab, "https://gitlab.com/owner/repo/-/releases/v1.0.0/downloads/package.mcpb", false},

		// Invalid registry types (should fail)
		{"invalid_maven", "maven", "https://example.com/registry", "test-package", true},
		{"invalid_cargo", "cargo", "https://example.com/registry", "test-package", true},
		{"invalid_gem", "gem", "https://example.com/registry", "test-package", true},
		{"invalid_invalid", "invalid", "https://example.com/registry", "test-package", true},
		{"invalid_unknown", "UNKNOWN", "https://example.com/registry", "test-package", true},
		{"invalid_custom", "custom-registry", "https://example.com/registry", "test-package", true},
		{"invalid_github", "github", "https://example.com/registry", "test-package", true}, // This is a source, not a registry type
		{"invalid_docker", "docker", "https://example.com/registry", "test-package", true}, // Should be "oci"
		{"invalid_empty", "", "https://example.com/registry", "test-package", true},        // Empty registry type
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			serverDetail := apiv0.ServerJSON{
				Name:        "com.example/test-server",
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
						Identifier:      tc.identifier,
						RegistryType:    tc.registryType,
						RegistryBaseURL: tc.baseURL,
					},
				},
				Remotes: []model.Remote{
					{
						URL: "https://example.com/remote",
					},
				},
			}

			err := validators.ValidateServerJSON(&serverDetail)
			if tc.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), validators.ErrUnsupportedRegistryType.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidate_RegistryBaseURLs(t *testing.T) {
	testCases := []struct {
		name         string
		registryType string
		baseURL      string
		identifier   string
		expectError  bool
	}{
		// Invalid base URLs for specific registry types
		{"npm_wrong_url", model.RegistryTypeNPM, "https://pypi.org", "test-package", true},
		{"pypi_wrong_url", model.RegistryTypePyPI, "https://registry.npmjs.org", "test-package", true},
		{"oci_wrong_url", model.RegistryTypeOCI, "https://registry.npmjs.org", "test-package", true},
		{"nuget_wrong_url", model.RegistryTypeNuGet, "https://docker.io", "test-package", true},
		{"mcpb_wrong_url", model.RegistryTypeMCPB, "https://evil.com", "https://github.com/owner/repo", true},
		{"mismatched_base_url_1", model.RegistryTypeNPM, model.RegistryURLDocker, "test-package", true},
		{"mismatched_base_url_2", model.RegistryTypeOCI, model.RegistryTypeNuGet, "test-package", true},

		// Localhost URLs should be rejected - no development exceptions
		{"localhost_npm", model.RegistryTypeNPM, "http://localhost:3000", "test-package", true},
		{"localhost_ip", model.RegistryTypePyPI, "http://127.0.0.1:8080", "test-package", true},

		// Valid combinations (should pass)
		{"valid_npm", model.RegistryTypeNPM, model.RegistryURLNPM, "test-package", false},
		{"valid_pypi", model.RegistryTypePyPI, model.RegistryURLPyPI, "test-package", false},
		{"valid_oci", model.RegistryTypeOCI, model.RegistryURLDocker, "test-package", false},
		{"valid_nuget", model.RegistryTypeNuGet, model.RegistryURLNuGet, "test-package", false},
		{"valid_mcpb_github", model.RegistryTypeMCPB, model.RegistryURLGitHub, "https://github.com/owner/repo/releases/download/v1.0.0/package.mcpb", false},
		{"valid_mcpb_gitlab", model.RegistryTypeMCPB, model.RegistryURLGitLab, "https://gitlab.com/owner/repo/-/releases/v1.0.0/downloads/package.mcpb", false},
		{"empty_base_url_npm", model.RegistryTypeNPM, "", "test-package", false},     // should be inferred
		{"empty_base_url_nuget", model.RegistryTypeNuGet, "", "test-package", false}, // should be inferred
		{"empty_base_url_mcpb", model.RegistryTypeMCPB, "", "https://github.com/owner/repo/releases/download/v1.0.0/package.mcpb", false},

		// Trailing slash URLs should be rejected - strict exact match only
		{"npm_trailing_slash", model.RegistryTypeNPM, "https://registry.npmjs.org/", "test-package", true},
		{"pypi_trailing_slash", model.RegistryTypePyPI, "https://pypi.org/", "test-package", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			serverDetail := apiv0.ServerJSON{
				Name:        "com.example/test-server",
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
						Identifier:      tc.identifier,
						RegistryType:    tc.registryType,
						RegistryBaseURL: tc.baseURL,
					},
				},
				Remotes: []model.Remote{
					{
						URL: "https://example.com/remote",
					},
				},
			}

			err := validators.ValidateServerJSON(&serverDetail)
			if tc.expectError {
				assert.Error(t, err)
				// Check that the error is related to registry validation
				errStr := err.Error()
				assert.True(t,
					strings.Contains(errStr, validators.ErrUnsupportedRegistryBaseURL.Error()) ||
						strings.Contains(errStr, validators.ErrMismatchedRegistryTypeAndURL.Error()),
					"Expected registry validation error, got: %s", errStr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidate_EmptyRegistryType(t *testing.T) {
	// Test that empty registry type is rejected
	serverDetail := apiv0.ServerJSON{
		Name:        "com.example/test-server",
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
				RegistryType:    "", // Empty registry type
				RegistryBaseURL: "",
			},
		},
		Remotes: []model.Remote{
			{
				URL: "https://example.com/remote",
			},
		},
	}

	err := validators.ValidateServerJSON(&serverDetail)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), validators.ErrUnsupportedRegistryType.Error())
	assert.Contains(t, err.Error(), "registry type is required")
}

func createValidServerWithArgument(arg model.Argument) apiv0.ServerJSON {
	return apiv0.ServerJSON{
		Name:        "com.example/test-server",
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
				Identifier:       "test-package",
				RegistryType:     "npm",
				RegistryBaseURL:  "https://registry.npmjs.org",
				RuntimeArguments: []model.Argument{arg},
			},
		},
		Remotes: []model.Remote{
			{
				URL: "https://example.com/remote",
			},
		},
	}
}

func TestValidate_MCPBReleaseURLs(t *testing.T) {
	testCases := []struct {
		name        string
		identifier  string
		expectError bool
		errorMsg    string
	}{
		// Valid GitHub release URLs
		{"valid_github_release", "https://github.com/owner/repo/releases/download/v1.0.0/package.mcpb", false, ""},
		{"valid_github_release_with_path", "https://github.com/org/project/releases/download/v2.1.0/my-server.mcpb", false, ""},
		{"valid_github_complex_tag", "https://github.com/owner/repo/releases/download/v1.0.0-alpha.1+build.123/package.mcpb", false, ""},
		{"valid_github_single_char_owner", "https://github.com/a/b/releases/download/v1.0.0/package.mcpb", false, ""},
		
		// Valid GitLab release URLs
		{"valid_gitlab_releases", "https://gitlab.com/owner/repo/-/releases/v1.0.0/downloads/package.mcpb", false, ""},
		{"valid_gitlab_package_files", "https://gitlab.com/owner/repo/-/package_files/123/download", false, ""},
		{"valid_gitlab_nested_group", "https://gitlab.com/group/subgroup/repo/-/releases/v1.0.0/downloads/package.mcpb", false, ""},
		{"valid_gitlab_deep_nested", "https://gitlab.com/org/team/project/repo/-/releases/v2.0.0/downloads/server.mcpb", false, ""},
		
		// Invalid GitHub URLs (not release URLs)
		{"invalid_github_root", "https://github.com/owner/repo", true, "GitHub MCPB packages must be release assets"},
		{"invalid_github_tree", "https://github.com/owner/repo/tree/main", true, "GitHub MCPB packages must be release assets"},
		{"invalid_github_blob", "https://github.com/owner/repo/blob/main/file.mcpb", true, "GitHub MCPB packages must be release assets"},
		{"invalid_github_fake_release_path", "https://github.com/owner/repo/fake/releases/download/v1.0.0/file.mcpb", true, "GitHub MCPB packages must be release assets"},
		{"invalid_github_missing_tag", "https://github.com/owner/repo/releases/download//file.mcpb", true, "GitHub MCPB packages must be release assets"},
		{"invalid_github_missing_filename", "https://github.com/owner/repo/releases/download/v1.0.0/", true, "GitHub MCPB packages must be release assets"},
		
		// Invalid GitLab URLs (not release URLs)
		{"invalid_gitlab_root", "https://gitlab.com/owner/repo", true, "GitLab MCPB packages must be release assets"},
		{"invalid_gitlab_tree", "https://gitlab.com/owner/repo/-/tree/main", true, "GitLab MCPB packages must be release assets"},
		{"invalid_gitlab_blob", "https://gitlab.com/owner/repo/-/blob/main/file.mcpb", true, "GitLab MCPB packages must be release assets"},
		{"invalid_gitlab_missing_dash_prefix", "https://gitlab.com/owner/repo/releases/v1.0.0/downloads/file.mcpb", true, "GitLab MCPB packages must be release assets"},
		{"invalid_gitlab_missing_downloads", "https://gitlab.com/owner/repo/-/releases/v1.0.0/file.mcpb", true, "GitLab MCPB packages must be release assets"},
		{"invalid_gitlab_invalid_package_files", "https://gitlab.com/owner/repo/-/package_files/abc/download", true, "GitLab MCPB packages must be release assets"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			server := apiv0.ServerJSON{
				Name:        "com.example/test-server",
				Description: "Test server",
				VersionDetail: model.VersionDetail{
					Version: "1.0.0",
				},
				Packages: []model.Package{
					{
						RegistryType:    model.RegistryTypeMCPB,
						RegistryBaseURL: model.RegistryURLGitHub,
						Identifier:      tc.identifier,
					},
				},
				Remotes: []model.Remote{
					{
						URL: "https://example.com/remote",
					},
				},
			}

			err := validators.ValidateServerJSON(&server)
			if tc.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tc.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
