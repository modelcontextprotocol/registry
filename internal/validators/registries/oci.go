package registries

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/registry/pkg/model"
)

const (
	dockerIoApiBaseUrl = "https://registry-1.docker.io"
)

// OCIAuthResponse represents the Docker Hub authentication response
type OCIAuthResponse struct {
	Token string `json:"token"`
}

// OCIManifest represents an OCI image manifest
type OCIManifest struct {
	Annotations map[string]string `json:"annotations"`
}

// ValidateOCI validates that an OCI image contains the correct MCP server name annotation
func ValidateOCI(ctx context.Context, pkg model.Package, serverName string) error {
	client := &http.Client{Timeout: 10 * time.Second}

	// Parse image reference (namespace/repo or repo)
	parts := strings.Split(pkg.Identifier, "/")
	var namespace, repo string
	if len(parts) == 2 {
		namespace = parts[0]
		repo = parts[1]
	} else if len(parts) == 1 {
		namespace = "library"
		repo = pkg.Identifier
	} else {
		return fmt.Errorf("invalid image reference: %s", pkg.Identifier)
	}

	// Get image manifest
	tag := pkg.Version
	if tag == "" {
		tag = "latest"
	}

	apiBaseUrl := pkg.RegistryBaseURL
	if pkg.RegistryBaseURL == model.RegistryURLDocker || pkg.RegistryBaseURL == "" {
		// docker.io is an exceptional registry that was created before standardisation, so needs a custom API base url
		// https://github.com/containers/image/blob/5e4845dddd57598eb7afeaa6e0f4c76531bd3c91/docker/docker_client.go#L225-L229
		apiBaseUrl = dockerIoApiBaseUrl
	}

	manifestURL := fmt.Sprintf("%s/v2/%s/%s/manifests/%s", apiBaseUrl, namespace, repo, tag)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create manifest request: %w", err)
	}

	// Get auth token for docker.io
	// We only support auth for docker.io, other registries must allow unauthed requests
	if apiBaseUrl == dockerIoApiBaseUrl {
		token, err := getDockerIoAuthToken(ctx, client, namespace, repo)
		if err != nil {
			return fmt.Errorf("failed to authenticate with Docker registry: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}

	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json")
	req.Header.Set("User-Agent", "MCP-Registry-Validator/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch OCI manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("OCI image '%s/%s:%s' not found (status: %d)", namespace, repo, tag, resp.StatusCode)
	}

	var manifest OCIManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return fmt.Errorf("failed to parse OCI manifest: %w", err)
	}

	mcpName, exists := manifest.Annotations["io.modelcontextprotocol.server.name"]
	if !exists {
		return fmt.Errorf("OCI image '%s/%s:%s' is missing required annotation. Add this to your Dockerfile: LABEL io.modelcontextprotocol.server.name=\"%s\"", namespace, repo, tag, serverName)
	}

	if mcpName != serverName {
		return fmt.Errorf("OCI image ownership validation failed. Expected annotation 'io.modelcontextprotocol.server.name' = '%s', got '%s'", serverName, mcpName)
	}

	return nil
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
