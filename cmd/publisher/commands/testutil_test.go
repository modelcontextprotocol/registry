package commands_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/registry/cmd/publisher/commands"
	"github.com/modelcontextprotocol/registry/internal/validators"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/stretchr/testify/require"
)

// SetupMockRegistryServer creates an httptest.Server that mocks the registry API
func SetupMockRegistryServer(t *testing.T, publishHandler func(w http.ResponseWriter, r *http.Request), validateHandler func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	// Default handlers if not provided
	if publishHandler == nil {
		publishHandler = func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
			response := apiv0.ServerResponse{
				Server: apiv0.ServerJSON{
					Name:    "com.example/test",
					Version: "1.0.0",
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		}
	}

	if validateHandler == nil {
		validateHandler = func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			result := validators.ValidationResult{Valid: true}
			_ = json.NewEncoder(w).Encode(result)
		}
	}

	mux.HandleFunc("/v0/publish", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		publishHandler(w, r)
	})

	mux.HandleFunc("/v0/validate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		validateHandler(w, r)
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return server
}

// SetupTestToken creates a token file pointing to the test server
func SetupTestToken(t *testing.T, registryURL, token string) string {
	t.Helper()

	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	tokenPath := filepath.Join(homeDir, commands.TokenFileName)
	tokenData := map[string]string{
		"token":    token,
		"registry": registryURL,
	}

	data, err := json.Marshal(tokenData)
	require.NoError(t, err)

	err = os.WriteFile(tokenPath, data, 0600)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = os.Remove(tokenPath)
	})

	return tokenPath
}

// CreateTestServerJSON creates a server.json file in a temp directory and changes to it
func CreateTestServerJSON(t *testing.T, serverJSON apiv0.ServerJSON) (string, string) {
	t.Helper()

	tempDir, err := os.MkdirTemp("", "mcp-publisher-test")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(tempDir) })

	jsonData, err := json.MarshalIndent(serverJSON, "", "  ")
	require.NoError(t, err)

	serverFile := filepath.Join(tempDir, "server.json")
	err = os.WriteFile(serverFile, jsonData, 0600)
	require.NoError(t, err)

	// Change to temp directory
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(originalDir) })

	err = os.Chdir(tempDir)
	require.NoError(t, err)

	return tempDir, serverFile
}
