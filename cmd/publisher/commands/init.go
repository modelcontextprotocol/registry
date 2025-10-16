package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

func InitCommand() error {
	// Check if server.json already exists
	if _, err := os.Stat("server.json"); err == nil {
		return errors.New("server.json already exists")
	}

	// Try to detect values from environment
	name := detectServerName()
	description := detectDescription()
	version := "1.0.0"
	repoURL := detectRepoURL()
	repoSource := "github"
	if repoURL != "" && !strings.Contains(repoURL, "github.com") {
		if strings.Contains(repoURL, "gitlab.com") {
			repoSource = "gitlab"
		} else {
			repoSource = "git"
		}
	}

	packageType := detectPackageType()
	packageIdentifier := detectPackageIdentifier(name, packageType)

	// Create example environment variables
	envVars := []model.KeyValueInput{
		{
			Name: "YOUR_API_KEY",
			InputWithVariables: model.InputWithVariables{
				Input: model.Input{
					Description: "Your API key for the service",
					IsRequired:  true,
					IsSecret:    true,
					Format:      model.FormatString,
				},
			},
		},
	}

	// Create the server structure
	server := createServerJSON(
		name, description, version, repoURL, repoSource,
		packageType, packageIdentifier, version, envVars,
	)

	// Write to file
	jsonData, err := json.MarshalIndent(server, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshaling JSON: %w", err)
	}

	err = os.WriteFile("server.json", jsonData, 0600)
	if err != nil {
		return fmt.Errorf("error writing file: %w", err)
	}

	_, _ = fmt.Fprintln(os.Stdout, "Created server.json")
	_, _ = fmt.Fprintln(os.Stdout, "\nEdit server.json to update:")
	_, _ = fmt.Fprintln(os.Stdout, "  • Server name and description")
	_, _ = fmt.Fprintln(os.Stdout, "  • Package details")
	_, _ = fmt.Fprintln(os.Stdout, "  • Environment variables")
	_, _ = fmt.Fprintln(os.Stdout, "\nThen publish with:")
	_, _ = fmt.Fprintln(os.Stdout, "  mcp-publisher login github  # or your preferred auth method")
	_, _ = fmt.Fprintln(os.Stdout, "  mcp-publisher publish")

	return nil
}

func getNameFromPackageJSON() string {
	data, err := os.ReadFile("package.json")
	if err != nil {
		return ""
	}

	var pkg map[string]any
	if err := json.Unmarshal(data, &pkg); err != nil {
		return ""
	}

	name, ok := pkg["name"].(string)
	if !ok || name == "" {
		return ""
	}

	// Convert npm package name to MCP server name
	// @org/package -> io.npm.org/package
	if strings.HasPrefix(name, "@") {
		parts := strings.Split(name[1:], "/")
		if len(parts) == 2 {
			return fmt.Sprintf("io.github.%s/%s", parts[0], parts[1])
		}
	}
	return fmt.Sprintf("io.github.<your-username>/%s", name)
}

func detectServerName() string {
	// Try to get from git remote
	repoURL := detectRepoURL()
	if repoURL != "" {
		// Extract owner/repo from GitHub URL
		if strings.Contains(repoURL, "github.com") {
			parts := strings.Split(repoURL, "/")
			if len(parts) >= 5 {
				owner := parts[3]
				repo := strings.TrimSuffix(parts[4], ".git")
				return fmt.Sprintf("io.github.%s/%s", owner, repo)
			}
		}
	}

	// Try to get from package.json
	name := getNameFromPackageJSON()
	if name != "" {
		return name
	}

	// Use current directory name as fallback
	if cwd, err := os.Getwd(); err == nil {
		return fmt.Sprintf("com.example/%s", filepath.Base(cwd))
	}

	return "com.example/my-mcp-server"
}

func detectDescription() string {
	// Try to get from package.json
	if data, err := os.ReadFile("package.json"); err == nil {
		var pkg map[string]any
		if json.Unmarshal(data, &pkg) == nil {
			if desc, ok := pkg["description"].(string); ok && desc != "" {
				return desc
			}
		}
	}

	return "An MCP server that provides [describe what your server does]"
}

func detectRepoURL() string {
	// Try git remote
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	if output, err := cmd.Output(); err == nil {
		url := strings.TrimSpace(string(output))
		// Convert SSH URL to HTTPS if needed
		if strings.HasPrefix(url, "git@github.com:") {
			url = strings.Replace(url, "git@github.com:", "https://github.com/", 1)
		}
		url = strings.TrimSuffix(url, ".git")
		return url
	}

	// Try package.json repository field
	if data, err := os.ReadFile("package.json"); err == nil {
		var pkg map[string]any
		if json.Unmarshal(data, &pkg) == nil {
			if repo, ok := pkg["repository"].(map[string]any); ok {
				if url, ok := repo["url"].(string); ok {
					return strings.TrimSuffix(url, ".git")
				}
			}
			if repo, ok := pkg["repository"].(string); ok {
				return strings.TrimSuffix(repo, ".git")
			}
		}
	}

	return "https://github.com/YOUR_USERNAME/YOUR_REPO"
}

func detectPackageType() string {
	// Check for package.json
	if _, err := os.Stat("package.json"); err == nil {
		return model.RegistryTypeNPM
	}

	// Check for pyproject.toml or setup.py
	if _, err := os.Stat("pyproject.toml"); err == nil {
		return model.RegistryTypePyPI
	}
	if _, err := os.Stat("setup.py"); err == nil {
		return model.RegistryTypePyPI
	}

	// Check for Dockerfile
	if _, err := os.Stat("Dockerfile"); err == nil {
		return model.RegistryTypeOCI
	}

	// Default to npm as most common
	return model.RegistryTypeNPM
}

func detectPackageIdentifier(serverName string, packageType string) string {
	switch packageType {
	case model.RegistryTypeNPM:
		// Try to get from package.json
		if data, err := os.ReadFile("package.json"); err == nil {
			var pkg map[string]any
			if json.Unmarshal(data, &pkg) == nil {
				if name, ok := pkg["name"].(string); ok && name != "" {
					return name
				}
			}
		}
		// Convert server name to npm package name
		if strings.HasPrefix(serverName, "io.github.") {
			parts := strings.Split(serverName, "/")
			if len(parts) == 2 {
				owner := strings.TrimPrefix(parts[0], "io.github.")
				return fmt.Sprintf("@%s/%s", owner, parts[1])
			}
		}
		return "@your-org/your-package"

	case model.RegistryTypePyPI:
		// Try to get from pyproject.toml or setup.py
		if data, err := os.ReadFile("pyproject.toml"); err == nil {
			// Simple extraction - could be improved with proper TOML parser
			lines := strings.Split(string(data), "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "name") && strings.Contains(line, "=") {
					parts := strings.Split(line, "=")
					if len(parts) >= 2 {
						name := strings.Trim(parts[1], " \"'")
						if name != "" {
							return name
						}
					}
				}
			}
		}
		return "your-package"

	case model.RegistryTypeOCI:
		// Use a sensible default
		if strings.Contains(serverName, "/") {
			parts := strings.Split(serverName, "/")
			return parts[len(parts)-1]
		}
		return "your-image"

	default:
		return "your-package"
	}
}

func createServerJSON(
	name, description, version, repoURL, repoSource,
	packageType, packageIdentifier, packageVersion string,
	envVars []model.KeyValueInput,
) apiv0.ServerJSON {
	// Create package based on type
	var pkg model.Package

	switch packageType {
	case model.RegistryTypeNPM:
		pkg = model.Package{
			RegistryType:         model.RegistryTypeNPM,
			Identifier:           packageIdentifier,
			Version:              packageVersion,
			EnvironmentVariables: envVars,
			Transport: model.Transport{
				Type: model.TransportTypeStdio,
			},
		}
	case model.RegistryTypePyPI:
		pkg = model.Package{
			RegistryType:         model.RegistryTypePyPI,
			Identifier:           packageIdentifier,
			Version:              packageVersion,
			EnvironmentVariables: envVars,
			Transport: model.Transport{
				Type: model.TransportTypeStdio,
			},
		}
	case model.RegistryTypeOCI:
		// OCI packages use canonical references: registry/namespace/image:tag
		// Format: docker.io/username/image:version
		canonicalRef := fmt.Sprintf("docker.io/%s:%s", packageIdentifier, packageVersion)
		pkg = model.Package{
			RegistryType: model.RegistryTypeOCI,
			Identifier:   canonicalRef,
			// No Version field for OCI - it's embedded in the canonical reference
			EnvironmentVariables: envVars,
			Transport: model.Transport{
				Type: model.TransportTypeStdio,
			},
		}
	case "url":
		pkg = model.Package{
			RegistryType:         "url",
			Identifier:           packageIdentifier,
			Version:              packageVersion,
			EnvironmentVariables: envVars,
			Transport: model.Transport{
				Type: model.TransportTypeStdio,
			},
		}
	default:
		pkg = model.Package{
			RegistryType:         packageType,
			Identifier:           packageIdentifier,
			Version:              packageVersion,
			EnvironmentVariables: envVars,
			Transport: model.Transport{
				Type: model.TransportTypeStdio,
			},
		}
	}

	// Create server structure
	return apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        name,
		Description: description,
		Repository: model.Repository{
			URL:    repoURL,
			Source: repoSource,
		},
		Version:  version,
		Packages: []model.Package{pkg},
	}
}
