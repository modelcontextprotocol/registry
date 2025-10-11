package registries

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/registry/pkg/model"
)

var (
	ErrMissingIdentifierForOCI = errors.New("package identifier is required for OCI packages")
	ErrMissingVersionForOCI    = errors.New("package version is required for OCI packages")
)

const (
	dockerIoAPIBaseURL = "https://registry-1.docker.io"
	ghcrAPIBaseURL     = "https://ghcr.io"
)

// ErrRateLimited is returned when a registry rate limits our requests
var ErrRateLimited = errors.New("rate limited by registry")

// OCIAuthResponse represents an OCI registry authentication response
type OCIAuthResponse struct {
	Token string `json:"token"`
}

// RegistryConfig holds configuration for different OCI registries
type RegistryConfig struct {
	APIBaseURL string
	AuthURL    string
	Service    string
	Scope      string
}

// getRegistryConfig returns the configuration for a specific registry
func getRegistryConfig(registryBaseURL, namespace, repo string) *RegistryConfig {
	switch registryBaseURL {
	case model.RegistryURLDocker:
		return &RegistryConfig{
			APIBaseURL: dockerIoAPIBaseURL,
			AuthURL:    "https://auth.docker.io/token",
			Service:    "registry.docker.io",
			Scope:      fmt.Sprintf("repository:%s/%s:pull", namespace, repo),
		}
	case model.RegistryURLGHCR:
		return &RegistryConfig{
			APIBaseURL: ghcrAPIBaseURL,
			AuthURL:    fmt.Sprintf("%s/token", ghcrAPIBaseURL),
			Service:    "ghcr.io",
			Scope:      fmt.Sprintf("repository:%s/%s:pull", namespace, repo),
		}
	default:
		return nil
	}
}

// OCIManifest represents an OCI image manifest
type OCIManifest struct {
	Manifests []struct {
		Digest string `json:"digest"`
	} `json:"manifests,omitempty"`
	Config struct {
		Digest string `json:"digest"`
	} `json:"config,omitempty"`
}

// OCIImageConfig represents an OCI image configuration
type OCIImageConfig struct {
	Config struct {
		Labels map[string]string `json:"Labels"`
	} `json:"config"`
}

// ValidateOCI validates that an OCI image contains the correct MCP server name annotation.
// Supports canonical OCI references including:
//   - registry/namespace/image:tag
//   - registry/namespace/image@sha256:digest
//   - registry/namespace/image:tag@sha256:digest
//   - namespace/image:tag (defaults to docker.io)
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

	// Parse the canonical OCI reference from the identifier
	ociRef, err := ParseOCIReference(pkg.Identifier)
	if err != nil {
		return fmt.Errorf("invalid OCI reference: %w", err)
	}

	// Validate that the registry is supported
	registryBaseURL := ociRef.GetRegistryBaseURL()
	if err := validateRegistryURL(registryBaseURL); err != nil {
		return err
	}

	client := &http.Client{Timeout: 10 * time.Second}

	// Get registry configuration
	registryConfig := getRegistryConfig(registryBaseURL, ociRef.Namespace, ociRef.Image)
	if registryConfig == nil {
		return fmt.Errorf("unsupported registry: %s", registryBaseURL)
	}

	// Determine what to use for manifest lookup: digest if available (most secure), otherwise tag
	manifestRef := ociRef.Tag
	if ociRef.Digest != "" {
		manifestRef = ociRef.Digest
	}

	// Get the image manifest
	manifest, err := fetchImageManifest(ctx, client, registryConfig, ociRef.Namespace, ociRef.Image, manifestRef)
	if err != nil {
		// Handle rate limiting explicitly - skip validation
		if errors.Is(err, ErrRateLimited) {
			log.Printf("Skipping OCI validation for %s due to rate limiting", ociRef.String())
			return nil
		}
		return err
	}

	// Get config digest from manifest
	configDigest, err := getConfigDigestFromManifest(ctx, client, registryConfig, ociRef.Namespace, ociRef.Image, manifest)
	if err != nil {
		return err
	}

	// Validate server name annotation
	return validateServerNameAnnotation(ctx, client, registryConfig, ociRef.Namespace, ociRef.Image, ociRef.Tag, configDigest, serverName)
}

// validateRegistryURL validates that the registry base URL is supported
func validateRegistryURL(registryURL string) error {
	if registryURL != model.RegistryURLDocker && registryURL != model.RegistryURLGHCR {
		return fmt.Errorf("registry type and base URL do not match: '%s' is not valid for registry type '%s'. Expected: %s or %s",
			registryURL, model.RegistryTypeOCI, model.RegistryURLDocker, model.RegistryURLGHCR)
	}
	return nil
}

// fetchImageManifest fetches the OCI manifest for an image
func fetchImageManifest(ctx context.Context, client *http.Client, registryConfig *RegistryConfig, namespace, repo, tag string) (*OCIManifest, error) {
	manifestURL := fmt.Sprintf("%s/v2/%s/%s/manifests/%s", registryConfig.APIBaseURL, namespace, repo, tag)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create manifest request: %w", err)
	}

	// Get auth token if registry requires it
	if registryConfig.AuthURL != "" {
		token, err := getRegistryAuthToken(ctx, client, registryConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to authenticate with registry: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}

	req.Header.Set("Accept", "application/vnd.oci.image.index.v1+json,application/vnd.docker.distribution.manifest.list.v2+json,application/vnd.docker.distribution.manifest.v2+json,application/vnd.oci.image.manifest.v1+json")
	req.Header.Set("User-Agent", "MCP-Registry-Validator/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch OCI manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("OCI image '%s/%s:%s' not found (status: %d)", namespace, repo, tag, resp.StatusCode)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		// Rate limited, return explicit error
		log.Printf("Rate limited when accessing OCI image '%s/%s:%s'", namespace, repo, tag)
		return nil, fmt.Errorf("%w: %s/%s:%s", ErrRateLimited, namespace, repo, tag)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch OCI manifest (status: %d)", resp.StatusCode)
	}

	var manifest OCIManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("failed to parse OCI manifest: %w", err)
	}

	return &manifest, nil
}

// getConfigDigestFromManifest extracts the config digest from an OCI manifest
func getConfigDigestFromManifest(ctx context.Context, client *http.Client, registryConfig *RegistryConfig, namespace, repo string, manifest *OCIManifest) (string, error) {
	// Handle multi-arch images by using first manifest
	if len(manifest.Manifests) > 0 {
		// This is a multi-arch image, get the specific manifest
		specificManifest, err := getSpecificManifest(ctx, client, registryConfig, namespace, repo, manifest.Manifests[0].Digest)
		if err != nil {
			return "", fmt.Errorf("failed to get specific manifest: %w", err)
		}
		return specificManifest.Config.Digest, nil
	}

	// For single-arch images, validate we have a config digest
	if manifest.Config.Digest == "" {
		return "", fmt.Errorf("manifest missing config digest - invalid or corrupted manifest")
	}

	return manifest.Config.Digest, nil
}

// validateServerNameAnnotation validates the MCP server name annotation in the image config
func validateServerNameAnnotation(ctx context.Context, client *http.Client, registryConfig *RegistryConfig, namespace, repo, tag, configDigest, serverName string) error {
	// Get image config (contains labels)
	config, err := getImageConfig(ctx, client, registryConfig, namespace, repo, configDigest)
	if err != nil {
		return fmt.Errorf("failed to get image config: %w", err)
	}

	mcpName, exists := config.Config.Labels["io.modelcontextprotocol.server.name"]
	if !exists {
		return fmt.Errorf("OCI image '%s/%s:%s' is missing required annotation. Add this to your Dockerfile: LABEL io.modelcontextprotocol.server.name=\"%s\"", namespace, repo, tag, serverName)
	}

	if mcpName != serverName {
		return fmt.Errorf("OCI image ownership validation failed. Expected annotation 'io.modelcontextprotocol.server.name' = '%s', got '%s'", serverName, mcpName)
	}

	return nil
}

// getRegistryAuthToken retrieves an authentication token from a registry
func getRegistryAuthToken(ctx context.Context, client *http.Client, config *RegistryConfig) (string, error) {
	if config.AuthURL == "" {
		return "", nil // No auth required
	}

	authURL := fmt.Sprintf("%s?service=%s&scope=%s", config.AuthURL, config.Service, config.Scope)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, authURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create auth request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to request auth token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("auth request failed with status %d", resp.StatusCode)
	}

	var authResp OCIAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return "", fmt.Errorf("failed to parse auth response: %w", err)
	}

	return authResp.Token, nil
}

// getSpecificManifest retrieves a specific manifest for multi-arch images
func getSpecificManifest(ctx context.Context, client *http.Client, registryConfig *RegistryConfig, namespace, repo, digest string) (*OCIManifest, error) {
	manifestURL := fmt.Sprintf("%s/v2/%s/%s/manifests/%s", registryConfig.APIBaseURL, namespace, repo, digest)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create specific manifest request: %w", err)
	}

	// Get auth token if registry requires it
	if registryConfig.AuthURL != "" {
		token, err := getRegistryAuthToken(ctx, client, registryConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to authenticate with registry: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}

	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json")
	req.Header.Set("User-Agent", "MCP-Registry-Validator/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch specific manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("specific manifest not found (status: %d)", resp.StatusCode)
	}

	var manifest OCIManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("failed to parse specific manifest: %w", err)
	}

	return &manifest, nil
}

// getImageConfig retrieves the image configuration containing labels
func getImageConfig(ctx context.Context, client *http.Client, registryConfig *RegistryConfig, namespace, repo, configDigest string) (*OCIImageConfig, error) {
	configURL := fmt.Sprintf("%s/v2/%s/%s/blobs/%s", registryConfig.APIBaseURL, namespace, repo, configDigest)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, configURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create config request: %w", err)
	}

	// Get auth token if registry requires it
	if registryConfig.AuthURL != "" {
		token, err := getRegistryAuthToken(ctx, client, registryConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to authenticate with registry: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}

	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json")
	req.Header.Set("User-Agent", "MCP-Registry-Validator/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch image config: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("image config not found (status: %d)", resp.StatusCode)
	}

	var config OCIImageConfig
	if err := json.NewDecoder(resp.Body).Decode(&config); err != nil {
		return nil, fmt.Errorf("failed to parse image config: %w", err)
	}

	return &config, nil
}
