package commands_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/registry/cmd/publisher/commands"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitCommand_PackageJSON(t *testing.T) {
	tests := []struct {
		name            string
		pkgJSON         string
		expectedName    string
		expectedVersion string
	}{
		{
			name:            "mcpName is returned as-is without transformation",
			pkgJSON:         `{"name": "@acme/weather", "mcpName": "io.github.acme/weather-mcp", "version": "2.3.4", "repository": "https://gitlab.com/acme/weather"}`,
			expectedName:    "io.github.acme/weather-mcp",
			expectedVersion: "2.3.4",
		},
		{
			name:            "empty mcpName falls back to scoped name transformation",
			pkgJSON:         `{"name": "@acme/weather", "mcpName": "", "version": "1.2.3", "repository": "https://gitlab.com/acme/weather"}`,
			expectedName:    "io.github.acme/weather",
			expectedVersion: "1.2.3",
		},
		{
			name:            "missing mcpName falls back to scoped name transformation",
			pkgJSON:         `{"name": "@acme/weather", "version": "1.2.3", "repository": "https://gitlab.com/acme/weather"}`,
			expectedName:    "io.github.acme/weather",
			expectedVersion: "1.2.3",
		},
		{
			name:            "missing version falls back to 1.0.0",
			pkgJSON:         `{"name": "@acme/weather", "mcpName": "io.github.acme/weather", "repository": "https://gitlab.com/acme/weather"}`,
			expectedName:    "io.github.acme/weather",
			expectedVersion: "1.0.0",
		},
		{
			name:            "mcpName takes precedence over GitHub repository URL",
			pkgJSON:         `{"name": "weather", "mcpName": "io.github.acme/weather-mcp", "version": "1.0.0", "repository": "https://github.com/someone-else/some-repo"}`,
			expectedName:    "io.github.acme/weather-mcp",
			expectedVersion: "1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := withIsolatedPackageJSON(t, tt.pkgJSON)

			require.NoError(t, commands.InitCommand())

			data, err := os.ReadFile(filepath.Join(dir, "server.json"))
			require.NoError(t, err)

			var got apiv0.ServerJSON
			require.NoError(t, json.Unmarshal(data, &got))

			assert.Equal(t, tt.expectedName, got.Name)
			assert.Equal(t, tt.expectedVersion, got.Version)
			require.Len(t, got.Packages, 1)
			assert.Equal(t, tt.expectedVersion, got.Packages[0].Version,
				"package version should match server version")
		})
	}
}

// withIsolatedPackageJSON creates a temp dir outside any git repository,
// writes package.json with the given contents, chdirs into it, and returns
// the directory path. The HOME override prevents init from detecting the
// host repo's git state.
func withIsolatedPackageJSON(t *testing.T, contents string) string {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(contents), 0600))

	originalDir, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(originalDir) })

	require.NoError(t, os.Chdir(dir))

	return dir
}
