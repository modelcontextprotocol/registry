package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
)

// readErrorBody reads an upstream error response body for inclusion in an
// error message. The cap protects against a misbehaving upstream returning a
// huge body — error diagnostics never need more than a few KB.
func readErrorBody(r io.Reader) string {
	const maxErrorBodySize = 8 * 1024
	b, _ := io.ReadAll(io.LimitReader(r, maxErrorBodySize))
	return string(b)
}

// ErrSignatureMismatch is returned by VerifySignature when the signature is structurally
// valid but does not verify against the public key. Distinguishing this from structural
// failures (wrong size, bad key format) lets the caller add fingerprint hints only when
// the failure is actually a "wrong key" situation.
var ErrSignatureMismatch = errors.New("signature does not match public key")

// MCPProofRecordPattern matches a well-formed MCPv1 DNS/HTTP proof record:
// "v=MCPv1; k=<algo>; p=<base64-public-key>". Shared so callers checking for the
// presence of a valid record see exactly what the parser will accept.
var MCPProofRecordPattern = regexp.MustCompile(`v=MCPv1;\s*k=([^;]+);\s*p=([A-Za-z0-9+/=]+)`)

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
	var lastErr error
	allMismatch := true
	for _, publicKeyInfo := range publicKeys {
		err := publicKeyInfo.VerifySignature(messageBytes, signature)
		if err == nil {
			return nil
		}
		lastErr = err
		if !errors.Is(err, ErrSignatureMismatch) {
			allMismatch = false
		}
	}

	// If at least one key failed for a structural reason (wrong size, unsupported algorithm),
	// surface that error directly — it's more actionable than a generic "didn't match" message.
	if !allMismatch {
		return lastErr
	}

	// Every key was tried and produced a clean cryptographic mismatch. Include short
	// fingerprints of every key that was tried so users can tell which published keys the
	// registry actually saw — the most common cause of this error is a stale record left
	// behind after a key rotation, which is otherwise indistinguishable from a generic
	// crypto failure.
	fingerprints := make([]string, 0, len(publicKeys))
	for _, publicKeyInfo := range publicKeys {
		fingerprints = append(fingerprints, publicKeyInfo.Fingerprint())
	}
	if len(publicKeys) == 1 {
		return fmt.Errorf(
			"signature verification failed (tried published key %s); "+
				"if this is not the key you are signing with, the published record may be stale",
			fingerprints[0],
		)
	}
	return fmt.Errorf(
		"signature verification failed against all %d published keys (tried: %s); "+
			"if you recently rotated keys, remove any stale records from the apex domain",
		len(publicKeys), strings.Join(fingerprints, ", "),
	)
}

// Fingerprint returns a short, human-readable identifier for the public key.
// Format: "<algorithm>:<first 8 base64 chars of the raw key>". Public keys are not secret,
// but truncating keeps error messages readable.
func (pki *PublicKeyInfo) Fingerprint() string {
	const prefixLen = 8
	var raw []byte
	switch pki.Algorithm {
	case AlgorithmEd25519:
		if k, ok := pki.Key.(ed25519.PublicKey); ok {
			raw = k
		}
	case AlgorithmECDSAP384:
		if k, ok := pki.Key.(ecdsa.PublicKey); ok {
			raw = elliptic.MarshalCompressed(k.Curve, k.X, k.Y) //nolint:staticcheck // SA1019: matches the encoding used in DNS records
		}
	}
	if len(raw) == 0 {
		return fmt.Sprintf("%s:unknown", pki.Algorithm)
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	if len(encoded) > prefixLen {
		encoded = encoded[:prefixLen]
	}
	return fmt.Sprintf("%s:%s", pki.Algorithm, encoded)
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
				return fmt.Errorf("Ed25519: %w", ErrSignatureMismatch)
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
				return fmt.Errorf("ECDSA P-384: %w", ErrSignatureMismatch)
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

	for _, record := range inputs {
		if matches := MCPProofRecordPattern.FindStringSubmatch(record); len(matches) == 3 {
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

	// Reject IP literals — this auth method proves domain ownership, not IP
	// ownership, and IP literals are an SSRF vector into internal networks.
	if net.ParseIP(domain) != nil {
		return false
	}

	// Require at least one dot — rejects single-label names like "localhost"
	// or "kubernetes" that resolve only inside private networks.
	if !strings.Contains(domain, ".") {
		return false
	}

	// Check for valid characters and structure
	domainPattern := regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)*$`)
	return domainPattern.MatchString(domain)
}
