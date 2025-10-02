package commands

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/registry/cmd/publisher/auth"
)

const (
	DefaultRegistryURL = "https://registry.modelcontextprotocol.io"
	TokenFileName      = ".mcp_publisher_token" //nolint:gosec // Not a credential, just a filename
)

func LoginCommand(args []string) error {
	if len(args) < 1 {
		return errors.New("authentication method required\n\nUsage: mcp-publisher login <method>\n\nMethods:\n  github        Interactive GitHub authentication\n  github-oidc   GitHub Actions OIDC authentication\n  oidc          Generic OIDC authentication using device flow\n  dns           DNS-based authentication (requires --domain and --private-key)\n  http          HTTP-based authentication (requires --domain and --private-key)\n  none          Anonymous authentication (for testing)")
	}

	method := args[0]

	// Parse remaining flags based on method
	loginFlags := flag.NewFlagSet("login", flag.ExitOnError)
	var domain string
	var privateKey string
	var registryURL string

	loginFlags.StringVar(&registryURL, "registry", DefaultRegistryURL, "Registry URL")

	if method == "dns" || method == "http" {
		loginFlags.StringVar(&domain, "domain", "", "Domain name")
		loginFlags.StringVar(&privateKey, "private-key", "", "Private key (64-char hex)")
	}

	if err := loginFlags.Parse(args[1:]); err != nil {
		return err
	}

	// Create auth provider based on method
	authProvider, err := createAuthProvider(method, registryURL, domain, privateKey)
	if err != nil {
		return err
	}

	// Perform login
	ctx := context.Background()
	_, _ = fmt.Fprintf(os.Stdout, "Logging in with %s...\n", method)

	if err := authProvider.Login(ctx); err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	// Get and save token
	token, err := authProvider.GetToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get token: %w", err)
	}

	// Save token to file
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	tokenPath := filepath.Join(homeDir, TokenFileName)
	tokenData := map[string]string{
		"token":    token,
		"method":   method,
		"registry": registryURL,
	}

	jsonData, err := json.Marshal(tokenData)
	if err != nil {
		return fmt.Errorf("failed to marshal token data: %w", err)
	}

	if err := os.WriteFile(tokenPath, jsonData, 0600); err != nil {
		return fmt.Errorf("failed to save token: %w", err)
	}

	_, _ = fmt.Fprintln(os.Stdout, "✓ Successfully logged in")
	return nil
}

// createAuthProvider creates an authentication provider based on the method
func createAuthProvider(method, registryURL, domain, privateKey string) (auth.Provider, error) {
	switch method {
	case "github":
		return auth.NewGitHubATProvider(true, registryURL), nil
	case "github-oidc":
		return auth.NewGitHubOIDCProvider(registryURL), nil
	case "oidc":
		return auth.NewOIDCProvider(registryURL), nil
	case "dns":
		if domain == "" || privateKey == "" {
			return nil, errors.New("dns authentication requires --domain and --private-key")
		}
		return auth.NewDNSProvider(registryURL, domain, privateKey), nil
	case "http":
		if domain == "" || privateKey == "" {
			return nil, errors.New("http authentication requires --domain and --private-key")
		}
		return auth.NewHTTPProvider(registryURL, domain, privateKey), nil
	case "none":
		return auth.NewNoneProvider(registryURL), nil
	default:
		return nil, fmt.Errorf("unknown authentication method: %s", method)
	}
}
