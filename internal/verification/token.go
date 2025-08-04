package verification

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// TokenLength defines the number of bytes for the verification token.
// 128 bits = 16 bytes provides cryptographically secure randomness
// suitable for domain ownership verification.
const TokenLength = 16

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
