package commands

import (
	"fmt"
	"os"
	"path/filepath"
)

func LogoutCommand() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	tokenPath := filepath.Join(homeDir, TokenFileName)

	// Check if token file exists
	if _, err := os.Stat(tokenPath); os.IsNotExist(err) {
		_, _ = fmt.Fprintln(os.Stdout, "Not logged in")
		return nil
	}

	// Remove token file
	if err := os.Remove(tokenPath); err != nil {
		return fmt.Errorf("failed to remove token: %w", err)
	}

	// Clean up .mcpregistry directory (new location) and legacy flat files
	_ = os.RemoveAll(".mcpregistry")
	for _, file := range []string{".mcpregistry_github_token", ".mcpregistry_registry_token"} {
		_ = os.Remove(file)
	}

	_, _ = fmt.Fprintln(os.Stdout, "✓ Successfully logged out")
	return nil
}
