package validators_test

import (
	"testing"

	"github.com/modelcontextprotocol/registry/internal/validators"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name          string
		serverDetail  model.ServerJSON
		expectedError string
	}{
		{
			name: "valid server detail with all fields",
			serverDetail: model.ServerJSON{
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
			serverDetail: model.ServerJSON{
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
			serverDetail: model.ServerJSON{
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
			serverDetail: model.ServerJSON{
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
			serverDetail: model.ServerJSON{
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
			serverDetail: model.ServerJSON{
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
			serverDetail: model.ServerJSON{
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
			serverDetail: model.ServerJSON{
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
			serverDetail: model.ServerJSON{
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
			serverDetail: model.ServerJSON{
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
			serverDetail: model.ServerJSON{
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
			errorMsg:    "invalid remote URL",
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
