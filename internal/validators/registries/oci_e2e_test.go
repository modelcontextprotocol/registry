package registries_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	registries "github.com/modelcontextprotocol/registry/internal/validators/registries"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// TestOCI_E2E_RunContainer_GHCR performs an end-to-end validation:
// 1) Validate image labels via ValidateOCI
// 2) docker login (if creds provided), pull, run the container briefly, and stop it
// This test only runs when explicitly enabled via OCI_E2E_RUN=1 and required env vars.
func TestOCI_E2E_RunContainer_GHCR(t *testing.T) {
	if os.Getenv("OCI_E2E_RUN") != "1" {
		t.Skip("Skipping E2E container run; set OCI_E2E_RUN=1 to enable")
	}

	params := loadE2EParamsOrSkip(t)
	validateOCIOrSkip(t, params)
	ensureDockerOrSkip(t)
	dockerLoginIfNeeded(t)

	fullRef := fmt.Sprintf("ghcr.io/%s:%s", params.image, params.tag)
	dockerPullOrSkip(t, fullRef)

	name, startedID := dockerRunContainer(t, fullRef)
	defer func() { _, _ = runDocker(context.Background(), 15*time.Second, "rm", "-f", name) }()

	waitBriefly()
	ensureRunningOrSkip(t, name, startedID)
	execSystemInfoAndAssertUbuntu(t, name)
	stopContainerIgnoreMissing(t, name)
}

type e2eParams struct {
	image     string
	tag       string
	server    string
	skipLabel bool
}

func loadE2EParamsOrSkip(t *testing.T) e2eParams {
	t.Helper()
	image := os.Getenv("GHCR_TEST_IMAGE")
	tag := os.Getenv("GHCR_TEST_TAG")
	server := os.Getenv("GHCR_TEST_SERVER_NAME")
	skipLabel := os.Getenv("MCP_REGISTRY_OCI_SKIP_LABEL_VALIDATION") == "1"

	if image == "" || tag == "" {
		t.Skip("Skipping E2E test; set GHCR_TEST_IMAGE and GHCR_TEST_TAG to run")
	}
	if !skipLabel && server == "" {
		t.Skip("Skipping E2E test; set GHCR_TEST_SERVER_NAME or MCP_REGISTRY_OCI_SKIP_LABEL_VALIDATION=1")
	}
	if skipLabel && server == "" {
		server = "ignored-when-skipping"
	}
	return e2eParams{image: image, tag: tag, server: server, skipLabel: skipLabel}
}

func validateOCIOrSkip(t *testing.T, p e2eParams) {
	t.Helper()
	pkg := model.Package{
		RegistryType:    model.RegistryTypeOCI,
		RegistryBaseURL: model.RegistryURLGHCR,
		Identifier:      p.image,
		Version:         p.tag,
	}
	if err := registries.ValidateOCI(context.Background(), pkg, p.server); err != nil {
		// If unauthorized and no token provided, skip instead of failing
		if os.Getenv("MCP_REGISTRY_OCI_TOKEN_GHCR_IO") == "" && os.Getenv("MCP_REGISTRY_OCI_REGISTRY_AUTH") == "" && os.Getenv("GITHUB_TOKEN") == "" {
			if err != nil && (strings.Contains(err.Error(), "status: 401") || strings.Contains(err.Error(), "unauthorized")) {
				t.Skipf("Skipping E2E due to unauthorized access without token: %v", err)
			}
		}
		t.Fatalf("E2E validation (manifest/config) failed: %v", err)
	}
}

func ensureDockerOrSkip(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Skipping E2E: docker CLI not found in PATH")
	}
	if out, err := runDocker(context.Background(), 15*time.Second, "info"); err != nil {
		t.Skipf("Skipping E2E: docker daemon not available: %v (%s)", err, out)
	}
}

func dockerLoginIfNeeded(t *testing.T) {
	t.Helper()
	user := os.Getenv("MCP_REGISTRY_OCI_GHCR_USERNAME")
	token := os.Getenv("MCP_REGISTRY_OCI_TOKEN_GHCR_IO")
	if user == "" || token == "" {
		return
	}
	if out, err := runDockerWithInput(context.Background(), 15*time.Second, token, "login", "ghcr.io", "-u", user, "--password-stdin"); err != nil {
		t.Fatalf("docker login failed: %v (%s)", err, out)
	}
	t.Cleanup(func() {
		_, _ = runDocker(context.Background(), 10*time.Second, "logout", "ghcr.io")
	})
}

func dockerPullOrSkip(t *testing.T, fullRef string) {
	t.Helper()
	if out, err := runDocker(context.Background(), 2*time.Minute, "pull", fullRef); err != nil {
		lower := strings.ToLower(out)
		if strings.Contains(lower, "unauthorized") || strings.Contains(lower, "forbidden") {
			t.Skipf("Skipping E2E pull; unauthorized/forbidden: %s", out)
		}
		t.Fatalf("docker pull failed: %v (%s)", err, out)
	}
}

func dockerRunContainer(t *testing.T, fullRef string) (string, string) {
	t.Helper()
	name := fmt.Sprintf("mcp_e2e_%d", time.Now().UnixNano())
	runArgs := fieldsAllowEmpty(os.Getenv("OCI_E2E_RUN_ARGS"))
	cmdArgs := fieldsAllowEmpty(os.Getenv("OCI_E2E_CMD"))
	args := append([]string{"run", "-d", "--name", name}, runArgs...)
	args = append(args, fullRef)
	args = append(args, cmdArgs...)

	runOut, runErr := runDocker(context.Background(), 1*time.Minute, args...)
	if runErr != nil {
		t.Fatalf("docker run failed: %v (%s)", runErr, runOut)
	}
	startedID := strings.TrimSpace(runOut)
	if startedID == "" {
		startedID = name
	}
	return name, startedID
}

func waitBriefly() { time.Sleep(2 * time.Second) }

func ensureRunningOrSkip(t *testing.T, name, startedID string) {
	t.Helper()
	if out, err := runDocker(context.Background(), 15*time.Second, "inspect", "-f", "{{.State.Running}}", name); err != nil {
		lower := strings.ToLower(out)
		if strings.Contains(lower, "no such object") {
			logs, _ := runDocker(context.Background(), 10*time.Second, "logs", startedID)
			t.Skipf("Container exited immediately and was not found. Provide OCI_E2E_CMD to keep it alive (e.g. 'sleep 5'). Logs: %s", logs)
		}
		t.Fatalf("docker inspect failed: %v (%s)", err, out)
	} else if !strings.Contains(out, "true") {
		logs, _ := runDocker(context.Background(), 10*time.Second, "logs", name)
		t.Skipf("Container not running (probably exited immediately). Set OCI_E2E_CMD to keep it alive. inspect=%q logs=%q", out, logs)
	}
}

func execSystemInfoAndAssertUbuntu(t *testing.T, name string) {
	t.Helper()
	osr, err1 := runDocker(context.Background(), 10*time.Second, "exec", name, "bash", "-lc", "cat /etc/os-release || cat /usr/lib/os-release || true")
	if err1 != nil {
		osr, _ = runDocker(context.Background(), 10*time.Second, "exec", name, "sh", "-lc", "cat /etc/os-release || cat /usr/lib/os-release || true")
	}
	uname, _ := runDocker(context.Background(), 10*time.Second, "exec", name, "sh", "-lc", "uname -a || true")

	// Print exec results for visibility in test output
	t.Logf("/etc/os-release (or fallback):\n%s", strings.TrimSpace(osr))
	t.Logf("uname -a:\n%s", strings.TrimSpace(uname))

	lower := strings.ToLower(osr)
	if !strings.Contains(lower, "id=ubuntu") && !strings.Contains(osr, "Ubuntu") {
		t.Fatalf("Container did not report Ubuntu base. os-release=\n%s\n\n uname=\n%s\n", osr, uname)
	}
}

func stopContainerIgnoreMissing(t *testing.T, name string) {
	t.Helper()
	if out, err := runDocker(context.Background(), 20*time.Second, "stop", name); err != nil {
		if !strings.Contains(strings.ToLower(out), "no such container") {
			t.Fatalf("docker stop failed: %v (%s)", err, out)
		}
	}
}

// runDocker executes `docker` with a timeout and returns combined output and error.
func runDocker(ctx context.Context, timeout time.Duration, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "docker", args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := buf.String()
	// If context deadline exceeded, wrap a more descriptive error
	if cctx.Err() != nil && errors.Is(cctx.Err(), context.DeadlineExceeded) {
		return out, fmt.Errorf("command timed out: docker %s", strings.Join(args, " "))
	}
	return out, err
}

// runDockerWithInput is like runDocker but writes 'input' to stdin (for docker login --password-stdin).
func runDockerWithInput(ctx context.Context, timeout time.Duration, input string, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "docker", args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	cmd.Stdin = strings.NewReader(input)
	err := cmd.Run()
	out := buf.String()
	if cctx.Err() != nil && errors.Is(cctx.Err(), context.DeadlineExceeded) {
		return out, fmt.Errorf("command timed out: docker %s", strings.Join(args, " "))
	}
	return out, err
}

// fieldsAllowEmpty splits on whitespace; returns empty slice if s is empty/whitespace.
func fieldsAllowEmpty(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return []string{}
	}
	return strings.Fields(s)
}
