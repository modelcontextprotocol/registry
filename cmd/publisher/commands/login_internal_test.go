package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeKeyFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "key.hex")
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
	return path
}

func TestResolvePrivateKey(t *testing.T) {
	flagKey := "0101010101010101010101010101010101010101010101010101010101010101"
	envKey := "0202020202020202020202020202020202020202020202020202020202020202"
	fileKey := "0303030303030303030303030303030303030303030303030303030303030303"

	tests := []struct {
		name    string
		flag    string
		file    string
		env     string
		want    string
		wantErr string
	}{
		{name: "flag only", flag: flagKey, want: flagKey},
		{name: "file only", file: writeKeyFile(t, fileKey), want: fileKey},
		{name: "env only", env: envKey, want: envKey},
		{name: "flag beats env", flag: flagKey, env: envKey, want: flagKey},
		{name: "file beats env", file: writeKeyFile(t, fileKey), env: envKey, want: fileKey},
		{name: "file with trailing LF", file: writeKeyFile(t, fileKey+"\n"), want: fileKey},
		{name: "file with trailing CRLF", file: writeKeyFile(t, fileKey+"\r\n"), want: fileKey},
		{name: "env trimmed", env: "  " + envKey + "\n", want: envKey},
		{name: "nothing given", want: ""},
		{name: "empty file", file: writeKeyFile(t, "\n"), wantErr: "is empty"},
		{name: "empty file does not fall back to env", file: writeKeyFile(t, ""), env: envKey, wantErr: "is empty"},
		{
			name:    "both flags",
			flag:    flagKey,
			file:    writeKeyFile(t, fileKey),
			wantErr: "use --private-key or --private-key-file, not both",
		},
		{
			name:    "PEM file",
			file:    writeKeyFile(t, "-----BEGIN PUBLIC KEY-----\nMCowBQYDK2VwAyEA\n-----END PUBLIC KEY-----\n"),
			wantErr: "PEM is not supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(key string) string {
				if key == "MCP_PRIVATE_KEY" {
					return tt.env
				}
				return ""
			}
			got, err := resolvePrivateKey(tt.flag, tt.file, getenv)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolvePrivateKey_UnreadablePathNamesPath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.hex")
	dir := t.TempDir()

	for _, path := range []string{missing, dir} {
		_, err := resolvePrivateKey("", path, func(string) string { return "" })
		require.Error(t, err)
		assert.Contains(t, err.Error(), path)
	}
}

func TestCreateSigner_EmptyKeyKeepsRequiredError(t *testing.T) {
	t.Setenv("MCP_PRIVATE_KEY", "")
	_, err := createSigner(LoginFlags{SignerType: InProcessSignerType, CryptoAlgorithm: "ed25519"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "private key (hex) is required")
}
