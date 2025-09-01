package validators_test

import (
	"fmt"
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
