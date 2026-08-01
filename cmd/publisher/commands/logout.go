package commands

import (
	"fmt"
	"os"
	"path/filepath"
)

func LogoutCommand() error {
	tokenPath, err := tokenFilePath()
	if err != nil {
		return err
	}

	// Check if token file exists at new location or legacy location
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "" // degrade gracefully for legacy cleanup
	}
	legacyTokenPath := ""
	if homeDir != "" {
		legacyTokenPath = filepath.Join(homeDir, LegacyTokenFileName)
	}

	newExists := fileExists(tokenPath)
	legacyExists := legacyTokenPath != "" && fileExists(legacyTokenPath)

	if !newExists && !legacyExists {
		_, _ = fmt.Fprintln(os.Stdout, "Not logged in")
		return nil
	}

	// Remove token file from new location
	os.Remove(tokenPath)

	// Remove from legacy location
	if legacyTokenPath != "" {
		os.Remove(legacyTokenPath)
	}

	// Clean up legacy intermediate token files from $HOME and cwd
	legacyIntermediateFiles := []string{
		".mcpregistry_github_token",
		".mcpregistry_registry_token",
	}

	for _, file := range legacyIntermediateFiles {
		// Clean from $HOME
		if homeDir != "" {
			os.Remove(filepath.Join(homeDir, file))
		}
		// Clean from cwd (the original bug location)
		os.Remove(file)
	}

	_, _ = fmt.Fprintln(os.Stdout, "✓ Successfully logged out")
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
