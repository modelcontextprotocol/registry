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

type CryptoAlgorithm auth.CryptoAlgorithm

func (c *CryptoAlgorithm) String() string {
	return string(*c)
}

func (c *CryptoAlgorithm) Set(v string) error {
	switch v {
	case string(auth.AlgorithmEd25519), string(auth.AlgorithmECDSAP384):
		*c = CryptoAlgorithm(v)
		return nil
	}
	return fmt.Errorf("invalid algorithm: %q (allowed: ed25519, ecdsap384)", v)
}

func LoginCommand(args []string) error {
	if len(args) < 1 {
		return errors.New("authentication method required\n\nUsage: mcp-publisher login <method>\n\nMethods:\n  github        Interactive GitHub authentication\n  github-oidc   GitHub Actions OIDC authentication\n  dns           DNS-based authentication (requires --domain and --private-key)\n  http          HTTP-based authentication (requires --domain and --private-key)\n  none          Anonymous authentication (for testing)")
	}

	method := args[0]

	// Parse remaining flags based on method
	loginFlags := flag.NewFlagSet("login", flag.ExitOnError)
	var domain string
	var privateKey string
	var cryptoAlgorithm = CryptoAlgorithm(auth.AlgorithmEd25519)
	var registryURL string

	loginFlags.StringVar(&registryURL, "registry", DefaultRegistryURL, "Registry URL")

	if method == "dns" || method == "http" {
		loginFlags.StringVar(&domain, "domain", "", "Domain name")
		loginFlags.StringVar(&privateKey, "private-key", "", "Private key (hex)")
		loginFlags.Var(&cryptoAlgorithm, "algorithm", "Cryptographic algorithm (ed25519, ecdsap384)")
	}

	if err := loginFlags.Parse(args[1:]); err != nil {
		return err
	}

	// Create auth provider based on method
	var authProvider auth.Provider
	switch method {
	case "github":
		authProvider = auth.NewGitHubATProvider(true, registryURL)
	case "github-oidc":
		authProvider = auth.NewGitHubOIDCProvider(registryURL)
	case "dns":
		if domain == "" || privateKey == "" {
			return errors.New("dns authentication requires --domain and --private-key")
		}
		authProvider = auth.NewDNSProvider(registryURL, domain, privateKey, auth.CryptoAlgorithm(cryptoAlgorithm))
	case "http":
		if domain == "" || privateKey == "" {
			return errors.New("http authentication requires --domain and --private-key")
		}
		authProvider = auth.NewHTTPProvider(registryURL, domain, privateKey, auth.CryptoAlgorithm(cryptoAlgorithm))
	case "none":
		authProvider = auth.NewNoneProvider(registryURL)
	default:
		return fmt.Errorf("unknown authentication method: %s\nFor a list of available methods, run: mcp-publisher login", method)
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
