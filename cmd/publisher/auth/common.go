package auth

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"time"
)

type CryptoAlgorithm string

const (
	AlgorithmEd25519 CryptoAlgorithm = "ed25519"

	// ECDSA with NIST P-384 curve
	// public key is in compressed format
	// signature is in R || S format
	AlgorithmECDSAP384 CryptoAlgorithm = "ecdsap384"
)

// CryptoProvider provides common functionality for DNS and HTTP authentication
type CryptoProvider struct {
	registryURL string
	domain      string
	signer      Signer
	authMethod  string
}

type Signer interface {
	GetSignedTimestamp(ctx context.Context) (*string, []byte, error)
}

func GetTimestamp() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func NewInProcessSigner(privateKey string, algorithm CryptoAlgorithm) (Signer, error) {
	if privateKey == "" {
		return nil, fmt.Errorf("%s private key (hex) is required", algorithm)
	}

	// Decode private key from hex
	privateKeyBytes, err := hex.DecodeString(privateKey)
	if err != nil {
		return nil, fmt.Errorf("invalid hex private key format: %w", err)
	}

	return &InProcessSigner{
		privateKey:      privateKeyBytes,
		cryptoAlgorithm: algorithm,
	}, nil
}

// GetToken retrieves the registry JWT token using cryptographic authentication
func (c *CryptoProvider) GetToken(ctx context.Context) (string, error) {
	if c.domain == "" {
		return "", fmt.Errorf("%s domain is required", c.authMethod)
	}

	// Generate current timestamp
	timestamp, signedTimestamp, err := c.signer.GetSignedTimestamp(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to sign timestamp: %w", err)
	}
	signedTimestampHex := hex.EncodeToString(signedTimestamp)

	// Exchange signature for registry token
	registryToken, err := c.exchangeTokenForRegistry(ctx, c.domain, *timestamp, signedTimestampHex)
	if err != nil {
		return "", fmt.Errorf("failed to exchange %s signature: %w", c.authMethod, err)
	}

	return registryToken, nil
}

type InProcessSigner struct {
	privateKey      []byte
	cryptoAlgorithm CryptoAlgorithm
}

func (c *InProcessSigner) GetSignedTimestamp(_ context.Context) (*string, []byte, error) {
	fmt.Fprintf(os.Stdout, "Signing in process using key algorithm %s\n", c.cryptoAlgorithm)

	timestamp := GetTimestamp()

	switch c.cryptoAlgorithm {
	case AlgorithmEd25519:
		if len(c.privateKey) != ed25519.SeedSize {
			return nil, nil, fmt.Errorf("invalid seed length: expected %d bytes, got %d", ed25519.SeedSize, len(c.privateKey))
		}

		privateKey := ed25519.NewKeyFromSeed(c.privateKey)

		PrintEd25519KeyInfo(privateKey.Public().(ed25519.PublicKey))

		signature := ed25519.Sign(privateKey, []byte(timestamp))
		return &timestamp, signature, nil
	case AlgorithmECDSAP384:
		if len(c.privateKey) != 48 {
			return nil, nil, fmt.Errorf("invalid seed length for ECDSA P-384: expected 48 bytes, got %d", len(c.privateKey))
		}

		digest := sha512.Sum384([]byte(timestamp))
		curve := elliptic.P384()

		// Parse the raw private key (compatible with Go 1.24)
		privateKey, err := parseRawPrivateKey(curve, c.privateKey)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse ECDSA private key: %w", err)
		}

		PrintEcdsaP384KeyInfo(privateKey.PublicKey)

		r, s, err := ecdsa.Sign(rand.Reader, privateKey, digest[:])
		if err != nil {
			return nil, nil, fmt.Errorf("failed to sign message: %w", err)
		}
		signature := append(r.Bytes(), s.Bytes()...)
		return &timestamp, signature, nil
	default:
		return nil, nil, fmt.Errorf("unsupported crypto algorithm: %s", c.cryptoAlgorithm)
	}
}

// parseRawPrivateKey parses a raw ECDSA private key from bytes.
// This mimics crypto/ecdsa.ParseRawPrivateKey from Go 1.25+ for compatibility with Go 1.24.
func parseRawPrivateKey(curve elliptic.Curve, privateKeyBytes []byte) (*ecdsa.PrivateKey, error) {
	if curve == nil {
		return nil, fmt.Errorf("nil curve")
	}

	expectedBytes := (curve.Params().N.BitLen() + 7) / 8
	if len(privateKeyBytes) != expectedBytes {
		return nil, fmt.Errorf("invalid private key length: expected %d bytes, got %d", expectedBytes, len(privateKeyBytes))
	}

	// Only standard NIST curves supported
	switch curve {
	case elliptic.P224(), elliptic.P256(), elliptic.P384(), elliptic.P521():
		// ok
	default:
		return nil, fmt.Errorf("unsupported curve")
	}

	d := new(big.Int).SetBytes(privateKeyBytes)
	params := curve.Params()
	if d.Sign() <= 0 || d.Cmp(params.N) >= 0 {
		return nil, fmt.Errorf("invalid private scalar")
	}

	x, y := curve.ScalarBaseMult(d.Bytes())
	return &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{
			Curve: curve,
			X:     x,
			Y:     y,
		},
		D: d,
	}, nil
}

// NeedsLogin always returns false for cryptographic auth since no interactive login is needed
func (c *CryptoProvider) NeedsLogin() bool {
	return false
}

// Login is not needed for cryptographic auth since authentication is cryptographic
func (c *CryptoProvider) Login(_ context.Context) error {
	return nil
}

func PrintEd25519KeyInfo(pubKey ed25519.PublicKey) {
	pubKeyString := base64.StdEncoding.EncodeToString(pubKey)
	fmt.Fprint(os.Stdout, "Expected proof record:\n")
	fmt.Fprintf(os.Stdout, "v=MCPv1; k=ed25519; p=%s\n", pubKeyString)
}

func PrintEcdsaP384KeyInfo(pubKey ecdsa.PublicKey) {
	printEcdsaKeyInfo("ecdsap384", pubKey)
}

func printEcdsaKeyInfo(k string, pubKey ecdsa.PublicKey) {
	compressed := elliptic.MarshalCompressed(pubKey.Curve, pubKey.X, pubKey.Y)
	pubKeyString := base64.StdEncoding.EncodeToString(compressed)
	fmt.Fprint(os.Stdout, "Expected proof record:\n")
	fmt.Fprintf(os.Stdout, "v=MCPv1; k=%s; p=%s\n", k, pubKeyString)
}

// exchangeTokenForRegistry exchanges signature for a registry JWT token
func (c *CryptoProvider) exchangeTokenForRegistry(ctx context.Context, domain, timestamp, signedTimestamp string) (string, error) {
	if c.registryURL == "" {
		return "", fmt.Errorf("registry URL is required for token exchange")
	}

	// Prepare the request body
	payload := map[string]string{
		"domain":           domain,
		"timestamp":        timestamp,
		"signed_timestamp": signedTimestamp,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make the token exchange request
	exchangeURL := fmt.Sprintf("%s/v0/auth/%s", c.registryURL, c.authMethod)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, exchangeURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token exchange failed with status %d: %s", resp.StatusCode, body)
	}

	var tokenResp RegistryTokenResponse
	err = json.Unmarshal(body, &tokenResp)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return tokenResp.RegistryToken, nil
}
