package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/registry/cmd/publisher/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGitHubATProvider_Name(t *testing.T) {
	p := auth.NewGitHubATProvider("https://registry.example.com", "")
	assert.Equal(t, "github", p.Name())
}

func TestNewGitHubATProvider_WithEnvToken(t *testing.T) {
	t.Setenv("MCP_GITHUB_TOKEN", "env-token")

	registry := newMockExchangeServer(t, "env-token")

	p := auth.NewGitHubATProvider(registry.URL, "")

	// Login should use the env var token
	err := p.Login(context.Background())
	require.NoError(t, err)

	// GetToken should exchange it successfully
	token, err := p.GetToken(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "registry-jwt", token)
}

func TestNewGitHubATProvider_ExplicitTokenTakesPrecedence(t *testing.T) {
	t.Setenv("MCP_GITHUB_TOKEN", "env-token")

	registry := newMockExchangeServer(t, "explicit-token")

	p := auth.NewGitHubATProvider(registry.URL, "explicit-token")

	err := p.Login(context.Background())
	require.NoError(t, err)

	token, err := p.GetToken(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "registry-jwt", token)
}

func TestGitHubATProvider_LoginWithProvidedToken(t *testing.T) {
	registry := newMockExchangeServer(t, "my-token")

	p := auth.NewGitHubATProvider(registry.URL, "my-token")

	err := p.Login(context.Background())
	require.NoError(t, err)

	// Verify the token exchange works (proves Login stored the token)
	token, err := p.GetToken(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "registry-jwt", token)
}

func TestGitHubATProvider_LoginDoesNotWriteFiles(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)

	p := auth.NewGitHubATProvider("https://registry.example.com", "my-token")

	err = p.Login(context.Background())
	require.NoError(t, err)

	// Verify no .mcpregistry_* files were created in cwd
	for _, name := range []string{".mcpregistry_github_token", ".mcpregistry_registry_token"} {
		_, statErr := os.Stat(filepath.Join(cwd, name))
		assert.True(t, os.IsNotExist(statErr), "Login should not create %s in cwd", name)
	}
}

func TestGitHubATProvider_GetTokenWithoutLogin(t *testing.T) {
	p := auth.NewGitHubATProvider("https://registry.example.com", "my-token")

	// GetToken without Login should fail
	_, err := p.GetToken(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no GitHub token available")
}

func TestGitHubATProvider_GetTokenClearsTokenAfterExchange(t *testing.T) {
	registry := newMockExchangeServer(t, "my-token")

	p := auth.NewGitHubATProvider(registry.URL, "my-token")

	err := p.Login(context.Background())
	require.NoError(t, err)

	// First GetToken should succeed
	token, err := p.GetToken(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "registry-jwt", token)

	// Second GetToken should fail (GitHub token was cleared after exchange)
	_, err = p.GetToken(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no GitHub token available")
}

func TestGitHubATProvider_GetTokenClearsTokenOnError(t *testing.T) {
	// Mock registry that always returns an error
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	t.Cleanup(registry.Close)

	p := auth.NewGitHubATProvider(registry.URL, "my-token")

	err := p.Login(context.Background())
	require.NoError(t, err)

	// GetToken should fail
	_, err = p.GetToken(context.Background())
	require.Error(t, err)

	// Token should be cleared — second call also fails with "no GitHub token"
	_, err = p.GetToken(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no GitHub token available")
}

func TestGitHubATProvider_GetTokenExchangeFailure_NoURL(t *testing.T) {
	p := auth.NewGitHubATProvider("", "my-token")

	err := p.Login(context.Background())
	require.NoError(t, err)

	_, err = p.GetToken(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to exchange token")
}

func TestGitHubATProvider_LoginDeviceFlow_FetchesClientID(t *testing.T) {
	// Mock registry health endpoint
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/health" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status":           "ok",
				"github_client_id": "test-client-id",
			})
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(registry.Close)

	p := auth.NewGitHubATProvider(registry.URL, "")

	// Login without a provided token triggers device flow, which calls
	// the real GitHub device code URL. This will fail, but only after
	// fetching the client ID from our mock health endpoint.
	err := p.Login(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error requesting device code")
}

func TestGitHubATProvider_ImplementsProviderInterface(_ *testing.T) {
	// Compile-time check
	var _ = auth.NewGitHubATProvider("https://example.com", "token")
}

// newMockExchangeServer creates an httptest.Server that mocks the registry's
// token exchange endpoint. It accepts a specific GitHub token and returns
// "registry-jwt" as the registry token.
func newMockExchangeServer(t *testing.T, expectedGitHubToken string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/auth/github-at" && r.Method == http.MethodPost {
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			if body["github_token"] == expectedGitHubToken {
				w.Header().Set("Content-Type", "application/json")
				resp := map[string]interface{}{
					"registry_token": "registry-jwt",
					"expires_at":     9999999999,
				}
				_ = json.NewEncoder(w).Encode(resp)
				return
			}
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	return server
}
