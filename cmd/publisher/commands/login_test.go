package commands_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
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

func TestLoginCommand_DNSPrivateKeyFromFile(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("MCP_PRIVATE_KEY", "")

	seedHex, pub := newTestEd25519Key(t)
	server := setupDNSAuthServer(t, pub)

	keyPath := filepath.Join(t.TempDir(), "key.hex")
	require.NoError(t, os.WriteFile(keyPath, []byte(seedHex+"\n"), 0600))

	err := commands.LoginCommand([]string{"dns", "--registry", server.URL, "--domain", "example.com", "--private-key-file", keyPath})
	require.NoError(t, err)
	assertTokenSaved(t, tempHome, "dns")
}

func TestLoginCommand_DNSPrivateKeyFromEnv(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	seedHex, pub := newTestEd25519Key(t)
	server := setupDNSAuthServer(t, pub)
	t.Setenv("MCP_PRIVATE_KEY", seedHex)

	err := commands.LoginCommand([]string{"dns", "--registry", server.URL, "--domain", "example.com"})
	require.NoError(t, err)
	assertTokenSaved(t, tempHome, "dns")
}

func newTestEd25519Key(t *testing.T) (string, ed25519.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	return hex.EncodeToString(priv.Seed()), pub
}

func assertTokenSaved(t *testing.T, home, method string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, ".config", "mcp-publisher", "token.json"))
	require.NoError(t, err)
	var tokenInfo map[string]string
	require.NoError(t, json.Unmarshal(data, &tokenInfo))
	assert.Equal(t, method, tokenInfo["method"])
	assert.Equal(t, "test-registry-jwt", tokenInfo["token"])
}

// setupDNSAuthServer creates a mock registry that handles the "dns" auth flow
// and only issues a token when the timestamp is signed by pub.
func setupDNSAuthServer(t *testing.T, pub ed25519.PublicKey) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v0/auth/dns", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Domain          string `json:"domain"`
			Timestamp       string `json:"timestamp"`
			SignedTimestamp string `json:"signed_timestamp"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		sig, err := hex.DecodeString(req.SignedTimestamp)
		if err != nil || req.Domain != "example.com" || !ed25519.Verify(pub, []byte(req.Timestamp), sig) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
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
