package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/registry/cmd/publisher/auth"
	"github.com/modelcontextprotocol/registry/cmd/publisher/auth/azurekeyvault"
	"github.com/modelcontextprotocol/registry/cmd/publisher/auth/googlekms"
	"github.com/spf13/cobra"
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

type Token string

type SignerType string

type LoginFlags struct {
	Domain          string
	PrivateKey      string
	RegistryURL     string
	KvVault         string
	KvKeyName       string
	KmsResource     string
	Token           string
	CryptoAlgorithm CryptoAlgorithm
	SignerType      SignerType
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

func (c *CryptoAlgorithm) Type() string {
	return "cryptoAlgorithm"
}

var flags LoginFlags

func init() {
	mcpPublisherCmd.AddCommand(loginCmd)
	loginCmd.Flags().StringVar(&flags.RegistryURL, "registry", DefaultRegistryURL, "Registry URL")
	loginCmd.Flags().StringVarP(&flags.Token, "token", "t", "", "GitHub Personal Access Token")
	loginCmd.Flags().StringVarP(&flags.Domain, "domain", "d", "", "Domain name")
	loginCmd.Flags().StringVarP(&flags.KvVault, "vault", "v", "", "The name of the Azure Key Vault resource")
	loginCmd.Flags().StringVarP(&flags.KvKeyName, "key", "k", "", "Name of the signing key in the Azure Key Vault")
	loginCmd.Flags().StringVarP(&flags.KmsResource, "resource", "r", "", "Google Cloud KMS resource name (e.g. projects/lotr/locations/global/keyRings/fellowship/cryptoKeys/frodo/cryptoKeyVersions/1)")
	loginCmd.Flags().StringVarP(&flags.PrivateKey, "private-key", "p", "", "Private key (hex)")
	loginCmd.Flags().VarP(&flags.CryptoAlgorithm, "algorithm", "a", "Cryptographic algorithm (ed25519, ecdsap384)")
}

var loginCmd = &cobra.Command{
	Use:   "login <method> [options]",
	Short: "Authenticate with the registry",
	Long: `Methods:
  github        Interactive GitHub authentication
  github-oidc   GitHub Actions OIDC authentication
  dns           DNS-based authentication (requires --domain)
  http          HTTP-based authentication (requires --domain)
  none          Anonymous authentication (for testing)`,
	Args: func(_ *cobra.Command, args []string) error {
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
	mcp-publisher login http azure-key-vault -domain example.com -vault myvault -key mysigningkey`)
		}
		return nil
	},
	Example: `
  mcp-publisher login github
  mcp-publisher login dns --domain example.com --private-key <key>`,
	RunE: runLoginCommand,
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

func createAuthProvider(method, registryURL, domain string, token string, signer auth.Signer) (auth.Provider, error) {
	switch method {
	case MethodGitHub:
		return auth.NewGitHubATProvider(true, registryURL, token), nil
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

var runLoginCommand = func(_ *cobra.Command, args []string) error {
	var (
		signer auth.Signer
		err    error
	)
	method := args[0]
	flags.SignerType = NoSignerType
	if method == "http" || method == "dns" {
		if len(args) > 1 {
			switch args[1] {
			case string(AzureKeyVaultSignerType):
				flags.SignerType = AzureKeyVaultSignerType
			case string(GoogleKMSSignerType):
				flags.SignerType = GoogleKMSSignerType
			}
		} else {
			flags.SignerType = InProcessSignerType
		}
	}

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
