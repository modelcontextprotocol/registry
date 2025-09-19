package registries

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
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

	// Validate that the registry base URL matches an allowed OCI registry base.
	// Currently support docker.io and ghcr.io (GitHub Container Registry) as first-class endpoints.
	allowed := map[string]struct{}{
		model.RegistryURLDocker: {},
		model.RegistryURLGHCR:   {},
	}
	if _, ok := allowed[pkg.RegistryBaseURL]; !ok {
		return fmt.Errorf("registry type and base URL do not match: '%s' is not valid for registry type '%s'. Expected one of: %s, %s",
			pkg.RegistryBaseURL, model.RegistryTypeOCI, model.RegistryURLDocker, model.RegistryURLGHCR)
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

	// Test-only override to enable mocked HTTP servers in unit tests.
	// If set, this replaces the computed apiBaseURL while keeping validation constraints on the public base URL intact.
	if override := os.Getenv("MCP_REGISTRY_OCI_TEST_BASE_URL"); override != "" {
		apiBaseURL = strings.TrimRight(override, "/")
	}

	// Attempt to load auth token from environment for private registries (GHCR or others) if needed.
	// We intentionally do this after setting apiBaseURL. For docker.io we keep existing anonymous flow unless token provided.
	authToken := lookupOCIAuthToken(pkg.RegistryBaseURL)

	// For GHCR: if we have a PAT and username, exchange for a repository-scoped Bearer token.
	if pkg.RegistryBaseURL == model.RegistryURLGHCR {
		if pat := authToken; pat != "" {
			if user := os.Getenv("MCP_REGISTRY_OCI_GHCR_USERNAME"); user != "" {
				if token, err := getGHCRAuthToken(ctx, client, namespace, repo, user, pat); err == nil && token != "" {
					authToken = token // replace with scoped Bearer token
				}
			}
		}
	}

	tag := pkg.Version
	manifestURL := fmt.Sprintf("%s/v2/%s/%s/manifests/%s", apiBaseURL, namespace, repo, tag)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create manifest request: %w", err)
	}

	// Acquire token for docker.io if none provided externally
	if apiBaseURL == dockerIoAPIBaseURL && authToken == "" {
		// anonymous flow for public images
		token, err := getDockerIoAuthToken(ctx, client, namespace, repo)
		if err != nil {
			return fmt.Errorf("failed to authenticate with Docker registry: %w", err)
		}
		authToken = token
	}
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}

	// Accept both single-arch and multi-arch (index) manifest media types
	req.Header.Set("Accept", strings.Join([]string{
		"application/vnd.docker.distribution.manifest.v2+json",
		"application/vnd.docker.distribution.manifest.list.v2+json",
		"application/vnd.oci.image.manifest.v1+json",
		"application/vnd.oci.image.index.v1+json",
	}, ","))
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
		if resp.StatusCode == http.StatusForbidden {
			return fmt.Errorf("failed to fetch OCI manifest (status: 403). If using GHCR, ensure your token has packages:read and access to the repository (org membership or visibility). For private GHCR images, you can also set MCP_REGISTRY_OCI_GHCR_USERNAME to use Basic auth with your PAT.")
		}
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
		specificManifest, err := getSpecificManifest(ctx, client, apiBaseURL, namespace, repo, manifest.Manifests[0].Digest, authToken)
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
	config, err := getImageConfig(ctx, client, apiBaseURL, namespace, repo, configDigest, authToken)
	if err != nil {
		return fmt.Errorf("failed to get image config: %w", err)
	}

	// Optional test-only bypass for label validation
	if os.Getenv("MCP_REGISTRY_OCI_SKIP_LABEL_VALIDATION") != "1" {
		mcpName, exists := config.Config.Labels["io.modelcontextprotocol.server.name"]
		if !exists {
			return fmt.Errorf("OCI image '%s/%s:%s' is missing required annotation. Add this to your Dockerfile: LABEL io.modelcontextprotocol.server.name=\"%s\"", namespace, repo, tag, serverName)
		}

		if mcpName != serverName {
			return fmt.Errorf("OCI image ownership validation failed. Expected annotation 'io.modelcontextprotocol.server.name' = '%s', got '%s'", serverName, mcpName)
		}
	}

	return nil
}

func parseImageReference(identifier string) (string, string, error) {
	parts := strings.Split(identifier, "/")
	switch len(parts) {
	case 1:
		return "library", parts[0], nil
	case 2:
		return parts[0], parts[1], nil
	default:
		// Support multi-segment repository paths: first segment as namespace, rest as repo path
		return parts[0], strings.Join(parts[1:], "/"), nil
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
func getSpecificManifest(ctx context.Context, client *http.Client, apiBaseURL, namespace, repo, digest string, authToken string) (*OCIManifest, error) {
	manifestURL := fmt.Sprintf("%s/v2/%s/%s/manifests/%s", apiBaseURL, namespace, repo, digest)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create specific manifest request: %w", err)
	}

	// Reuse provided auth for private registries (e.g., GHCR)
	applyAuthHeader(req, apiBaseURL, authToken)

	// Get auth token for docker.io
	if apiBaseURL == dockerIoAPIBaseURL && tokenHeaderNotPresent(req.Header) {
		// Only try anonymous token flow if not already supplied
		token, err := getDockerIoAuthToken(ctx, client, namespace, repo)
		if err != nil {
			return nil, fmt.Errorf("failed to authenticate with Docker registry: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}

	req.Header.Set("Accept", strings.Join([]string{
		"application/vnd.oci.image.manifest.v1+json",
		"application/vnd.docker.distribution.manifest.v2+json",
	}, ","))
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
func getImageConfig(ctx context.Context, client *http.Client, apiBaseURL, namespace, repo, configDigest string, authToken string) (*OCIImageConfig, error) {
	configURL := fmt.Sprintf("%s/v2/%s/%s/blobs/%s", apiBaseURL, namespace, repo, configDigest)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, configURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create config request: %w", err)
	}

	// Reuse provided auth for private registries (e.g., GHCR)
	applyAuthHeader(req, apiBaseURL, authToken)

	// Get auth token for docker.io
	if apiBaseURL == dockerIoAPIBaseURL && tokenHeaderNotPresent(req.Header) {
		token, err := getDockerIoAuthToken(ctx, client, namespace, repo)
		if err != nil {
			return nil, fmt.Errorf("failed to authenticate with Docker registry: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}

	req.Header.Set("Accept", strings.Join([]string{
		"application/vnd.oci.image.config.v1+json",
		"application/vnd.docker.container.image.v1+json",
		"application/octet-stream",
	}, ","))
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

// lookupOCIAuthToken tries to find a token for a given registry base URL via environment variables.
// Priority:
//  1. Specific host variable: MCP_REGISTRY_OCI_TOKEN_<UPPER_HOST_WITH_DOTS_REPLACED_BY_UNDERSCORES>
//     e.g. ghcr.io -> MCP_REGISTRY_OCI_TOKEN_GHCR_IO
//  2. Generic mapping variable: MCP_REGISTRY_OCI_REGISTRY_AUTH (host=token,...)
//  3. GitHub Actions default: use GITHUB_TOKEN if registry is ghcr.io
func lookupOCIAuthToken(registryBaseURL string) string {
	if registryBaseURL == "" {
		return ""
	}
	// Extract host
	trimmed := strings.TrimPrefix(registryBaseURL, "https://")
	trimmed = strings.TrimPrefix(trimmed, "http://")
	host := strings.TrimRight(trimmed, "/")
	if host == "" {
		return ""
	}
	envKey := "MCP_REGISTRY_OCI_TOKEN_" + strings.ToUpper(strings.ReplaceAll(host, ".", "_"))
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	if mapping := os.Getenv("MCP_REGISTRY_OCI_REGISTRY_AUTH"); mapping != "" {
		pairs := strings.Split(mapping, ",")
		for _, p := range pairs {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			kv := strings.SplitN(p, "=", 2)
			if len(kv) != 2 {
				continue
			}
			if strings.TrimSpace(kv[0]) == host {
				return strings.TrimSpace(kv[1])
			}
		}
	}
	// GitHub Actions convenience: use ephemeral token if present for ghcr.io
	if host == "ghcr.io" {
		if ghToken := os.Getenv("GITHUB_TOKEN"); ghToken != "" {
			return ghToken
		}
	}
	return ""
}

func tokenHeaderNotPresent(h http.Header) bool {
	return h.Get("Authorization") == ""
}

// applyAuthHeader sets appropriate Authorization header for the request based on registry.
// For GHCR, supports PAT via Bearer or Basic (when MCP_REGISTRY_OCI_GHCR_USERNAME is set).
func applyAuthHeader(req *http.Request, apiBaseURL, authToken string) {
	// Do not override if already set (e.g., we may already have a repository-scoped Bearer token)
	if req.Header.Get("Authorization") != "" {
		return
	}
	u, err := url.Parse(apiBaseURL)
	host := apiBaseURL
	if err == nil && u.Host != "" {
		host = u.Host
	}

	// Prefer explicit token if provided
	if authToken != "" {
		if strings.Contains(host, "ghcr.io") {
			// Use Basic only when the token looks like a GitHub PAT and username is provided; else prefer Bearer.
			tokenLooksLikePAT := strings.HasPrefix(authToken, "ghp_") ||
				strings.HasPrefix(authToken, "github_pat_") ||
				strings.HasPrefix(authToken, "gho_") ||
				strings.HasPrefix(authToken, "ghu_") ||
				strings.HasPrefix(authToken, "ghs_") ||
				strings.HasPrefix(authToken, "ghr_")

			if tokenLooksLikePAT {
				if user := os.Getenv("MCP_REGISTRY_OCI_GHCR_USERNAME"); user != "" {
					// Basic auth header value
					creds := user + ":" + authToken
					b64 := base64Std(creds)
					req.Header.Set("Authorization", "Basic "+b64)
					return
				}
			}
			// Default to Bearer for GHCR when not using PAT Basic
			req.Header.Set("Authorization", "Bearer "+authToken)
			return
		}
		// Default to Bearer token
		req.Header.Set("Authorization", "Bearer "+authToken)
		return
	}
}

// base64Std performs standard base64 encoding without importing encoding/base64 repeatedly in call sites.
func base64Std(s string) string {
	// Inline encoding to avoid extra imports in multiple places
	// Use the standard library here
	return base64Encode([]byte(s))
}

// separate function so we can mock/override in tests if needed
func base64Encode(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

// getGHCRAuthToken exchanges username+PAT for a repository-scoped Bearer token from GHCR.
func getGHCRAuthToken(ctx context.Context, client *http.Client, namespace, repo, username, pat string) (string, error) {
	v := url.Values{}
	v.Set("service", "ghcr.io")
	v.Set("scope", fmt.Sprintf("repository:%s/%s:pull", namespace, repo))
	tokenURL := fmt.Sprintf("https://ghcr.io/token?%s", v.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL, nil)
	if err != nil {
		return "", err
	}
	// Use Basic auth with PAT
	creds := base64Std(username + ":" + pat)
	req.Header.Set("Authorization", "Basic "+creds)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ghcr token exchange failed with status %d", resp.StatusCode)
	}
	var auth OCIAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&auth); err != nil {
		return "", err
	}
	return auth.Token, nil
}
