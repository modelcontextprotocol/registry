package validators_test

import (
	"testing"

	"github.com/modelcontextprotocol/registry/internal/model"
	"github.com/modelcontextprotocol/registry/internal/validators"
	"github.com/stretchr/testify/assert"
)

func TestObjectValidator_Validate(t *testing.T) {
	validator := validators.NewObjectValidator()

	tests := []struct {
		name          string
		serverDetail  model.ServerDetail
		expectedError string
	}{
		{
			name: "valid server detail with all fields",
			serverDetail: model.ServerDetail{
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
						Name:         "test-package",
						RegistryName: "npm",
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
			serverDetail: model.ServerDetail{
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
			serverDetail: model.ServerDetail{
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
			serverDetail: model.ServerDetail{
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
			serverDetail: model.ServerDetail{
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
						Name:         "test package with spaces",
						RegistryName: "npm",
					},
				},
			},
			expectedError: validators.ErrPackageNameHasSpaces.Error(),
		},
		{
			name: "multiple packages with one invalid",
			serverDetail: model.ServerDetail{
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
						Name:         "valid-package",
						RegistryName: "npm",
					},
					{
						Name:         "invalid package", // Has space
						RegistryName: "pip",
					},
				},
			},
			expectedError: validators.ErrPackageNameHasSpaces.Error(),
		},
		{
			name: "remote with invalid URL",
			serverDetail: model.ServerDetail{
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
			serverDetail: model.ServerDetail{
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
			serverDetail: model.ServerDetail{
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
			serverDetail: model.ServerDetail{
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
			serverDetail: model.ServerDetail{
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
