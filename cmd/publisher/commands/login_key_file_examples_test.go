package commands_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoginKeyFileExamplesPreserveExistingFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the documented Bash examples use Unix file permissions")
	}
	_, err := exec.LookPath("bash")
	require.NoError(t, err)
	doc, err := os.ReadFile("../../../docs/reference/cli/commands.md")
	require.NoError(t, err)
	lines := strings.Split(string(doc), "\n")
	var examples []string
	for i, line := range lines {
		if strings.HasPrefix(line, "KEY_FILE=\"$(mktemp ") {
			require.Less(t, i+1, len(lines))
			examples = append(examples, line+"\n"+lines[i+1])
		}
	}
	require.Len(t, examples, 3, "cover Ed25519 DNS and both ECDSA file setup examples")
	for i, example := range examples {
		t.Run(fmt.Sprintf("example-%d", i+1), func(t *testing.T) {
			directory := t.TempDir()
			directoryRoot, err := os.OpenRoot(directory)
			require.NoError(t, err)
			defer directoryRoot.Close()
			// Include the formerly documented filename and an existing file in
			// the new prefix. Neither its contents nor permissions may change.
			for _, name := range []string{"key.hex", "mcp-key.existing"} {
				path := filepath.Join(directory, name)
				require.NoError(t, os.WriteFile(path, []byte("existing key fixture"), 0600))
				require.NoError(t, os.Chmod(path, 0644))
			}
			require.NoError(t, os.Symlink("key.hex", filepath.Join(directory, "mcp-key.link")))
			size := 32
			if strings.Contains(example, "openssl ec ") {
				size = 48
			}
			key := strings.Repeat("ab", size)
			fixture := "priv:\n    " + strings.Repeat("ab:", size-1) + "ab\npub:\n    01:02\n"
			// Replace only OpenSSL with a deterministic, non-secret producer;
			// execute the documentation's actual creation, extraction and write.
			script := example
			for _, producer := range []string{"openssl ec -in key.pem -noout -text", "openssl pkey -in key.pem -noout -text"} {
				script = strings.ReplaceAll(script, producer, "printf '%s' \"${TEST_PRIVATE_KEY_TEXT}\"")
			}
			var created []string
			for attempt := 0; attempt < 2; attempt++ {
				cmd := exec.Command("bash", "-e", "-u", "-o", "pipefail", "-c", script+"\nprintf '%s' \"${KEY_FILE}\"") //nolint:gosec // Executes repository examples with fixture input in an isolated directory.
				cmd.Dir = directory
				cmd.Env = append(os.Environ(), "TEST_PRIVATE_KEY_TEXT="+fixture)
				output, err := cmd.CombinedOutput()
				require.NoError(t, err, "%s", output)
				name := strings.TrimSpace(string(output))
				require.True(t, strings.HasPrefix(name, "./mcp-key."))
				require.NotContains(t, created, name, "each invocation must create a new file")
				created = append(created, name)
			}
			for _, name := range created {
				data, err := directoryRoot.ReadFile(name)
				require.NoError(t, err)
				require.Equal(t, key, string(data))
				info, err := directoryRoot.Stat(name)
				require.NoError(t, err)
				require.Equal(t, os.FileMode(0600), info.Mode().Perm())
			}
			for _, name := range []string{"key.hex", "mcp-key.existing"} {
				data, err := directoryRoot.ReadFile(name)
				require.NoError(t, err)
				require.Equal(t, "existing key fixture", string(data))
				info, err := directoryRoot.Stat(name)
				require.NoError(t, err)
				require.Equal(t, os.FileMode(0644), info.Mode().Perm())
			}
			target, err := os.Readlink(filepath.Join(directory, "mcp-key.link"))
			require.NoError(t, err)
			require.Equal(t, "key.hex", target)
		})
	}
}
