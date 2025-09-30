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

	// Also clean up legacy token files if they exist
	legacyFiles := []string{
		".mcpregistry_github_token",
		".mcpregistry_registry_token",
	}

	for _, file := range legacyFiles {
		path := filepath.Join(homeDir, file)
		if _, err := os.Stat(path); err == nil {
			os.Remove(path) // Ignore errors for legacy files
		}
	}

	_, _ = fmt.Fprintln(os.Stdout, "✓ Successfully logged out")
	return nil
}
