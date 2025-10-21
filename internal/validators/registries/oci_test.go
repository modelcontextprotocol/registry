package registries_test

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/registry/internal/validators/registries"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
)

func TestValidateOCI_RealPackages(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		identifier   string
		serverName   string
		expectError  bool
		errorMessage string
		skip         bool
		skipReason   string
	}{
		{
			name:         "empty package identifier should fail",
			identifier:   "",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "package identifier is required for OCI packages",
		},
		{
			name:        "real image with correct MCP annotation should pass (Docker Hub)",
			identifier:  "docker.io/domdomegg/airtable-mcp-server:1.7.2",
			serverName:  "io.github.domdomegg/airtable-mcp-server",
			expectError: false,
			skip:        true,
			skipReason:  "Skipping to avoid hitting DockerHub rate limits in CI",
		},
		{
			name:        "GHCR image with correct MCP annotation should pass",
			identifier:  "ghcr.io/nkapila6/mcp-local-rag:latest",
			serverName:  "io.github.nkapila6/mcp-local-rag",
			expectError: false,
			skip:        true,
			skipReason:  "Skipping to avoid network dependencies in CI",
		},
		{
			name:         "image without MCP annotation should fail",
			identifier:   "docker.io/library/nginx:latest",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "missing required annotation",
			skip:         true,
			skipReason:   "Skipping to avoid hitting DockerHub rate limits in CI",
		},
		{
			name:         "non-existent image should fail",
			identifier:   "docker.io/nonexistent/doesnotexist:v99.99.99",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "not found",
			skip:         true,
			skipReason:   "Skipping to avoid network dependencies in CI",
		},
		{
			name:         "Quay.io registry should be supported",
			identifier:   "quay.io/test/image:v1.0.0",
			serverName:   "com.example/test",
			expectError:  true, // Will fail because image doesn't exist, but registry should be accepted
			errorMessage: "not found",
			skip:         true,
			skipReason:   "Skipping to avoid network dependencies in CI",
		},
		{
			name:         "GCR registry should be supported",
			identifier:   "gcr.io/test/image:v1.0.0",
			serverName:   "com.example/test",
			expectError:  true, // Will fail because image doesn't exist, but registry should be accepted
			errorMessage: "not found",
			skip:         true,
			skipReason:   "Skipping to avoid network dependencies in CI",
		},
		{
			name:         "GitLab registry should be supported",
			identifier:   "registry.gitlab.com/test/image:v1.0.0",
			serverName:   "com.example/test",
			expectError:  true, // Will fail because image doesn't exist, but registry should be accepted
			errorMessage: "not found",
			skip:         true,
			skipReason:   "Skipping to avoid network dependencies in CI",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skip {
				t.Skip(tt.skipReason)
			}

			pkg := model.Package{
				RegistryType: model.RegistryTypeOCI,
				Identifier:   tt.identifier,
			}

			err := registries.ValidateOCI(ctx, pkg, tt.serverName)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMessage != "" {
					assert.Contains(t, err.Error(), tt.errorMessage)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateOCI_AllRegistriesSupported(t *testing.T) {
	ctx := context.Background()

	// Test that various registry formats are accepted (they will fail on fetch, not on validation)
	testRegistries := []string{
		"docker.io/test/image:latest",
		"ghcr.io/test/image:latest",
		"quay.io/test/image:latest",
		"gcr.io/test/image:latest",
		"public.ecr.aws/test/image:latest",
		"registry.gitlab.com/test/image:latest",
		"custom-registry.com/test/image:latest",
	}

	for _, registry := range testRegistries {
		t.Run(registry, func(t *testing.T) {
			pkg := model.Package{
				RegistryType: model.RegistryTypeOCI,
				Identifier:   registry,
			}

			err := registries.ValidateOCI(ctx, pkg, "com.example/test")

			// Should NOT fail with "unsupported registry" error
			// Will fail with "not found" or similar, but that means the registry was accepted
			if err != nil {
				assert.NotContains(t, err.Error(), "unsupported registry")
				assert.NotContains(t, err.Error(), "registry type and base URL do not match")
			}
		})
	}
}

func TestValidateOCI_RejectsOldFormat(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		pkg          model.Package
		errorMessage string
	}{
		{
			name: "OCI package with registryBaseUrl should be rejected",
			pkg: model.Package{
				RegistryType:    model.RegistryTypeOCI,
				RegistryBaseURL: "https://docker.io",
				Identifier:      "docker.io/test/image:latest",
			},
			errorMessage: "OCI packages must not have 'registryBaseUrl' field",
		},
		{
			name: "OCI package with version field should be rejected",
			pkg: model.Package{
				RegistryType: model.RegistryTypeOCI,
				Identifier:   "docker.io/test/image:latest",
				Version:      "1.0.0",
			},
			errorMessage: "OCI packages must not have 'version' field",
		},
		{
			name: "OCI package with fileSha256 field should be rejected",
			pkg: model.Package{
				RegistryType: model.RegistryTypeOCI,
				Identifier:   "docker.io/test/image:latest",
				FileSHA256:   "abcd1234",
			},
			errorMessage: "OCI packages must not have 'fileSha256' field",
		},
		{
			name: "OCI package with canonical format should pass format validation",
			pkg: model.Package{
				RegistryType: model.RegistryTypeOCI,
				Identifier:   "docker.io/test/image:latest",
			},
			errorMessage: "", // Should pass old format check (will fail later due to image not existing)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := registries.ValidateOCI(ctx, tt.pkg, "com.example/test")

			if tt.errorMessage != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMessage)
			} else if err != nil {
				// Should not fail with old format error (may fail with other errors like image not found)
				assert.NotContains(t, err.Error(), "must not have 'registryBaseUrl'")
				assert.NotContains(t, err.Error(), "must not have 'version'")
				assert.NotContains(t, err.Error(), "must not have 'fileSha256'")
			}
		})
	}
}

func TestValidateOCI_InvalidReferences(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name       string
		identifier string
	}{
		{
			name:       "invalid characters in reference",
			identifier: "docker.io/test/image:INVALID SPACE",
		},
		{
			name:       "malformed reference",
			identifier: "not-a-valid-reference::::",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := model.Package{
				RegistryType: model.RegistryTypeOCI,
				Identifier:   tt.identifier,
			}

			err := registries.ValidateOCI(ctx, pkg, "com.example/test")
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "invalid OCI reference")
		})
	}
}
