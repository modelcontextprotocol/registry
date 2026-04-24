package commands_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/registry/cmd/publisher/commands"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoginCommand_WritesTokenToConfigDir(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	// Mock registry that returns a token for "none" auth
	server := setupNoneAuthServer(t)

	err := commands.LoginCommand([]string{"none", "--registry", server.URL})
	require.NoError(t, err)

	// Verify token written to new location
	tokenPath := filepath.Join(tempHome, ".config", "mcp-publisher", "token.json")
	data, err := os.ReadFile(tokenPath)
	require.NoError(t, err)

	var tokenInfo map[string]string
	require.NoError(t, json.Unmarshal(data, &tokenInfo))

	assert.Equal(t, "none", tokenInfo["method"])
	assert.Equal(t, server.URL, tokenInfo["registry"])
	assert.NotEmpty(t, tokenInfo["token"])
}

func TestLoginCommand_CreatesDirectoryWithCorrectPerms(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	server := setupNoneAuthServer(t)

	err := commands.LoginCommand([]string{"none", "--registry", server.URL})
	require.NoError(t, err)

	// Verify directory permissions
	dir := filepath.Join(tempHome, ".config", "mcp-publisher")
	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0700), info.Mode().Perm())

	// Verify file permissions
	tokenPath := filepath.Join(dir, "token.json")
	info, err = os.Stat(tokenPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestLoginCommand_DoesNotWriteLegacyFiles(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	server := setupNoneAuthServer(t)

	err := commands.LoginCommand([]string{"none", "--registry", server.URL})
	require.NoError(t, err)

	// Verify no legacy file at $HOME
	_, err = os.Stat(filepath.Join(tempHome, ".mcp_publisher_token"))
	assert.True(t, os.IsNotExist(err), "should not create legacy ~/.mcp_publisher_token")

	// Verify no .mcpregistry_* files in cwd
	cwd, err := os.Getwd()
	require.NoError(t, err)
	for _, name := range []string{".mcpregistry_github_token", ".mcpregistry_registry_token"} {
		_, err := os.Stat(filepath.Join(cwd, name))
		assert.True(t, os.IsNotExist(err), "should not create %s in cwd", name)
	}
}

// setupNoneAuthServer creates a mock registry that handles the "none" auth flow.
func setupNoneAuthServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v0/auth/none", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"registry_token": "test-registry-jwt",
			"expires_at":     9999999999,
		})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}
