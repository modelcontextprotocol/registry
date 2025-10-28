package registries

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

var (
	ErrMissingIdentifierForOCI = errors.New("package identifier is required for OCI packages")
	ErrUnsupportedRegistry     = errors.New("unsupported OCI registry")
)

// ErrRateLimited is returned when a registry rate limits our requests
var ErrRateLimited = errors.New("rate limited by registry")

// allowedOCIRegistries defines the list of supported OCI registries.
// This can be expanded in the future to support additional public registries.
var allowedOCIRegistries = map[string]bool{
	// Docker Hub (and its various endpoints)
	"docker.io":            true,
	"registry-1.docker.io": true, // Docker Hub API endpoint
	"index.docker.io":      true, // Docker Hub index
	// GitHub Container Registry
	"ghcr.io": true,
	// Google Artifact Registry (*.pkg.dev pattern handled in isAllowedRegistry)
}

// ValidateOCI validates that an OCI image contains the correct MCP server name annotation.
// Supports canonical OCI references including:
//   - registry/namespace/image:tag
//   - registry/namespace/image@sha256:digest
//   - registry/namespace/image:tag@sha256:digest
//   - namespace/image:tag (defaults to docker.io)
//
// Supported registries:
//   - Docker Hub (docker.io)
//   - GitHub Container Registry (ghcr.io)
//   - Google Artifact Registry (*.pkg.dev)
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

	// Validate that the registry is in the allowlist
	registry := ref.Context().RegistryStr()
	if !isAllowedRegistry(registry) {
		return fmt.Errorf("%w: %s", ErrUnsupportedRegistry, registry)
	}

	// Add explicit timeout to prevent hanging on slow registries
	// Use a new context with timeout to avoid modifying the caller's context
	timeoutCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Fetch the image using anonymous authentication (public images only)
	// The go-containerregistry library handles:
	// - OCI auth discovery via WWW-Authenticate headers
	// - Token negotiation for different registries
	// - Rate limiting and retries
	// - Multi-arch manifest resolution
	img, err := remote.Image(ref, remote.WithAuth(authn.Anonymous), remote.WithContext(timeoutCtx))
	if err != nil {
		// Check if this is a timeout error
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("OCI image validation timed out after 30 seconds for '%s'. The registry may be slow or unreachable", pkg.Identifier)
		}

		// Check for specific HTTP status codes
		var transportErr *transport.Error
		if errors.As(err, &transportErr) {
			switch transportErr.StatusCode {
			case http.StatusTooManyRequests:
				// Rate limited - skip validation to avoid blocking publishers
				// This is intentional: we prioritize UX over strict validation during high traffic
				log.Printf("Skipping OCI validation for %s due to rate limiting", pkg.Identifier)
				return nil
			case http.StatusNotFound:
				return fmt.Errorf("OCI image '%s' does not exist in the registry", pkg.Identifier)
			case http.StatusUnauthorized, http.StatusForbidden:
				return fmt.Errorf("OCI image '%s' is private or requires authentication. Only public images are supported", pkg.Identifier)
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

// isAllowedRegistry checks if the given registry is in the allowlist.
// It handles registry aliases and wildcard patterns (e.g., *.pkg.dev for Artifact Registry).
func isAllowedRegistry(registry string) bool {
	// Direct match
	if allowedOCIRegistries[registry] {
		return true
	}

	// Check for wildcard patterns
	// Google Artifact Registry: *.pkg.dev (e.g., us-docker.pkg.dev, europe-west1-docker.pkg.dev)
	if strings.HasSuffix(registry, ".pkg.dev") {
		return true
	}

	return false
}
