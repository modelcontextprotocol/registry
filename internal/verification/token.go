package verification

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
)

// TokenLength defines the number of bytes for the verification token.
// 128 bits = 16 bytes provides cryptographically secure randomness
// suitable for domain ownership verification.
const TokenLength = 16

// TokenInfo contains a verification token and formatted strings for different verification methods
type TokenInfo struct {
	// Token is the raw verification token
	Token string `json:"token"`

	// DNSRecord is the formatted DNS TXT record value
	DNSRecord string `json:"dns_record"`

	// HTTPPath is the formatted HTTP challenge path
	HTTPPath string `json:"http_path"`
}

// GenerateVerificationToken generates a cryptographically secure 128-bit (16 bytes)
// random token for domain ownership verification. The token is encoded using base64url
// (RFC 4648) which is both URL-safe and DNS TXT record safe.
//
// This function is designed for use in both DNS TXT record verification
// (mcp-verify=<token>) and HTTP-01 web challenge verification
// (https://domain/.well-known/mcp-verify).
//
// Security considerations:
// - Uses crypto/rand for cryptographically secure random number generation
// - 128 bits provides 2^128 possible values, making collision probability negligible
// - Base64url encoding ensures compatibility with DNS and HTTP standards
// - Tokens should be treated as single-use and rotated regularly
//
// Returns:
// - A base64url-encoded token string suitable for verification
// - An error if the system's entropy source is unavailable
//
// Example usage:
//
//	token, err := GenerateVerificationToken()
//	if err != nil {
//	    return fmt.Errorf("failed to generate verification token: %w", err)
//	}
//	// Use token in DNS: mcp-verify=<token>
//	// Or HTTP: /.well-known/mcp-verify
func GenerateVerificationToken() (string, error) {
	// Allocate byte slice for random data
	randomBytes := make([]byte, TokenLength)

	// Generate cryptographically secure random bytes
	// crypto/rand.Read uses the operating system's entropy source
	_, err := rand.Read(randomBytes)
	if err != nil {
		return "", fmt.Errorf("failed to generate cryptographically secure random bytes: %w", err)
	}

	// Encode using base64url (RFC 4648) for URL and DNS safety
	// base64url encoding is URL-safe and doesn't contain characters
	// that would be problematic in DNS TXT records or HTTP URLs
	token := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(randomBytes)

	return token, nil
}

// GenerateTokenWithInfo generates a verification token with additional metadata
// about how to use it for different verification methods.
//
// This function generates a token and returns it along with pre-formatted
// strings for DNS TXT records and HTTP challenge paths, making it easier
// for callers to implement verification workflows.
//
// Returns:
// - TokenInfo struct containing the token and formatted verification strings
// - An error if token generation fails
//
// Example usage:
//
//	tokenInfo, err := GenerateTokenWithInfo()
//	if err != nil {
//	    return fmt.Errorf("failed to generate token info: %w", err)
//	}
//
//	fmt.Printf("Add this DNS record: %s\n", tokenInfo.DNSRecord)
//	fmt.Printf("Or serve content at: %s\n", tokenInfo.HTTPPath)
func GenerateTokenWithInfo() (*TokenInfo, error) {
	token, err := GenerateVerificationToken()
	if err != nil {
		return nil, err
	}

	return &TokenInfo{
		Token:     token,
		DNSRecord: fmt.Sprintf("mcp-verify=%s", token),
		HTTPPath:  fmt.Sprintf("/.well-known/mcp-challenge/%s", token),
	}, nil
}

// ValidateTokenFormat validates that a token string follows the expected format
// for MCP verification tokens (base64url encoding, no padding, 22 characters).
//
// This function verifies that:
// - Token is exactly 22 characters long (base64url encoding of 16 bytes)
// - Token contains only valid base64url characters (A-Z, a-z, 0-9, -, _)
// - Token contains no padding characters (=)
//
// Parameters:
// - token: The token string to validate
//
// Returns:
// - true if the token format is valid, false otherwise
func ValidateTokenFormat(token string) bool {
	// Check length (22 characters for base64url encoding of 16 bytes)
	if len(token) != 22 {
		return false
	}

	// Check for padding (shouldn't be present in base64url)
	if strings.Contains(token, "=") {
		return false
	}

	// Check that all characters are valid base64url characters
	for _, char := range token {
		if !isValidBase64URLChar(char) {
			return false
		}
	}

	return true
}

// isValidBase64URLChar checks if a character is valid for base64url encoding
func isValidBase64URLChar(char rune) bool {
	return (char >= 'A' && char <= 'Z') ||
		(char >= 'a' && char <= 'z') ||
		(char >= '0' && char <= '9') ||
		char == '-' || char == '_'
}
