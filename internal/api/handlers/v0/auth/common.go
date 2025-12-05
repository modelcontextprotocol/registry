package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
)

// CryptoAlgorithm represents the cryptographic algorithm used for a public key
type CryptoAlgorithm string

const (
	AlgorithmEd25519 CryptoAlgorithm = "ed25519"

	// ECDSA with NIST P-384 curve
	// public key is in compressed format
	// signature is in R || S format
	AlgorithmECDSAP384 CryptoAlgorithm = "ecdsap384"
)

// PublicKeyInfo contains a public key along with its algorithm type
type PublicKeyInfo struct {
	Algorithm CryptoAlgorithm
	Key       any
}

// SignatureTokenExchangeInput represents the common input structure for token exchange
type SignatureTokenExchangeInput struct {
	Domain          string `json:"domain" doc:"Domain name" example:"example.com" required:"true"`
	Timestamp       string `json:"timestamp" doc:"RFC3339 timestamp" example:"2023-01-01T00:00:00Z" required:"true"`
	SignedTimestamp string `json:"signed_timestamp" doc:"Hex-encoded signature of timestamp" example:"abcdef1234567890" required:"true"`
}

// KeyFetcher defines a function type for fetching keys from external sources
type KeyFetcher func(ctx context.Context, domain string) ([]string, error)

// CoreAuthHandler represents the common handler structure
type CoreAuthHandler struct {
	config     *config.Config
	jwtManager *auth.JWTManager
}

// NewCoreAuthHandler creates a new core authentication handler
func NewCoreAuthHandler(cfg *config.Config) *CoreAuthHandler {
	return &CoreAuthHandler{
		config:     cfg,
		jwtManager: auth.NewJWTManager(cfg),
	}
}

// ValidateDomainAndTimestamp validates the domain format and timestamp
func ValidateDomainAndTimestamp(domain, timestamp string) (*time.Time, error) {
	if !IsValidDomain(domain) {
		return nil, fmt.Errorf("invalid domain format")
	}

	ts, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return nil, fmt.Errorf("invalid timestamp format: %w", err)
	}

	// Check timestamp is within 15 seconds, to allow for clock skew
	now := time.Now()
	if ts.Before(now.Add(-15*time.Second)) || ts.After(now.Add(15*time.Second)) {
		return nil, fmt.Errorf("timestamp outside valid window (±15 seconds)")
	}

	return &ts, nil
}

func DecodeAndValidateSignature(signedTimestamp string) ([]byte, error) {
	signature, err := hex.DecodeString(signedTimestamp)
	if err != nil {
		return nil, fmt.Errorf("invalid signature format, must be hex: %w", err)
	}

	return signature, nil
}

func VerifySignatureWithKeys(publicKeys []PublicKeyInfo, messageBytes []byte, signature []byte) error {
	for _, publicKeyInfo := range publicKeys {
		err := publicKeyInfo.VerifySignature(messageBytes, signature)
		if err == nil {
			return nil
		}

		if len(publicKeys) == 1 {
			return err
		}
	}

	return fmt.Errorf("signature verification failed")
}

// VerifySignature verifies a signature using the appropriate algorithm
func (pki *PublicKeyInfo) VerifySignature(message, signature []byte) error {
	switch pki.Algorithm {
	case AlgorithmEd25519:
		if ed25519Key, ok := pki.Key.(ed25519.PublicKey); ok {
			if len(signature) != ed25519.SignatureSize {
				return fmt.Errorf("invalid signature size for Ed25519")
			}
			if !ed25519.Verify(ed25519Key, message, signature) {
				return fmt.Errorf("Ed25519 signature verification failed")
			}
			return nil
		}
	case AlgorithmECDSAP384:
		if ecdsaKey, ok := pki.Key.(ecdsa.PublicKey); ok {
			if len(signature) != 96 {
				return fmt.Errorf("invalid signature size for ECDSA P-384")
			}
			r := new(big.Int).SetBytes(signature[:48])
			s := new(big.Int).SetBytes(signature[48:])
			digest := sha512.Sum384(message)
			if !ecdsa.Verify(&ecdsaKey, digest[:], r, s) {
				return fmt.Errorf("ECDSA P-384 signature verification failed")
			}
			return nil
		}
	}
	return fmt.Errorf("unsupported public key algorithm")
}

// BuildPermissions builds permissions for a domain with optional subdomain support
func BuildPermissions(domain string, includeSubdomains bool) []auth.Permission {
	reverseDomain := ReverseString(domain)

	permissions := []auth.Permission{
		// Grant permissions for the exact domain (e.g., com.example/*)
		{
			Action:          auth.PermissionActionPublish,
			ResourcePattern: fmt.Sprintf("%s/*", reverseDomain),
		},
	}

	if includeSubdomains {
		permissions = append(permissions, auth.Permission{
			Action:          auth.PermissionActionPublish,
			ResourcePattern: fmt.Sprintf("%s.*", reverseDomain),
		})
	}

	return permissions
}

// CreateJWTClaimsAndToken creates JWT claims and generates a token response
func (h *CoreAuthHandler) CreateJWTClaimsAndToken(ctx context.Context, authMethod auth.Method, domain string, permissions []auth.Permission) (*auth.TokenResponse, error) {
	// Create JWT claims
	jwtClaims := auth.JWTClaims{
		AuthMethod:        authMethod,
		AuthMethodSubject: domain,
		Permissions:       permissions,
	}

	// Generate Registry JWT token
	tokenResponse, err := h.jwtManager.GenerateTokenResponse(ctx, jwtClaims)
	if err != nil {
		return nil, fmt.Errorf("failed to generate JWT token: %w", err)
	}

	return tokenResponse, nil
}

// ExchangeToken is a shared method for token exchange that takes a key fetcher function,
// subdomain inclusion flag, and auth method
func (h *CoreAuthHandler) ExchangeToken(
	ctx context.Context,
	domain, timestamp, signedTimestamp string,
	keyFetcher KeyFetcher,
	includeSubdomains bool,
	authMethod auth.Method) (*auth.TokenResponse, error) {
	_, err := ValidateDomainAndTimestamp(domain, timestamp)
	if err != nil {
		return nil, err
	}

	signature, err := DecodeAndValidateSignature(signedTimestamp)
	if err != nil {
		return nil, err
	}

	keyStrings, err := keyFetcher(ctx, domain)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch keys: %w", err)
	}

	publicKeysAndErrors := ParseMCPKeysFromStrings(keyStrings)
	if len(publicKeysAndErrors) == 0 {
		switch authMethod {
		case auth.MethodHTTP:
			return nil, fmt.Errorf("no MCP public key found in HTTP response")
		case auth.MethodDNS:
			return nil, fmt.Errorf("no MCP public key found in DNS TXT records")
		case auth.MethodGitHubAT, auth.MethodGitHubOIDC, auth.MethodOIDC, auth.MethodNone:
		default:
			return nil, fmt.Errorf("no MCP public key found using %s authentication", authMethod)
		}
	}

	// provide a specific error message if there's only one key found
	if len(publicKeysAndErrors) == 1 && publicKeysAndErrors[0].error != nil {
		return nil, publicKeysAndErrors[0].error
	}

	var publicKeys []PublicKeyInfo
	for _, pke := range publicKeysAndErrors {
		if pke.error == nil {
			publicKeys = append(publicKeys, *pke.PublicKeyInfo)
		}
	}

	if len(publicKeys) == 0 {
		return nil, fmt.Errorf("no valid MCP public key found")
	}

	messageBytes := []byte(timestamp)
	err = VerifySignatureWithKeys(publicKeys, messageBytes, signature)
	if err != nil {
		return nil, err
	}

	permissions := BuildPermissions(domain, includeSubdomains)

	return h.CreateJWTClaimsAndToken(ctx, authMethod, domain, permissions)
}

func ParseMCPKeysFromStrings(inputs []string) []struct {
	*PublicKeyInfo
	error
} {
	var publicKeys []struct {
		*PublicKeyInfo
		error
	}

	// proof record pattern: v=MCPv1; k=<algo>; p=<base64-public-key>
	cryptoPattern := regexp.MustCompile(`v=MCPv1;\s*k=([^;]+);\s*p=([A-Za-z0-9+/=]+)`)

	for _, record := range inputs {
		if matches := cryptoPattern.FindStringSubmatch(record); len(matches) == 3 {
			publicKey, err := ParsePublicKey(matches[1], matches[2])
			publicKeys = append(publicKeys, struct {
				*PublicKeyInfo
				error
			}{publicKey, err})
		}
	}

	return publicKeys
}

func ParsePublicKey(algorithm, publicKey string) (*PublicKeyInfo, error) {
	publicKeyBytes, err := base64.StdEncoding.DecodeString(publicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decode public key: %w", err)
	}

	// match to a supported crypto algorithm
	switch algorithm {
	case string(AlgorithmEd25519):
		if len(publicKeyBytes) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("invalid Ed25519 public key size")
		}
		return &PublicKeyInfo{
			Algorithm: AlgorithmEd25519,
			Key:       ed25519.PublicKey(publicKeyBytes),
		}, nil
	case string(AlgorithmECDSAP384):
		if len(publicKeyBytes) != 49 {
			return nil, fmt.Errorf("invalid ECDSA P-384 public key size")
		}
		if publicKeyBytes[0] != 0x02 && publicKeyBytes[0] != 0x03 {
			return nil, fmt.Errorf("invalid ECDSA P-384 public key format (must be compressed, with a leading 0x02 or 0x03 byte)")
		}
		curve := elliptic.P384()
		x, y := elliptic.UnmarshalCompressed(curve, publicKeyBytes)
		if x == nil || y == nil {
			return nil, fmt.Errorf("failed to decompress ECDSA P-384 public key")
		}
		return &PublicKeyInfo{
			Algorithm: AlgorithmECDSAP384,
			Key:       ecdsa.PublicKey{Curve: curve, X: x, Y: y},
		}, nil
	}

	return nil, fmt.Errorf("unsupported public key algorithm: %s", algorithm)
}

// ReverseString reverses a domain string (example.com -> com.example)
func ReverseString(domain string) string {
	parts := strings.Split(domain, ".")
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, ".")
}

func IsValidDomain(domain string) bool {
	if len(domain) == 0 || len(domain) > 253 {
		return false
	}

	// Check for valid characters and structure
	domainPattern := regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)*$`)
	return domainPattern.MatchString(domain)
}
