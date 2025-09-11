package registries

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/registry/pkg/model"
)

const (
	dockerIoAPIBaseURL = "https://registry-1.docker.io"
	ghcrAPIBaseURL     = "https://ghcr.io"
)

// RegistryConfig holds configuration for different OCI registries
type RegistryConfig struct {
	RegistryURL    string
	APIBaseURL     string
	AuthFunc       func(ctx context.Context, client *http.Client, namespace, repo string) (string, error)
	RequiresAuth   bool
	AllowAnonymous bool
}

// registryConfigs maps registry URLs to their configurations
var registryConfigs = map[string]RegistryConfig{
	model.RegistryURLDocker: {
		RegistryURL:    model.RegistryURLDocker,
		APIBaseURL:     dockerIoAPIBaseURL,
		AuthFunc:       getDockerIoAuthToken,
		RequiresAuth:   true,
		AllowAnonymous: false,
	},
	model.RegistryURLGHCR: {
		RegistryURL:    model.RegistryURLGHCR,
		APIBaseURL:     ghcrAPIBaseURL,
		AuthFunc:       getGHCRAuthToken,
		RequiresAuth:   false,
		AllowAnonymous: true,
	},
}

// OCIAuthResponse represents the Docker Hub authentication response
type OCIAuthResponse struct {
	Token string `json:"token"`
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

// ValidateOCI validates that an OCI image contains the correct MCP server name annotation
func ValidateOCI(ctx context.Context, pkg model.Package, serverName string) error {
	// Set default registry base URL if empty
	if pkg.RegistryBaseURL == "" {
		pkg.RegistryBaseURL = model.RegistryURLDocker
	}

	// Get registry configuration
	config, err := GetRegistryConfig(pkg.RegistryBaseURL)
	if err != nil {
		return fmt.Errorf("registry type and base URL do not match: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}

	// Parse image reference (namespace/repo or repo)
	namespace, repo, err := parseImageReference(pkg.Identifier)
	if err != nil {
		return fmt.Errorf("invalid OCI image reference: %w", err)
	}

	apiBaseURL := config.APIBaseURL

	tag := pkg.Version
	manifestURL := fmt.Sprintf("%s/v2/%s/%s/manifests/%s", apiBaseURL, namespace, repo, tag)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create manifest request: %w", err)
	}

	// Authenticate request based on registry configuration
	if err := authenticateRequest(ctx, client, req, config, namespace, repo); err != nil {
		return err
	}

	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json,application/vnd.oci.image.manifest.v1+json")
	req.Header.Set("User-Agent", "MCP-Registry-Validator/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch OCI manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("OCI image '%s/%s:%s' not found (status: %d)", namespace, repo, tag, resp.StatusCode)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		// Rate limited, skip validation for now
		log.Printf("Warning: Rate limited when accessing OCI image '%s/%s:%s'. Skipping validation.", namespace, repo, tag)
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch OCI manifest (status: %d)", resp.StatusCode)
	}

	var manifest OCIManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return fmt.Errorf("failed to parse OCI manifest: %w", err)
	}

	// Handle multi-arch images by using first manifest
	var configDigest string
	if len(manifest.Manifests) > 0 {
		// This is a multi-arch image, get the specific manifest
		specificManifest, err := getSpecificManifest(ctx, client, config, namespace, repo, manifest.Manifests[0].Digest)
		if err != nil {
			return fmt.Errorf("failed to get specific manifest: %w", err)
		}
		configDigest = specificManifest.Config.Digest
	} else {
		configDigest = manifest.Config.Digest
	}

	if configDigest == "" {
		return fmt.Errorf("unable to determine image config digest for '%s/%s:%s'", namespace, repo, tag)
	}

	// Get image config (contains labels)
	imageConfig, err := getImageConfig(ctx, client, config, namespace, repo, configDigest)
	if err != nil {
		return fmt.Errorf("failed to get image config: %w", err)
	}

	mcpName, exists := imageConfig.Config.Labels["io.modelcontextprotocol.server.name"]
	if !exists {
		return fmt.Errorf("OCI image '%s/%s:%s' is missing required annotation. Add this to your Dockerfile: LABEL io.modelcontextprotocol.server.name=\"%s\"", namespace, repo, tag, serverName)
	}

	if mcpName != serverName {
		return fmt.Errorf("OCI image ownership validation failed. Expected annotation 'io.modelcontextprotocol.server.name' = '%s', got '%s'", serverName, mcpName)
	}

	return nil
}

func parseImageReference(identifier string) (string, string, error) {
	parts := strings.Split(identifier, "/")
	switch len(parts) {
	case 2:
		return parts[0], parts[1], nil
	case 1:
		return "library", parts[0], nil
	default:
		return "", "", fmt.Errorf("invalid image reference: %s", identifier)
	}
}

// getDockerIoAuthToken retrieves an authentication token from Docker Hub
func getDockerIoAuthToken(ctx context.Context, client *http.Client, namespace, repo string) (string, error) {
	authURL := fmt.Sprintf("https://auth.docker.io/token?service=registry.docker.io&scope=repository:%s/%s:pull", namespace, repo)

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

// getGHCRAuthToken retrieves an authentication token from GitHub Container Registry
func getGHCRAuthToken(ctx context.Context, client *http.Client, namespace, repo string) (string, error) {
	// GHCR uses the standard OCI distribution spec for authentication
	// For public repos, we can try without auth first, but for better rate limits
	// we can try to get a token using the GitHub API
	authURL := fmt.Sprintf("https://ghcr.io/token?service=ghcr.io&scope=repository:%s/%s:pull", namespace, repo)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, authURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create GHCR auth request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to request GHCR auth token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// For public repos, GHCR might not require auth, so return empty token
		if resp.StatusCode == http.StatusUnauthorized {
			return "", nil
		}
		return "", fmt.Errorf("GHCR auth request failed with status %d", resp.StatusCode)
	}

	var authResp OCIAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return "", fmt.Errorf("failed to parse GHCR auth response: %w", err)
	}

	return authResp.Token, nil
}

// getSpecificManifest retrieves a specific manifest for multi-arch images
func getSpecificManifest(ctx context.Context, client *http.Client, config RegistryConfig, namespace, repo, digest string) (*OCIManifest, error) {
	manifestURL := fmt.Sprintf("%s/v2/%s/%s/manifests/%s", config.APIBaseURL, namespace, repo, digest)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create specific manifest request: %w", err)
	}

	// Authenticate request based on registry configuration
	if err := authenticateRequest(ctx, client, req, config, namespace, repo); err != nil {
		return nil, err
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
func getImageConfig(ctx context.Context, client *http.Client, registryConfig RegistryConfig, namespace, repo, configDigest string) (*OCIImageConfig, error) {
	configURL := fmt.Sprintf("%s/v2/%s/%s/blobs/%s", registryConfig.APIBaseURL, namespace, repo, configDigest)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, configURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create config request: %w", err)
	}

	// Authenticate request based on registry configuration
	if err := authenticateRequest(ctx, client, req, registryConfig, namespace, repo); err != nil {
		return nil, err
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

	var imageConfig OCIImageConfig
	if err := json.NewDecoder(resp.Body).Decode(&imageConfig); err != nil {
		return nil, fmt.Errorf("failed to parse image config: %w", err)
	}

	return &imageConfig, nil
}

// GetRegistryConfig returns the configuration for a given registry URL
func GetRegistryConfig(registryURL string) (RegistryConfig, error) {
	config, exists := registryConfigs[registryURL]
	if !exists {
		return RegistryConfig{}, fmt.Errorf("unsupported registry URL: %s", registryURL)
	}
	return config, nil
}

// authenticateRequest adds authentication headers to a request based on registry configuration
func authenticateRequest(ctx context.Context, client *http.Client, req *http.Request, config RegistryConfig, namespace, repo string) error {
	if config.AuthFunc == nil {
		return nil // No authentication function defined
	}

	token, err := config.AuthFunc(ctx, client, namespace, repo)
	if err != nil {
		if config.RequiresAuth {
			return fmt.Errorf("failed to authenticate with %s registry: %w", config.RegistryURL, err)
		}
		// If auth is not required, log warning and continue
		if config.AllowAnonymous {
			log.Printf("Warning: Failed to authenticate with %s for '%s/%s': %v. Continuing without auth.", config.RegistryURL, namespace, repo, err)
		}
		return nil
	}

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	return nil
}
