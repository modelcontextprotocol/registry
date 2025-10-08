package validators_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/validators"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name          string
		serverDetail  apiv0.ServerJSON
		expectedError string
	}{
		{
			name: "Schema version is required",
			serverDetail: apiv0.ServerJSON{
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "1.0.0",
			},
			expectedError: "$schema field is required",
		},
		{
			name: "Schema version rejects old schema (2025-01-27)",
			serverDetail: apiv0.ServerJSON{
				Schema:      "https://static.modelcontextprotocol.io/schemas/2025-01-27/server.schema.json",
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "1.0.0",
			},
			expectedError: "schema version https://static.modelcontextprotocol.io/schemas/2025-01-27/server.schema.json is not supported",
		},
		{
			name: "Schema version accepts current schema (2025-09-29)",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "1.0.0",
			},
			expectedError: "",
		},
		{
			name: "Version rejects top-level version ranges",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "^1.2.3",
			},
			expectedError: validators.ErrVersionLooksLikeRange.Error(),
		},
		{
			name: "rejects package version ranges",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "1.0.0",
				Packages: []model.Package{
					{
						Identifier:   "test-package",
						RegistryType: model.RegistryTypeNPM,
						Version:      ">=1.2.3",
						Transport:    model.Transport{Type: "stdio"},
					},
				},
			},
			expectedError: validators.ErrVersionLooksLikeRange.Error(),
		},
		{
			name: "Version allows specific versions (semver)",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "1.2.3",
				Packages: []model.Package{
					{
						Identifier:   "test-package",
						RegistryType: model.RegistryTypeNPM,
						Version:      "1.2.3-alpha.1",
						Transport:    model.Transport{Type: "stdio"},
					},
				},
			},
			expectedError: "",
		},
		{
			name: "Version allows specific non-semver versions",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "2021.03.15",
				Packages: []model.Package{
					{
						Identifier:   "test-package",
						RegistryType: model.RegistryTypeNPM,
						Version:      "snapshot",
						Transport:    model.Transport{Type: "stdio"},
					},
				},
			},
			expectedError: "",
		},
		{
			name: "Version rejects wildcard and x-range",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "1.x",
			},
			expectedError: validators.ErrVersionLooksLikeRange.Error(),
		},
		{
			name: "Version rejects wildcard *",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "1.*",
			},
			expectedError: validators.ErrVersionLooksLikeRange.Error(),
		},
		{
			name: "Version allows freeform version with hyphen not a range",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "snapshot - 2025.09",
			},
			expectedError: "",
		},
		{
			name: "Version rejects hyphen range of two versions",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "1.2.3 - 2.0.0",
			},
			expectedError: validators.ErrVersionLooksLikeRange.Error(),
		},
		{
			name: "Version rejects OR range with two versions",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "1.2 || 1.3",
			},
			expectedError: validators.ErrVersionLooksLikeRange.Error(),
		},
		{
			name: "Version rejects comparator with space",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: ">= 1.2.3",
			},
			expectedError: validators.ErrVersionLooksLikeRange.Error(),
		},
		// Server name validation - multiple slashes
		{
			name: "server name with two slashes",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/server/path",
				Description: "A test server",
				Version:     "1.0.0",
			},
			expectedError: validators.ErrMultipleSlashesInServerName.Error(),
		},
		{
			name: "server name with three slashes",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/server/path/deep",
				Description: "A test server",
				Version:     "1.0.0",
			},
			expectedError: validators.ErrMultipleSlashesInServerName.Error(),
		},
		{
			name: "valid server detail with all fields",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
					ID:     "owner/repo",
				},
				Version:    "1.0.0",
				WebsiteURL: "https://example.com/docs",
				Packages: []model.Package{
					{
						Identifier:      "test-package",
						RegistryType:    "npm",
						RegistryBaseURL: "https://registry.npmjs.org",
						Transport: model.Transport{
							Type: "stdio",
						},
					},
				},
				Remotes: []model.Transport{
					{
						Type: "streamable-http",
						URL:  "https://example.com/remote",
					},
				},
			},
			expectedError: "",
		},
		{
			name: "server with invalid repository source",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://bitbucket.org/owner/repo",
					Source: "bitbucket", // Not in validSources
				},
				Version: "1.0.0",
			},
			expectedError: validators.ErrInvalidRepositoryURL.Error(),
		},
		{
			name: "server with invalid GitHub URL format",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner", // Missing repo name
					Source: "github",
				},
				Version: "1.0.0",
			},
			expectedError: validators.ErrInvalidRepositoryURL.Error(),
		},
		{
			name: "server with invalid GitLab URL format",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://gitlab.com", // Missing owner and repo
					Source: "gitlab",
				},
				Version: "1.0.0",
			},
			expectedError: validators.ErrInvalidRepositoryURL.Error(),
		},
		{
			name: "server with valid repository subfolder",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:       "https://github.com/owner/repo",
					Source:    "github",
					Subfolder: "servers/my-server",
				},
				Version: "1.0.0",
			},
			expectedError: "",
		},
		{
			name: "server with repository subfolder containing path traversal",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:       "https://github.com/owner/repo",
					Source:    "github",
					Subfolder: "../parent/folder",
				},
				Version: "1.0.0",
			},
			expectedError: validators.ErrInvalidSubfolderPath.Error(),
		},
		{
			name: "server with repository subfolder starting with slash",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:       "https://github.com/owner/repo",
					Source:    "github",
					Subfolder: "/absolute/path",
				},
				Version: "1.0.0",
			},
			expectedError: validators.ErrInvalidSubfolderPath.Error(),
		},
		{
			name: "server with repository subfolder ending with slash",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:       "https://github.com/owner/repo",
					Source:    "github",
					Subfolder: "servers/my-server/",
				},
				Version: "1.0.0",
			},
			expectedError: validators.ErrInvalidSubfolderPath.Error(),
		},
		{
			name: "server with repository subfolder containing invalid characters",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:       "https://github.com/owner/repo",
					Source:    "github",
					Subfolder: "servers/my server",
				},
				Version: "1.0.0",
			},
			expectedError: validators.ErrInvalidSubfolderPath.Error(),
		},
		{
			name: "server with repository subfolder containing empty segments",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:       "https://github.com/owner/repo",
					Source:    "github",
					Subfolder: "servers//my-server",
				},
				Version: "1.0.0",
			},
			expectedError: validators.ErrInvalidSubfolderPath.Error(),
		},
		{
			name: "server with valid websiteUrl",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version:    "1.0.0",
				WebsiteURL: "https://example.com/docs",
			},
			expectedError: "",
		},
		{
			name: "server with invalid websiteUrl - no scheme",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version:    "1.0.0",
				WebsiteURL: "example.com/docs",
			},
			expectedError: "websiteUrl must be absolute (include scheme): example.com/docs",
		},
		{
			name: "server with invalid websiteUrl - invalid scheme",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version:    "1.0.0",
				WebsiteURL: "ftp://example.com/docs",
			},
			expectedError: "websiteUrl must use https scheme: ftp://example.com/docs",
		},
		{
			name: "server with malformed websiteUrl",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version:    "1.0.0",
				WebsiteURL: "ht tp://example.com/docs",
			},
			expectedError: "invalid websiteUrl:",
		},
		{
			name: "server with websiteUrl that matches namespace domain",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version:    "1.0.0",
				WebsiteURL: "https://example.com/docs",
			},
			expectedError: "",
		},
		{
			name: "server with websiteUrl subdomain that matches namespace",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version:    "1.0.0",
				WebsiteURL: "https://docs.example.com/mcp",
			},
			expectedError: "",
		},
		{
			name: "server with websiteUrl that does not match namespace",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version:    "1.0.0",
				WebsiteURL: "https://different.com/docs",
			},
			expectedError: "websiteUrl https://different.com/docs does not match namespace com.example/test-server",
		},
		{
			name: "package with spaces in name",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "1.0.0",
				Packages: []model.Package{
					{
						Identifier:      "test package with spaces",
						RegistryType:    "npm",
						RegistryBaseURL: "https://registry.npmjs.org",
						Transport: model.Transport{
							Type: "stdio",
						},
					},
				},
			},
			expectedError: validators.ErrPackageNameHasSpaces.Error(),
		},
		{
			name: "package with reserved version 'latest'",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "1.0.0",
				Packages: []model.Package{
					{
						Identifier:      "test-package",
						RegistryType:    "npm",
						RegistryBaseURL: "https://registry.npmjs.org",
						Version:         "latest",
						Transport: model.Transport{
							Type: "stdio",
						},
					},
				},
			},
			expectedError: validators.ErrReservedVersionString.Error(),
		},
		{
			name: "multiple packages with one invalid",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "1.0.0",
				Packages: []model.Package{
					{
						Identifier:      "valid-package",
						RegistryType:    "npm",
						RegistryBaseURL: "https://registry.npmjs.org",
						Transport: model.Transport{
							Type: "stdio",
						},
					},
					{
						Identifier:      "invalid package", // Has space
						RegistryType:    "pypi",
						RegistryBaseURL: "https://pypi.org",
						Transport: model.Transport{
							Type: "stdio",
						},
					},
				},
			},
			expectedError: validators.ErrPackageNameHasSpaces.Error(),
		},
		{
			name: "remote with invalid URL",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "1.0.0",
				Remotes: []model.Transport{
					{
						Type: "streamable-http",
						URL:  "not-a-valid-url",
					},
				},
			},
			expectedError: validators.ErrInvalidRemoteURL.Error(),
		},
		{
			name: "remote with missing scheme",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "1.0.0",
				Remotes: []model.Transport{
					{
						Type: "streamable-http",
						URL:  "example.com/remote",
					},
				},
			},
			expectedError: validators.ErrInvalidRemoteURL.Error(),
		},
		{
			name: "remote with localhost url",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "1.0.0",
				Remotes: []model.Transport{
					{
						Type: "streamable-http",
						URL:  "http://localhost",
					},
				},
			},
			expectedError: validators.ErrInvalidRemoteURL.Error(),
		},
		{
			name: "remote with localhost url with port",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "1.0.0",
				Remotes: []model.Transport{
					{
						Type: "streamable-http",
						URL:  "http://localhost:3000",
					},
				},
			},
			expectedError: validators.ErrInvalidRemoteURL.Error(),
		},
		{
			name: "multiple remotes with one invalid",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "1.0.0",
				Remotes: []model.Transport{
					{
						Type: "streamable-http",
						URL:  "https://valid.com/remote",
					},
					{
						Type: "streamable-http",
						URL:  "invalid-url",
					},
				},
			},
			expectedError: validators.ErrInvalidRemoteURL.Error(),
		},
		{
			name: "server detail with nil packages and remotes",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version:  "1.0.0",
				Packages: nil,
				Remotes:  nil,
			},
			expectedError: "",
		},
		{
			name: "server detail with empty packages and remotes slices",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version:  "1.0.0",
				Packages: []model.Package{},
				Remotes:  []model.Transport{},
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
				Schema: model.CurrentSchemaURL,
				Name:   "com.example/test-server",
				Remotes: []model.Transport{
					{
						Type: "streamable-http",
						URL:  "https://example.com/mcp",
					},
				},
			},
			expectError: false,
		},
		{
			name: "valid match - subdomain mcp.example.com",
			serverDetail: apiv0.ServerJSON{
				Schema: model.CurrentSchemaURL,
				Name:   "com.example/test-server",
				Remotes: []model.Transport{
					{
						Type: "streamable-http",
						URL:  "https://mcp.example.com/endpoint",
					},
				},
			},
			expectError: false,
		},
		{
			name: "valid match - api subdomain",
			serverDetail: apiv0.ServerJSON{
				Schema: model.CurrentSchemaURL,
				Name:   "com.example/api-server",
				Remotes: []model.Transport{
					{
						Type: "streamable-http",
						URL:  "https://api.example.com/mcp",
					},
				},
			},
			expectError: false,
		},
		{
			name: "invalid - wrong domain",
			serverDetail: apiv0.ServerJSON{
				Schema: model.CurrentSchemaURL,
				Name:   "com.example/test-server",
				Remotes: []model.Transport{
					{
						Type: "streamable-http",
						URL:  "https://google.com/mcp",
					},
				},
			},
			expectError: true,
			errorMsg:    "remote URL host google.com does not match publisher domain example.com",
		},
		{
			name: "invalid - different domain entirely",
			serverDetail: apiv0.ServerJSON{
				Schema: model.CurrentSchemaURL,
				Name:   "com.microsoft/server",
				Remotes: []model.Transport{
					{
						Type: "streamable-http",
						URL:  "https://api.github.com/endpoint",
					},
				},
			},
			expectError: true,
			errorMsg:    "remote URL host api.github.com does not match publisher domain microsoft.com",
		},
		{
			name: "invalid URL format",
			serverDetail: apiv0.ServerJSON{
				Schema: model.CurrentSchemaURL,
				Name:   "com.example/test",
				Remotes: []model.Transport{
					{
						Type: "streamable-http",
						URL:  "not-a-valid-url",
					},
				},
			},
			expectError: true,
			errorMsg:    "invalid remote URL",
		},
		{
			name: "empty remotes array",
			serverDetail: apiv0.ServerJSON{
				Schema:  model.CurrentSchemaURL,
				Name:    "com.example/test",
				Remotes: []model.Transport{},
			},
			expectError: false,
		},
		{
			name: "multiple valid remotes - different subdomains",
			serverDetail: apiv0.ServerJSON{
				Schema: model.CurrentSchemaURL,
				Name:   "com.example/server",
				Remotes: []model.Transport{
					{
						Type: "streamable-http",
						URL:  "https://api.example.com/sse",
					},
					{
						Type: "streamable-http",
						URL:  "https://mcp.example.com/websocket",
					},
				},
			},
			expectError: false,
		},
		{
			name: "one valid, one invalid remote",
			serverDetail: apiv0.ServerJSON{
				Schema: model.CurrentSchemaURL,
				Name:   "com.example/server",
				Remotes: []model.Transport{
					{
						Type: "streamable-http",
						URL:  "https://example.com/sse",
					},
					{
						Type: "streamable-http",
						URL:  "https://google.com/websocket",
					},
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
				Schema: model.CurrentSchemaURL,
				Name:   "com.example.api/server",
			},
			expectError: false,
		},
		{
			name: "valid complex namespace",
			serverDetail: apiv0.ServerJSON{
				Schema: model.CurrentSchemaURL,
				Name:   "com.microsoft.azure.service/webapp-server",
			},
			expectError: false,
		},
		{
			name: "empty server name",
			serverDetail: apiv0.ServerJSON{
				Schema: model.CurrentSchemaURL,
				Name:   "",
			},
			expectError: true,
			errorMsg:    "server name is required",
		},
		{
			name: "missing slash separator",
			serverDetail: apiv0.ServerJSON{
				Schema: model.CurrentSchemaURL,
				Name:   "com.example.server",
			},
			expectError: true,
			errorMsg:    "server name must be in format 'dns-namespace/name'",
		},
		{
			name: "empty namespace part",
			serverDetail: apiv0.ServerJSON{
				Schema: model.CurrentSchemaURL,
				Name:   "/server-name",
			},
			expectError: true,
			errorMsg:    "non-empty namespace and name parts",
		},
		{
			name: "empty name part",
			serverDetail: apiv0.ServerJSON{
				Schema: model.CurrentSchemaURL,
				Name:   "com.example/",
			},
			expectError: true,
			errorMsg:    "non-empty namespace and name parts",
		},
		{
			name: "multiple slashes - should be rejected",
			serverDetail: apiv0.ServerJSON{
				Schema: model.CurrentSchemaURL,
				Name:   "com.example/server/path",
			},
			expectError: true,
			errorMsg:    "server name cannot contain multiple slashes",
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

func TestValidate_MultipleSlashesInServerName(t *testing.T) {
	tests := []struct {
		name        string
		serverName  string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "single slash - valid",
			serverName:  "com.example/my-server",
			expectError: false,
		},
		{
			name:        "two slashes - invalid",
			serverName:  "com.example/my-server/extra",
			expectError: true,
			errorMsg:    "server name cannot contain multiple slashes",
		},
		{
			name:        "three slashes - invalid",
			serverName:  "com.example/my/server/name",
			expectError: true,
			errorMsg:    "server name cannot contain multiple slashes",
		},
		{
			name:        "many slashes - invalid",
			serverName:  "com.example/a/b/c/d/e",
			expectError: true,
			errorMsg:    "server name cannot contain multiple slashes",
		},
		{
			name:        "double slash - invalid",
			serverName:  "com.example//server",
			expectError: true,
			errorMsg:    "server name cannot contain multiple slashes",
		},
		{
			name:        "trailing slash counts as two - invalid",
			serverName:  "com.example/server/",
			expectError: true,
			errorMsg:    "server name cannot contain multiple slashes",
		},
		{
			name:        "no slash - still invalid for different reason",
			serverName:  "com.example.server",
			expectError: true,
			errorMsg:    "server name must be in format 'dns-namespace/name'",
		},
		{
			name:        "complex valid namespace with single slash",
			serverName:  "com.microsoft.azure.service/webapp-server",
			expectError: false,
		},
		{
			name:        "complex namespace with multiple slashes - invalid",
			serverName:  "com.microsoft.azure/service/webapp-server",
			expectError: true,
			errorMsg:    "server name cannot contain multiple slashes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serverDetail := apiv0.ServerJSON{
				Schema: model.CurrentSchemaURL,
				Name:   tt.serverName,
			}
			err := validators.ValidateServerJSON(&serverDetail)

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
func TestValidate_TransportValidation(t *testing.T) {
	tests := []struct {
		name          string
		serverDetail  apiv0.ServerJSON
		expectedError string
	}{
		// Package transport tests - stdio (no URL required)
		{
			name: "package transport stdio without URL should pass",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Version:     "1.0.0",
				Packages: []model.Package{
					{
						Identifier:   "test-package",
						RegistryType: "npm",
						Transport: model.Transport{
							Type: "stdio",
						},
					},
				},
			},
			expectedError: "",
		},
		{
			name: "package transport stdio with URL (should fail)",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Version:     "1.0.0",
				Packages: []model.Package{
					{
						Identifier:   "test-package",
						RegistryType: "npm",
						Transport: model.Transport{
							Type: "stdio",
							URL:  "ignored-for-stdio",
						},
					},
				},
			},
			expectedError: "url must be empty for stdio transport type",
		},
		// Package transport tests - streamable-http (URL required)
		{
			name: "package transport streamable-http with valid URL",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Version:     "1.0.0",
				Packages: []model.Package{
					{
						Identifier:   "test-package",
						RegistryType: "npm",
						Transport: model.Transport{
							Type: "streamable-http",
							URL:  "https://example.com/mcp",
						},
					},
				},
			},
			expectedError: "",
		},
		{
			name: "package transport streamable-http with templated URL",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Version:     "1.0.0",
				Packages: []model.Package{
					{
						Identifier:   "test-package",
						RegistryType: "npm",
						Transport: model.Transport{
							Type: "streamable-http",
							URL:  "http://{host}:{port}/mcp",
						},
						EnvironmentVariables: []model.KeyValueInput{
							{Name: "host"},
							{Name: "port"},
						},
					},
				},
			},
			expectedError: "",
		},
		{
			name: "package transport streamable-http without URL",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Version:     "1.0.0",
				Packages: []model.Package{
					{
						Identifier:   "test-package",
						RegistryType: "npm",
						Transport: model.Transport{
							Type: "streamable-http",
						},
					},
				},
			},
			expectedError: "url is required for streamable-http transport type",
		},
		{
			name: "package transport streamable-http with templated URL missing variables",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Version:     "1.0.0",
				Packages: []model.Package{
					{
						Identifier:   "test-package",
						RegistryType: "npm",
						Transport: model.Transport{
							Type: "streamable-http",
							URL:  "http://{host}:{port}/mcp",
						},
						// Missing host and port variables
					},
				},
			},
			expectedError: "template variables in URL",
		},
		// Package transport tests - sse (URL required)
		{
			name: "package transport sse with valid URL",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Version:     "1.0.0",
				Packages: []model.Package{
					{
						Identifier:   "test-package",
						RegistryType: "npm",
						Transport: model.Transport{
							Type: "sse",
							URL:  "https://example.com/events",
						},
					},
				},
			},
			expectedError: "",
		},
		{
			name: "package transport sse without URL",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Version:     "1.0.0",
				Packages: []model.Package{
					{
						Identifier:   "test-package",
						RegistryType: "npm",
						Transport: model.Transport{
							Type: "sse",
						},
					},
				},
			},
			expectedError: "url is required for sse transport type",
		},
		// Package transport tests - unsupported type
		{
			name: "package transport unsupported type",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Version:     "1.0.0",
				Packages: []model.Package{
					{
						Identifier:   "test-package",
						RegistryType: "npm",
						Transport: model.Transport{
							Type: "websocket",
						},
					},
				},
			},
			expectedError: "unsupported transport type: websocket",
		},
		// Remote transport tests - streamable-http
		{
			name: "remote transport streamable-http with valid URL",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Version:     "1.0.0",
				Remotes: []model.Transport{
					{
						Type: "streamable-http",
						URL:  "https://example.com/mcp",
					},
				},
			},
			expectedError: "",
		},
		{
			name: "remote transport streamable-http without URL",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Version:     "1.0.0",
				Remotes: []model.Transport{
					{
						Type: "streamable-http",
					},
				},
			},
			expectedError: "url is required for streamable-http transport type",
		},
		// Remote transport tests - sse
		{
			name: "remote transport sse with valid URL",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Version:     "1.0.0",
				Remotes: []model.Transport{
					{
						Type: "sse",
						URL:  "https://example.com/events",
					},
				},
			},
			expectedError: "",
		},
		{
			name: "remote transport sse without URL",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Version:     "1.0.0",
				Remotes: []model.Transport{
					{
						Type: "sse",
					},
				},
			},
			expectedError: "url is required for sse transport type",
		},
		// Remote transport tests - unsupported types
		{
			name: "remote transport stdio not supported",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Version:     "1.0.0",
				Remotes: []model.Transport{
					{
						Type: "stdio",
					},
				},
			},
			expectedError: "unsupported transport type for remotes: stdio",
		},
		{
			name: "remote transport unsupported type",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Version:     "1.0.0",
				Remotes: []model.Transport{
					{
						Type: "websocket",
						URL:  "wss://example.com/ws",
					},
				},
			},
			expectedError: "unsupported transport type for remotes: websocket",
		},
		// Localhost URL tests - packages vs remotes
		{
			name: "package transport allows localhost URLs",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Version:     "1.0.0",
				Packages: []model.Package{
					{
						Identifier:   "test-package",
						RegistryType: "npm",
						Transport: model.Transport{
							Type: "streamable-http",
							URL:  "http://localhost:3000/mcp",
						},
					},
				},
			},
			expectedError: "",
		},
		{
			name: "remote transport rejects localhost URLs",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Version:     "1.0.0",
				Remotes: []model.Transport{
					{
						Type: "streamable-http",
						URL:  "http://localhost:3000/mcp",
					},
				},
			},
			expectedError: "invalid remote URL",
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

func TestValidate_RegistryTypesAndUrls(t *testing.T) {
	testCases := []struct {
		tcName       string
		name         string
		registryType string
		baseURL      string
		identifier   string
		version      string
		fileSHA256   string
		expectError  bool
	}{
		// Valid registry types (should pass)
		{"valid_npm", "io.github.domdomegg/airtable-mcp-server", model.RegistryTypeNPM, model.RegistryURLNPM, "airtable-mcp-server", "1.7.2", "", false},
		{"valid_npm", "io.github.domdomegg/airtable-mcp-server", model.RegistryTypeNPM, "", "airtable-mcp-server", "1.7.2", "", false},
		{"valid_pypi", "io.github.domdomegg/time-mcp-pypi", model.RegistryTypePyPI, model.RegistryURLPyPI, "time-mcp-pypi", "1.0.1", "", false},
		{"valid_pypi", "io.github.domdomegg/time-mcp-pypi", model.RegistryTypePyPI, "", "time-mcp-pypi", "1.0.1", "", false},
		{"valid_oci", "io.github.domdomegg/airtable-mcp-server", model.RegistryTypeOCI, "", "domdomegg/airtable-mcp-server:1.7.2", "", "", false},
		{"valid_nuget", "io.github.domdomegg/time-mcp-server", model.RegistryTypeNuGet, model.RegistryURLNuGet, "TimeMcpServer", "1.0.2", "", false},
		{"valid_nuget", "io.github.domdomegg/time-mcp-server", model.RegistryTypeNuGet, "", "TimeMcpServer", "1.0.2", "", false},
		{"valid_mcpb_github", "io.github.domdomegg/airtable-mcp-server", model.RegistryTypeMCPB, "", "https://github.com/domdomegg/airtable-mcp-server/releases/download/v1.7.2/airtable-mcp-server.mcpb", "", "fe333e598595000ae021bd27117db32ec69af6987f507ba7a63c90638ff633ce", false},
		{"valid_mcpb_gitlab", "io.gitlab.fforster/gitlab-mcp", model.RegistryTypeMCPB, "", "https://gitlab.com/fforster/gitlab-mcp/-/releases/v1.31.0/downloads/gitlab-mcp_1.31.0_Linux_x86_64.tar.gz", "", "abc123ef4567890abcdef1234567890abcdef1234567890abcdef1234567890", false}, // this is not actually a valid mcpb, but it's the closest I can get for testing for now

		// Test MCPB without file hash (should fail)
		{"invalid_mcpb_no_hash", "io.github.domdomegg/airtable-mcp-server", model.RegistryTypeMCPB, "", "https://github.com/domdomegg/airtable-mcp-server/releases/download/v1.7.2/airtable-mcp-server.mcpb", "", "", true},

		// Invalid registry types (should fail)
		{"invalid_maven", "io.github.domdomegg/airtable-mcp-server", "maven", model.RegistryURLNPM, "airtable-mcp-server", "1.7.2", "", true},
		{"invalid_cargo", "io.github.domdomegg/time-mcp-pypi", "cargo", model.RegistryURLPyPI, "time-mcp-pypi", "1.0.1", "", true},
		{"invalid_gem", "io.github.domdomegg/airtable-mcp-server", "gem", model.RegistryURLDocker, "domdomegg/airtable-mcp-server", "1.7.2", "", true},
		{"invalid_unknown", "io.github.domdomegg/time-mcp-server", "unknown", model.RegistryURLNuGet, "TimeMcpServer", "1.0.2", "", true},
		{"invalid_blank", "io.github.domdomegg/time-mcp-server", "", model.RegistryURLNuGet, "TimeMcpServer", "1.0.2", "", true},
		{"invalid_docker", "io.github.domdomegg/airtable-mcp-server", "docker", model.RegistryURLDocker, "domdomegg/airtable-mcp-server", "1.7.2", "", true},                                                                      // should be oci
		{"invalid_github", "io.github.domdomegg/airtable-mcp-server", "github", model.RegistryURLGitHub, "https://github.com/domdomegg/airtable-mcp-server/releases/download/v1.7.2/airtable-mcp-server.mcpb", "1.7.2", "", true}, // should be mcpb

		{"invalid_mix_1", "io.github.domdomegg/time-mcp-server", model.RegistryTypeNuGet, model.RegistryURLNPM, "TimeMcpServer", "1.0.2", "", true},
		{"invalid_mix_2", "io.github.domdomegg/airtable-mcp-server", model.RegistryTypeOCI, model.RegistryURLNPM, "domdomegg/airtable-mcp-server", "1.7.2", "", true},
		{"invalid_mix_3", "io.github.domdomegg/airtable-mcp-server", model.RegistryURLNPM, model.RegistryURLNPM, "airtable-mcp-server", "1.7.2", "", true},
	}

	for _, tc := range testCases {
		t.Run(tc.tcName, func(t *testing.T) {
			serverJSON := apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        tc.name,
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
					ID:     "owner/repo",
				},
				Version: "1.0.0",
				Packages: []model.Package{
					{
						Identifier:      tc.identifier,
						RegistryType:    tc.registryType,
						RegistryBaseURL: tc.baseURL,
						Version:         tc.version,
						FileSHA256:      tc.fileSHA256,
						Transport: model.Transport{
							Type: "stdio",
						},
					},
				},
			}

			err := validators.ValidatePublishRequest(context.Background(), serverJSON, &config.Config{
				EnableRegistryValidation: true,
			})
			if tc.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func createValidServerWithArgument(arg model.Argument) apiv0.ServerJSON {
	return apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.example/test-server",
		Description: "A test server",
		Repository: model.Repository{
			URL:    "https://github.com/owner/repo",
			Source: "github",
			ID:     "owner/repo",
		},
		Version: "1.0.0",
		Packages: []model.Package{
			{
				Identifier:      "test-package",
				RegistryType:    "npm",
				RegistryBaseURL: "https://registry.npmjs.org",
				Transport: model.Transport{
					Type: "stdio",
				},
				RuntimeArguments: []model.Argument{arg},
			},
		},
		Remotes: []model.Transport{
			{
				Type: "streamable-http",
				URL:  "https://example.com/remote",
			},
		},
	}
}

func TestValidateTitle(t *testing.T) {
	tests := []struct {
		name          string
		serverDetail  apiv0.ServerJSON
		expectedError string
	}{
		{
			name: "Valid title without MCP suffix",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Title:       "GitHub",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "1.0.0",
			},
			expectedError: "",
		},
		{
			name: "Valid title with multiple words",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Title:       "Weather API",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "1.0.0",
			},
			expectedError: "",
		},
		{
			name: "Empty title is allowed (optional field)",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Title:       "",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "1.0.0",
			},
			expectedError: "",
		},
		{
			name: "Allows title with 'MCP Server' suffix",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Title:       "GitHub MCP Server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "1.0.0",
			},
			expectedError: "",
		},
		{
			name: "Allows title with 'MCP' suffix",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Title:       "GitHub MCP",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "1.0.0",
			},
			expectedError: "",
		},
		{
			name: "Allows title with 'mcp server' suffix (lowercase)",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Title:       "GitHub mcp server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "1.0.0",
			},
			expectedError: "",
		},
		{
			name: "Allows title with hyphenated 'MCP' suffix",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Title:       "GitHub-MCP",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "1.0.0",
			},
			expectedError: "",
		},
		{
			name: "Allows 'MCP' in the middle of title",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Title:       "MCP Weather API",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "1.0.0",
			},
			expectedError: "",
		},
		{
			name: "Rejects title with only whitespace",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Title:       "   ",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "1.0.0",
			},
			expectedError: "title cannot be only whitespace",
		},
		// Icon validation tests
		{
			name: "Accepts valid icon with HTTPS URL",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "1.0.0",
				Icons: []model.Icon{
					{
						Src: "https://example.com/icon.png",
					},
				},
			},
			expectedError: "",
		},
		{
			name: "Accepts icon with all optional fields",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "1.0.0",
				Icons: []model.Icon{
					{
						Src:      "https://example.com/icon.png",
						MimeType: stringPtr("image/png"),
						Sizes:    []string{"48x48", "96x96"},
						Theme:    stringPtr("light"),
					},
				},
			},
			expectedError: "",
		},
		{
			name: "Accepts icon with 'any' size for SVG",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "1.0.0",
				Icons: []model.Icon{
					{
						Src:      "https://example.com/icon.svg",
						MimeType: stringPtr("image/svg+xml"),
						Sizes:    []string{"any"},
					},
				},
			},
			expectedError: "",
		},
		{
			name: "Accepts icon with dark theme",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "1.0.0",
				Icons: []model.Icon{
					{
						Src:   "https://example.com/icon-dark.png",
						Theme: stringPtr("dark"),
					},
				},
			},
			expectedError: "",
		},
		{
			name: "Accepts multiple icons",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "1.0.0",
				Icons: []model.Icon{
					{
						Src:   "https://example.com/icon-light.png",
						Theme: stringPtr("light"),
					},
					{
						Src:   "https://example.com/icon-dark.png",
						Theme: stringPtr("dark"),
					},
				},
			},
			expectedError: "",
		},
		{
			name: "Rejects icon with HTTP URL (not HTTPS)",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "1.0.0",
				Icons: []model.Icon{
					{
						Src: "http://example.com/icon.png",
					},
				},
			},
			expectedError: "icon src must use https scheme",
		},
		{
			name: "Rejects icon with data URI",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "1.0.0",
				Icons: []model.Icon{
					{
						Src: "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAUA",
					},
				},
			},
			expectedError: "icon src must use https scheme",
		},
		{
			name: "Rejects icon with relative URL",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
				Version: "1.0.0",
				Icons: []model.Icon{
					{
						Src: "/icon.png",
					},
				},
			},
			expectedError: "icon src must be an absolute URL",
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

// Helper function for creating string pointers in tests
func stringPtr(s string) *string {
	return &s
}
