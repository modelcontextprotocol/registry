package commands

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/registry/cmd/publisher/auth"
	"github.com/modelcontextprotocol/registry/cmd/publisher/auth/azurekeyvault"
	"github.com/modelcontextprotocol/registry/cmd/publisher/auth/googlekms"
)

const (
	DefaultRegistryURL = "https://registry.modelcontextprotocol.io"
	TokenFileName      = ".mcp_publisher_token" //nolint:gosec // Not a credential, just a filename
	MethodGitHub       = "github"
	MethodGitHubOIDC   = "github-oidc"
	MethodDNS          = "dns"
	MethodHTTP         = "http"
	MethodNone         = "none"
)

type CryptoAlgorithm auth.CryptoAlgorithm

type SignerType string

type LoginFlags struct {
	Domain          string
	PrivateKey      string
	RegistryURL     string
	KvVault         string
	KvKeyName       string
	KmsResource     string
	Token           Token
	CryptoAlgorithm CryptoAlgorithm
	SignerType      SignerType
	ArgOffset       int
}

const (
	InProcessSignerType     SignerType = "in-process"
	AzureKeyVaultSignerType SignerType = "azure-key-vault"
	GoogleKMSSignerType     SignerType = "google-kms"
	NoSignerType            SignerType = "none"
)

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

type Token string

func parseLoginFlags(method string, args []string) (LoginFlags, error) {
	var flags LoginFlags
	loginFlags := flag.NewFlagSet("login", flag.ExitOnError)
	flags.CryptoAlgorithm = CryptoAlgorithm(auth.AlgorithmEd25519)
	flags.SignerType = NoSignerType
	flags.ArgOffset = 1
	loginFlags.StringVar(&flags.RegistryURL, "registry", DefaultRegistryURL, "Registry URL")

	// Add --token flag for GitHub authentication
	var token string
	if method == MethodGitHub {
		loginFlags.StringVar(&token, "token", "", "GitHub Personal Access Token")
	}

	if method == "dns" || method == "http" {
		loginFlags.StringVar(&flags.Domain, "domain", "", "Domain name")
		if len(args) > 1 {
			switch args[1] {
			case string(AzureKeyVaultSignerType):
				flags.SignerType = AzureKeyVaultSignerType
				loginFlags.StringVar(&flags.KvVault, "vault", "", "The name of the Azure Key Vault resource")
				loginFlags.StringVar(&flags.KvKeyName, "key", "", "Name of the signing key in the Azure Key Vault")
				flags.ArgOffset = 2
			case string(GoogleKMSSignerType):
				flags.SignerType = GoogleKMSSignerType
				loginFlags.StringVar(&flags.KmsResource, "resource", "", "Google Cloud KMS resource name (e.g. projects/lotr/locations/global/keyRings/fellowship/cryptoKeys/frodo/cryptoKeyVersions/1)")
				flags.ArgOffset = 2
			}
		}
		if flags.SignerType == NoSignerType {
			flags.SignerType = InProcessSignerType
			loginFlags.StringVar(&flags.PrivateKey, "private-key", "", "Private key (hex)")
			loginFlags.Var(&flags.CryptoAlgorithm, "algorithm", "Cryptographic algorithm (ed25519, ecdsap384)")
		}
	}
	err := loginFlags.Parse(args[flags.ArgOffset:])
	if err == nil {
		flags.RegistryURL = strings.TrimRight(flags.RegistryURL, "/")
	}

	// Store the token in flags if it was provided
	if method == MethodGitHub {
		flags.Token = Token(token)
	}

	return flags, err
}

func createSigner(flags LoginFlags) (auth.Signer, error) {
	switch flags.SignerType {
	case AzureKeyVaultSignerType:
		return azurekeyvault.GetSignatureProvider(flags.KvVault, flags.KvKeyName)
	case GoogleKMSSignerType:
		return googlekms.GetSignatureProvider(flags.KmsResource)
	case InProcessSignerType:
		return auth.NewInProcessSigner(flags.PrivateKey, auth.CryptoAlgorithm(flags.CryptoAlgorithm))
	case NoSignerType:
		return nil, errors.New("no signing provider specified")
	default:
		return nil, errors.New("unknown signing provider specified")
	}
}

func createAuthProvider(method, registryURL, domain string, token Token, signer auth.Signer) (auth.Provider, error) {
	switch method {
	case MethodGitHub:
		return auth.NewGitHubATProvider(true, registryURL, string(token)), nil
	case MethodGitHubOIDC:
		return auth.NewGitHubOIDCProvider(registryURL), nil
	case MethodDNS:
		if domain == "" {
			return nil, errors.New("dns authentication requires --domain")
		}
		return auth.NewDNSProvider(registryURL, domain, &signer), nil
	case MethodHTTP:
		if domain == "" {
			return nil, errors.New("http authentication requires --domain")
		}
		return auth.NewHTTPProvider(registryURL, domain, &signer), nil
	case MethodNone:
		return auth.NewNoneProvider(registryURL), nil
	default:
		return nil, fmt.Errorf("unknown authentication method: %s\nFor a list of available methods, run: mcp-publisher login", method)
	}
}

func LoginCommand(args []string) error {
	if len(args) < 1 {
		return errors.New(`authentication method required

Usage: mcp-publisher login <method> [<signing provider>]

Methods:
  github            Interactive GitHub authentication
  github-oidc       GitHub Actions OIDC authentication
  dns               DNS-based authentication (requires --domain)
  http              HTTP-based authentication (requires --domain)
  none              Anonymous authentication (for testing)

Signing providers:
  azure-key-vault   Sign using Azure Key Vault
  google-kms        Sign using Google Cloud KMS

The dns and http methods require a --private-key for in-process signing. For
out-of-process signing, use one of the supported signing providers. Signing is
needed for an authentication challenge with the registry.

The github and github-oidc methods do not support signing providers and
authenticate using the GitHub as an identity provider.

Examples:

  # Interactive GitHub login, using device code flow
  mcp-publisher login github
  
  # Sign in using a specific Ed25519 private key for DNS authentication
  mcp-publisher login dns -algorithm ed25519 -domain example.com -private-key <64 hex chars>

  # Sign in using a specific ECDSA P-384 private key for DNS authentication
  mcp-publisher login dns -algorithm ecdsap384 -domain example.com -private-key <96 hex chars>
  
  # Sign in with gcloud CLI, use Google Cloud KMS for signing in DNS authentication
  gcloud auth application-default login
  mcp-publisher login dns google-kms -domain example.com -resource projects/lotr/locations/global/keyRings/fellowship/cryptoKeys/frodo/cryptoKeyVersions/1

  # Sign in with az CLI, use Azure Key Vault for signing in HTTP authentication
  az login
  mcp-publisher login http azure-key-vault -domain example.com -vault myvault -key mysigningkey

  `)
	}

	method := args[0]
	flags, err := parseLoginFlags(method, args)
	if err != nil {
		return err
	}

	var signer auth.Signer
	if flags.SignerType != NoSignerType {
		signer, err = createSigner(flags)
		if err != nil {
			return err
		}
	}

	authProvider, err := createAuthProvider(method, flags.RegistryURL, flags.Domain, flags.Token, signer)
	if err != nil {
		return err
	}
	ctx := context.Background()
	_, _ = fmt.Fprintf(os.Stdout, "Logging in with %s...\n", method)

	if err := authProvider.Login(ctx); err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	token, err := authProvider.GetToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get token: %w", err)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	tokenPath := filepath.Join(homeDir, TokenFileName)
	tokenData := map[string]string{
		"token":    token,
		"method":   method,
		"registry": flags.RegistryURL,
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
