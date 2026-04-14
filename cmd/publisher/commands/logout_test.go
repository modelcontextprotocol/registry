package commands_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/registry/cmd/publisher/commands"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogoutCommand_RemovesNewToken(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	// Create token at new location
	dir := filepath.Join(tempHome, ".config", "mcp-publisher")
	require.NoError(t, os.MkdirAll(dir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "token.json"), []byte(`{"token":"t"}`), 0600))

	err := commands.LogoutCommand()
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(dir, "token.json"))
	assert.True(t, os.IsNotExist(err))
}

func TestLogoutCommand_RemovesLegacyToken(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	// Create token at legacy location only
	require.NoError(t, os.WriteFile(filepath.Join(tempHome, ".mcp_publisher_token"), []byte(`{"token":"t"}`), 0600))

	err := commands.LogoutCommand()
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(tempHome, ".mcp_publisher_token"))
	assert.True(t, os.IsNotExist(err))
}

func TestLogoutCommand_RemovesBothOldAndNew(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	// Create tokens at both locations
	dir := filepath.Join(tempHome, ".config", "mcp-publisher")
	require.NoError(t, os.MkdirAll(dir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "token.json"), []byte(`{"token":"new"}`), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(tempHome, ".mcp_publisher_token"), []byte(`{"token":"old"}`), 0600))

	err := commands.LogoutCommand()
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(dir, "token.json"))
	assert.True(t, os.IsNotExist(err), "new token should be removed")

	_, err = os.Stat(filepath.Join(tempHome, ".mcp_publisher_token"))
	assert.True(t, os.IsNotExist(err), "legacy token should be removed")
}

func TestLogoutCommand_CleansUpCwdLegacyFiles(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	// Create a token so logout doesn't say "Not logged in"
	dir := filepath.Join(tempHome, ".config", "mcp-publisher")
	require.NoError(t, os.MkdirAll(dir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "token.json"), []byte(`{"token":"t"}`), 0600))

	// Create legacy intermediate files in a temp cwd
	tempCwd := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(tempCwd))

	require.NoError(t, os.WriteFile(".mcpregistry_github_token", []byte("gh-token"), 0600))
	require.NoError(t, os.WriteFile(".mcpregistry_registry_token", []byte("reg-token"), 0600))

	err = commands.LogoutCommand()
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(tempCwd, ".mcpregistry_github_token"))
	assert.True(t, os.IsNotExist(err), ".mcpregistry_github_token should be removed from cwd")

	_, err = os.Stat(filepath.Join(tempCwd, ".mcpregistry_registry_token"))
	assert.True(t, os.IsNotExist(err), ".mcpregistry_registry_token should be removed from cwd")
}

func TestLogoutCommand_CleansUpHomeLegacyFiles(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	// Create a token so logout doesn't say "Not logged in"
	dir := filepath.Join(tempHome, ".config", "mcp-publisher")
	require.NoError(t, os.MkdirAll(dir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "token.json"), []byte(`{"token":"t"}`), 0600))

	// Create legacy intermediate files in $HOME
	require.NoError(t, os.WriteFile(filepath.Join(tempHome, ".mcpregistry_github_token"), []byte("gh-token"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(tempHome, ".mcpregistry_registry_token"), []byte("reg-token"), 0600))

	err := commands.LogoutCommand()
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(tempHome, ".mcpregistry_github_token"))
	assert.True(t, os.IsNotExist(err), ".mcpregistry_github_token should be removed from $HOME")

	_, err = os.Stat(filepath.Join(tempHome, ".mcpregistry_registry_token"))
	assert.True(t, os.IsNotExist(err), ".mcpregistry_registry_token should be removed from $HOME")
}

func TestLogoutCommand_NotLoggedIn(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	// No token files exist anywhere
	err := commands.LogoutCommand()
	// Should not error, just print "Not logged in"
	require.NoError(t, err)
}
