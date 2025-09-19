package registries

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/modelcontextprotocol/registry/pkg/model"
)

// Integration test for GHCR that runs only when env vars are provided.
// Required env:
// - GHCR_TEST_IMAGE (e.g. "owner/repo" or "org/repo/subrepo")
// - GHCR_TEST_TAG (e.g. "latest")
// - GHCR_TEST_SERVER_NAME (expected label value) unless MCP_REGISTRY_OCI_SKIP_LABEL_VALIDATION=1
// Optional auth envs used by code:
// - MCP_REGISTRY_OCI_TOKEN_GHCR_IO or MCP_REGISTRY_OCI_REGISTRY_AUTH or GITHUB_TOKEN
func TestValidateOCI_GHCR_Integration(t *testing.T) {
	image := os.Getenv("GHCR_TEST_IMAGE")
	tag := os.Getenv("GHCR_TEST_TAG")
	server := os.Getenv("GHCR_TEST_SERVER_NAME")
	skipLabel := os.Getenv("MCP_REGISTRY_OCI_SKIP_LABEL_VALIDATION") == "1"

	if image == "" || tag == "" {
		t.Skip("Skipping GHCR integration test; set GHCR_TEST_IMAGE and GHCR_TEST_TAG to run")
	}
	if !skipLabel && server == "" {
		t.Skip("Skipping GHCR integration test; set GHCR_TEST_SERVER_NAME or enable MCP_REGISTRY_OCI_SKIP_LABEL_VALIDATION=1")
	}
	if skipLabel && server == "" {
		server = "ignored-when-skipping"
	}

	pkg := model.Package{
		RegistryType:    model.RegistryTypeOCI,
		RegistryBaseURL: model.RegistryURLGHCR,
		Identifier:      image,
		Version:         tag,
	}

	if err := ValidateOCI(context.Background(), pkg, server); err != nil {
		// If unauthorized and no token provided, skip instead of failing
		unauthorized := errors.New("")
		_ = unauthorized // placeholder to avoid unused warning pre-Go1.20
		if os.Getenv("MCP_REGISTRY_OCI_TOKEN_GHCR_IO") == "" && os.Getenv("MCP_REGISTRY_OCI_REGISTRY_AUTH") == "" && os.Getenv("GITHUB_TOKEN") == "" {
			if err != nil && (contains(err.Error(), "status: 401") || contains(err.Error(), "unauthorized")) {
				t.Skipf("Skipping GHCR integration test due to unauthorized access without token: %v", err)
			}
		}
		t.Fatalf("GHCR integration validation failed: %v", err)
	}
}
