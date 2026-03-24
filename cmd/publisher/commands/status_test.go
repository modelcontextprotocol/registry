package commands_test

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/registry/cmd/publisher/commands"
)

func TestStatusCommand_Validation(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectError bool
		errorSubstr string
	}{
		{
			name:        "missing --status flag",
			args:        []string{"io.github.user/my-server", "1.0.0"},
			expectError: true,
			errorSubstr: "--status flag is required",
		},
		{
			name:        "invalid status value",
			args:        []string{"--status", "invalid", "io.github.user/my-server", "1.0.0"},
			expectError: true,
			errorSubstr: "invalid status 'invalid'",
		},
		{
			name:        "missing server name",
			args:        []string{"--status", "deprecated"},
			expectError: true,
			errorSubstr: "server name is required",
		},
		{
			name:        "missing version without --all-versions",
			args:        []string{"--status", "deprecated", "io.github.user/my-server"},
			expectError: true,
			errorSubstr: "version is required unless --all-versions",
		},
		{
			name:        "valid args passes validation",
			args:        []string{"--status", "deprecated", "io.github.user/my-server", "1.0.0"},
			expectError: false,
		},
		{
			name:        "valid args with --all-versions passes validation",
			args:        []string{"--status", "deprecated", "--all-versions", "io.github.user/my-server"},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := commands.StatusCommand(tt.args)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
					return
				}
				if !strings.Contains(err.Error(), tt.errorSubstr) {
					t.Errorf("Expected error containing '%s', got: %v", tt.errorSubstr, err)
				}
			} else if err != nil {
				// For valid args, we expect it to pass validation
				// It may fail later at auth or API level, which is acceptable
				// Just check it's not a validation error
				if strings.Contains(err.Error(), "invalid status") ||
					strings.Contains(err.Error(), "server name is required") ||
					strings.Contains(err.Error(), "version is required unless") ||
					strings.Contains(err.Error(), "--status flag is required") {
					t.Errorf("Validation failed unexpectedly: %v", err)
				}
			}
		})
	}
}

func TestStatusCommand_ServerNameValidation(t *testing.T) {
	tests := []struct {
		name       string
		serverName string
	}{
		{
			name:       "valid github server name",
			serverName: "io.github.user/my-server",
		},
		{
			name:       "valid domain server name",
			serverName: "com.example/my-server",
		},
		{
			name:       "server name with dashes",
			serverName: "io.github.user/my-cool-server",
		},
		{
			name:       "server name with underscores",
			serverName: "io.github.user/my_server",
		},
		{
			name:       "server name with dots",
			serverName: "io.github.user/my.server",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := []string{"--status", "deprecated", tt.serverName, "1.0.0"}
			err := commands.StatusCommand(args)

			// Should pass validation (server name format is not validated by CLI)
			if err != nil && strings.Contains(err.Error(), "server name is required") {
				t.Errorf("Server name '%s' was rejected", tt.serverName)
			}
		})
	}
}

func TestStatusCommand_VersionValidation(t *testing.T) {
	tests := []struct {
		name    string
		version string
	}{
		{
			name:    "semver version",
			version: "1.0.0",
		},
		{
			name:    "semver with patch",
			version: "1.2.3",
		},
		{
			name:    "semver with prerelease",
			version: "1.0.0-alpha",
		},
		{
			name:    "semver with build metadata",
			version: "1.0.0+20130313144700",
		},
		{
			name:    "semver with prerelease and build",
			version: "1.0.0-beta.1+exp.sha.5114f85",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := []string{"--status", "deprecated", "io.github.user/my-server", tt.version}
			err := commands.StatusCommand(args)

			// Should pass validation (version format is not validated by CLI)
			if err != nil && strings.Contains(err.Error(), "version is required") {
				t.Errorf("Version '%s' was rejected", tt.version)
			}
		})
	}
}

func TestStatusCommand_AllVersionsFlag(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectError bool
		errorSubstr string
	}{
		{
			name:        "all-versions without version arg passes validation",
			args:        []string{"--status", "deprecated", "--all-versions", "io.github.user/my-server"},
			expectError: false,
		},
		{
			name:        "all-versions with extra version arg still works",
			args:        []string{"--status", "deprecated", "--all-versions", "io.github.user/my-server", "1.0.0"},
			expectError: false,
		},
		{
			name:        "missing server name with all-versions",
			args:        []string{"--status", "deprecated", "--all-versions"},
			expectError: true,
			errorSubstr: "server name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := commands.StatusCommand(tt.args)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
					return
				}
				if !strings.Contains(err.Error(), tt.errorSubstr) {
					t.Errorf("Expected error containing '%s', got: %v", tt.errorSubstr, err)
				}
			} else if err != nil {
				// Should pass validation
				// Just check it's not a validation error
				if strings.Contains(err.Error(), "invalid status") ||
					strings.Contains(err.Error(), "server name is required") ||
					strings.Contains(err.Error(), "version is required unless") ||
					strings.Contains(err.Error(), "--status flag is required") {
					t.Errorf("Validation failed unexpectedly: %v", err)
				}
			}
		})
	}
}

func TestStatusCommand_FlagCombinations(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "status with message",
			args: []string{"--status", "deprecated", "--message", "Please upgrade to v2", "io.github.user/my-server", "1.0.0"},
		},
		{
			name: "all-versions with message",
			args: []string{"--status", "deprecated", "--all-versions", "--message", "All versions deprecated", "io.github.user/my-server"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := commands.StatusCommand(tt.args)
			// All these should pass CLI validation
			// They may fail at auth or API level which is acceptable
			if err != nil {
				// Just check it's not a validation error we can detect
				if strings.Contains(err.Error(), "invalid status") ||
					strings.Contains(err.Error(), "server name is required") ||
					strings.Contains(err.Error(), "version is required unless") ||
					strings.Contains(err.Error(), "--status flag is required") {
					t.Errorf("Validation failed unexpectedly: %v", err)
				}
			}
		})
	}
}

func TestStatusCommand_MissingStatus(t *testing.T) {
	// Test various ways status flag can be missing
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "no status flag at all",
			args: []string{"io.github.user/my-server", "1.0.0"},
		},
		{
			name: "empty status value",
			args: []string{"--status", "", "io.github.user/my-server", "1.0.0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := commands.StatusCommand(tt.args)

			if err == nil {
				t.Errorf("Expected error for missing status but got none")
				return
			}
			if !strings.Contains(err.Error(), "--status flag is required") {
				t.Errorf("Expected '--status flag is required' error, got: %v", err)
			}
		})
	}
}

func TestStatusCommand_YesFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "all-versions with --yes flag",
			args: []string{"--status", "deprecated", "--all-versions", "--yes", "io.github.user/my-server"},
		},
		{
			name: "all-versions with -y shorthand",
			args: []string{"--status", "deprecated", "--all-versions", "-y", "io.github.user/my-server"},
		},
		{
			name: "yes flag with single version (flag accepted but not used)",
			args: []string{"--status", "deprecated", "--yes", "io.github.user/my-server", "1.0.0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := commands.StatusCommand(tt.args)
			// All these should pass CLI validation
			// They may fail at auth or API level which is acceptable
			if err != nil {
				// Just check it's not a validation error
				if strings.Contains(err.Error(), "invalid status") ||
					strings.Contains(err.Error(), "server name is required") ||
					strings.Contains(err.Error(), "version is required unless") ||
					strings.Contains(err.Error(), "--status flag is required") ||
					strings.Contains(err.Error(), "flag provided but not defined") {
					t.Errorf("Validation failed unexpectedly: %v", err)
				}
			}
		})
	}
}
