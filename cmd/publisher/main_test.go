package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const argsEnv = "MCP_PUBLISHER_TEST_ARGS"

// TestMain re-enters main() when argsEnv is set, so tests can check real exit codes.
func TestMain(m *testing.M) {
	if args, ok := os.LookupEnv(argsEnv); ok {
		os.Args = append([]string{"mcp-publisher"}, strings.Fields(args)...)
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func runPublisher(t *testing.T, args string) (string, int) {
	t.Helper()
	cmd := exec.Command(os.Args[0]) //nolint:gosec // re-executes this test binary
	cmd.Env = append(os.Environ(), argsEnv+"="+args)
	output, err := cmd.CombinedOutput()

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return string(output), exitErr.ExitCode()
	}
	require.NoError(t, err)
	return string(output), 0
}

func TestPublishHelpAfterWaitPrintsUsageAndExitsZero(t *testing.T) {
	output, code := runPublisher(t, "publish --wait 1m --help")

	assert.Equal(t, 0, code)
	assert.Contains(t, output, "Flags (must come before positional arguments):")
	assert.Contains(t, output, "--wait <duration>")
}

func TestPublishFlagErrorsExitOne(t *testing.T) {
	tests := []struct {
		name string
		args string
		want string
	}{
		{"unknown flag", "publish --nope", "flag provided but not defined: -nope"},
		{"flag after path", "publish server.json --wait 1m", "flags must come before the path"},
		{"negative duration", "publish --wait -5s", "invalid --wait duration"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, code := runPublisher(t, tt.args)

			assert.Equal(t, 1, code)
			assert.Contains(t, output, tt.want)
		})
	}
}
