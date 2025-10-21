package registries

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

var (
	ErrMissingIdentifierForOCI = errors.New("package identifier is required for OCI packages")
)

// ErrRateLimited is returned when a registry rate limits our requests
var ErrRateLimited = errors.New("rate limited by registry")

// ValidateOCI validates that an OCI image contains the correct MCP server name annotation.
// Supports canonical OCI references including:
//   - registry/namespace/image:tag
//   - registry/namespace/image@sha256:digest
//   - registry/namespace/image:tag@sha256:digest
//   - namespace/image:tag (defaults to docker.io)
//
// This validator now supports ANY public OCI-compliant registry including:
//   - Docker Hub (docker.io)
//   - GitHub Container Registry (ghcr.io)
//   - Quay.io (quay.io)
//   - Google Container Registry (gcr.io, artifacts.dev)
//   - Amazon ECR Public (public.ecr.aws)
//   - GitLab Container Registry (registry.gitlab.com)
//   - Any other OCI Distribution Spec compliant registry
func ValidateOCI(ctx context.Context, pkg model.Package, serverName string) error {
	if pkg.Identifier == "" {
		return ErrMissingIdentifierForOCI
	}

	// Validate that old format fields are not present
	if pkg.RegistryBaseURL != "" {
		return fmt.Errorf("OCI packages must not have 'registryBaseUrl' field - use canonical reference in 'identifier' instead (e.g., 'docker.io/owner/image:1.0.0')")
	}
	if pkg.Version != "" {
		return fmt.Errorf("OCI packages must not have 'version' field - include version in 'identifier' instead (e.g., 'docker.io/owner/image:1.0.0')")
	}
	if pkg.FileSHA256 != "" {
		return fmt.Errorf("OCI packages must not have 'fileSha256' field")
	}

	// Parse the OCI reference using go-containerregistry's name package
	// This handles all the complexity of reference parsing including defaults
	ref, err := name.ParseReference(pkg.Identifier)
	if err != nil {
		return fmt.Errorf("invalid OCI reference: %w", err)
	}

	// Fetch the image using anonymous authentication (public images only)
	// The go-containerregistry library handles:
	// - OCI auth discovery via WWW-Authenticate headers
	// - Token negotiation for different registries
	// - Rate limiting and retries
	// - Multi-arch manifest resolution
	img, err := remote.Image(ref, remote.WithAuth(authn.Anonymous), remote.WithContext(ctx))
	if err != nil {
		// Check if this is a rate limiting error
		var transportErr *transport.Error
		if errors.As(err, &transportErr) {
			if transportErr.StatusCode == http.StatusTooManyRequests {
				log.Printf("Skipping OCI validation for %s due to rate limiting", pkg.Identifier)
				return nil
			}
			if transportErr.StatusCode == http.StatusNotFound || transportErr.StatusCode == http.StatusUnauthorized {
				return fmt.Errorf("OCI image '%s' not found or not accessible (status: %d)", pkg.Identifier, transportErr.StatusCode)
			}
		}
		return fmt.Errorf("failed to fetch OCI image: %w", err)
	}

	// Get the image config which contains labels
	configFile, err := img.ConfigFile()
	if err != nil {
		return fmt.Errorf("failed to get image config: %w", err)
	}

	// Validate the MCP server name label
	if configFile.Config.Labels == nil {
		return fmt.Errorf("OCI image '%s' is missing required annotation. Add this to your Dockerfile: LABEL io.modelcontextprotocol.server.name=\"%s\"", pkg.Identifier, serverName)
	}

	mcpName, exists := configFile.Config.Labels["io.modelcontextprotocol.server.name"]
	if !exists {
		return fmt.Errorf("OCI image '%s' is missing required annotation. Add this to your Dockerfile: LABEL io.modelcontextprotocol.server.name=\"%s\"", pkg.Identifier, serverName)
	}

	if mcpName != serverName {
		return fmt.Errorf("OCI image ownership validation failed. Expected annotation 'io.modelcontextprotocol.server.name' = '%s', got '%s'", serverName, mcpName)
	}

	return nil
}
