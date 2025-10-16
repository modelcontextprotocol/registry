package auth

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	registryURL     string
	domain          string
	privateKey      string
	cryptoAlgorithm CryptoAlgorithm
	authMethod      string
}

// GetToken retrieves the registry JWT token using cryptographic authentication
func (c *CryptoProvider) GetToken(ctx context.Context) (string, error) {
	if c.domain == "" {
		return "", fmt.Errorf("%s domain is required", c.authMethod)
	}

	if c.privateKey == "" {
		return "", fmt.Errorf("%s private key (hex) is required", c.authMethod)
	}

	// Decode private key from hex
	privateKeyBytes, err := hex.DecodeString(c.privateKey)
	if err != nil {
		return "", fmt.Errorf("invalid hex private key format: %w", err)
	}

	// Generate current timestamp
	timestamp := time.Now().UTC().Format(time.RFC3339)
	signedTimestamp, err := c.signMessage(privateKeyBytes, []byte(timestamp))
	if err != nil {
		return "", fmt.Errorf("failed to sign timestamp: %w", err)
	}
	signedTimestampHex := hex.EncodeToString(signedTimestamp)

	// Exchange signature for registry token
	registryToken, err := c.exchangeTokenForRegistry(ctx, c.domain, timestamp, signedTimestampHex)
	if err != nil {
		return "", fmt.Errorf("failed to exchange %s signature: %w", c.authMethod, err)
	}

	return registryToken, nil
}

func (c *CryptoProvider) signMessage(privateKeyBytes []byte, message []byte) ([]byte, error) {
	switch c.cryptoAlgorithm {
	case AlgorithmEd25519:
		if len(privateKeyBytes) != ed25519.SeedSize {
			return nil, fmt.Errorf("invalid seed length: expected %d bytes, got %d", ed25519.SeedSize, len(privateKeyBytes))
		}

		privateKey := ed25519.NewKeyFromSeed(privateKeyBytes)
		signature := ed25519.Sign(privateKey, message)
		return signature, nil
	case AlgorithmECDSAP384:
		if len(privateKeyBytes) != 48 {
			return nil, fmt.Errorf("invalid seed length for ECDSA P-384: expected 48 bytes, got %d", len(privateKeyBytes))
		}

		digest := sha512.Sum384(message)
		curve := elliptic.P384()
		privateKey, err := ecdsa.ParseRawPrivateKey(curve, privateKeyBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse ECDSA private key: %w", err)
		}
		r, s, err := ecdsa.Sign(rand.Reader, privateKey, digest[:])
		if err != nil {
			return nil, fmt.Errorf("failed to sign message: %w", err)
		}
		signature := append(r.Bytes(), s.Bytes()...)
		return signature, nil
	default:
		return nil, fmt.Errorf("unsupported crypto algorithm: %s", c.cryptoAlgorithm)
	}
}

// NeedsLogin always returns false for cryptographic auth since no interactive login is needed
func (c *CryptoProvider) NeedsLogin() bool {
	return false
}

// Login is not needed for cryptographic auth since authentication is cryptographic
func (c *CryptoProvider) Login(_ context.Context) error {
	return nil
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
