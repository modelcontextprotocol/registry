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
)

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

	// Validate that the registry base URL matches OCI/Docker exactly
	if pkg.RegistryBaseURL != model.RegistryURLDocker {
		return fmt.Errorf("registry type and base URL do not match: '%s' is not valid for registry type '%s'. Expected: %s",
			pkg.RegistryBaseURL, model.RegistryTypeOCI, model.RegistryURLDocker)
	}

	client := &http.Client{Timeout: 10 * time.Second}

	// Parse image reference (namespace/repo or repo)
	namespace, repo, err := parseImageReference(pkg.Identifier)
	if err != nil {
		return fmt.Errorf("invalid OCI image reference: %w", err)
	}

	apiBaseURL := pkg.RegistryBaseURL
	if pkg.RegistryBaseURL == model.RegistryURLDocker {
		// docker.io is an exceptional registry that was created before standardisation, so needs a custom API base url
		// https://github.com/containers/image/blob/5e4845dddd57598eb7afeaa6e0f4c76531bd3c91/docker/docker_client.go#L225-L229
		apiBaseURL = dockerIoAPIBaseURL
	}

	tag := pkg.Version
	manifestURL := fmt.Sprintf("%s/v2/%s/%s/manifests/%s", apiBaseURL, namespace, repo, tag)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create manifest request: %w", err)
	}

	// Get auth token for docker.io
	// We only support auth for docker.io, other registries must allow unauthed requests
	if apiBaseURL == dockerIoAPIBaseURL {
		token, err := getDockerIoAuthToken(ctx, client, namespace, repo)
		if err != nil {
			return fmt.Errorf("failed to authenticate with Docker registry: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
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
		specificManifest, err := getSpecificManifest(ctx, client, apiBaseURL, namespace, repo, manifest.Manifests[0].Digest)
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
	config, err := getImageConfig(ctx, client, apiBaseURL, namespace, repo, configDigest)
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

// getSpecificManifest retrieves a specific manifest for multi-arch images
func getSpecificManifest(ctx context.Context, client *http.Client, apiBaseURL, namespace, repo, digest string) (*OCIManifest, error) {
	manifestURL := fmt.Sprintf("%s/v2/%s/%s/manifests/%s", apiBaseURL, namespace, repo, digest)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create specific manifest request: %w", err)
	}

	// Get auth token for docker.io
	if apiBaseURL == dockerIoAPIBaseURL {
		token, err := getDockerIoAuthToken(ctx, client, namespace, repo)
		if err != nil {
			return nil, fmt.Errorf("failed to authenticate with Docker registry: %w", err)
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
func getImageConfig(ctx context.Context, client *http.Client, apiBaseURL, namespace, repo, configDigest string) (*OCIImageConfig, error) {
	configURL := fmt.Sprintf("%s/v2/%s/%s/blobs/%s", apiBaseURL, namespace, repo, configDigest)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, configURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create config request: %w", err)
	}

	// Get auth token for docker.io
	if apiBaseURL == dockerIoAPIBaseURL {
		token, err := getDockerIoAuthToken(ctx, client, namespace, repo)
		if err != nil {
			return nil, fmt.Errorf("failed to authenticate with Docker registry: %w", err)
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
