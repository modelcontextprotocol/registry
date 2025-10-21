package registries_test

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/registry/internal/validators/registries"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
)

func TestValidateOCI_RegistryAllowlist(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		identifier  string
		expectError bool
		errorMsg    string
	}{
		// Allowed registries - these should NOT fail with "unsupported registry"
		{
			name:       "Docker Hub should be allowed",
			identifier: "docker.io/test/image:latest",
			// Will fail on image not found, but registry should be accepted
			expectError: true,
		},
		{
			name:       "Docker Hub without explicit registry should default and be allowed",
			identifier: "test/image:latest",
			// Will fail on image not found, but registry should be accepted
			expectError: true,
		},
		{
			name:       "GHCR should be allowed",
			identifier: "ghcr.io/test/image:latest",
			// Will fail on image fetch, but registry should be accepted
			expectError: true,
		},
		{
			name:       "Artifact Registry us-central1 should be allowed",
			identifier: "us-central1-docker.pkg.dev/project/repo/image:latest",
			// Will fail on image fetch, but registry should be accepted
			expectError: true,
		},
		{
			name:       "Artifact Registry europe-west1 should be allowed",
			identifier: "europe-west1-docker.pkg.dev/project/repo/image:latest",
			// Will fail on image fetch, but registry should be accepted
			expectError: true,
		},
		{
			name:       "Artifact Registry multi-region us should be allowed",
			identifier: "us-docker.pkg.dev/project/repo/image:latest",
			// Will fail on image fetch, but registry should be accepted
			expectError: true,
		},

		// Disallowed registries
		{
			name:        "GCR should be rejected",
			identifier:  "gcr.io/test/image:latest",
			expectError: true,
			errorMsg:    "unsupported OCI registry",
		},
		{
			name:        "Quay.io should be rejected",
			identifier:  "quay.io/test/image:latest",
			expectError: true,
			errorMsg:    "unsupported OCI registry",
		},
		{
			name:        "ECR Public should be rejected",
			identifier:  "public.ecr.aws/test/image:latest",
			expectError: true,
			errorMsg:    "unsupported OCI registry",
		},
		{
			name:        "GitLab registry should be rejected",
			identifier:  "registry.gitlab.com/test/image:latest",
			expectError: true,
			errorMsg:    "unsupported OCI registry",
		},
		{
			name:        "Custom registry should be rejected",
			identifier:  "custom-registry.com/test/image:latest",
			expectError: true,
			errorMsg:    "unsupported OCI registry",
		},
		{
			name:        "Harbor registry should be rejected",
			identifier:  "harbor.example.com/test/image:latest",
			expectError: true,
			errorMsg:    "unsupported OCI registry",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := model.Package{
				RegistryType: model.RegistryTypeOCI,
				Identifier:   tt.identifier,
			}

			err := registries.ValidateOCI(ctx, pkg, "com.example/test")

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					// Should contain the specific error message
					assert.Contains(t, err.Error(), tt.errorMsg)
				} else {
					// For allowed registries, should NOT be "unsupported registry" error
					assert.NotContains(t, err.Error(), "unsupported OCI registry")
				}
			} else {
				assert.NoError(t, err)
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := registries.ValidateOCI(ctx, tt.pkg, "com.example/test")

			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.errorMessage)
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

func TestValidateOCI_EmptyIdentifier(t *testing.T) {
	ctx := context.Background()

	pkg := model.Package{
		RegistryType: model.RegistryTypeOCI,
		Identifier:   "",
	}

	err := registries.ValidateOCI(ctx, pkg, "com.example/test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "package identifier is required")
}
