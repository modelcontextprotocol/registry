package auth_test

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/modelcontextprotocol/registry/internal/api/handlers/v0/auth"
	intauth "github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
)

// MockHTTPKeyFetcher for testing
type MockHTTPKeyFetcher struct {
	keyResponses map[string]string
	err          error
}

func (m *MockHTTPKeyFetcher) FetchKey(_ context.Context, domain string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.keyResponses[domain], nil
}

func TestHTTPAuthHandler_ExchangeToken(t *testing.T) {
	cfg := &config.Config{
		JWTPrivateKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
	handler := auth.NewHTTPAuthHandler(cfg)

	// Generate a test key pair
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	// Create mock HTTP key fetcher
	publicKeyB64 := base64.StdEncoding.EncodeToString(publicKey)
	mockFetcher := &MockHTTPKeyFetcher{
		keyResponses: map[string]string{
			"example.com": fmt.Sprintf("v=MCPv1; k=ed25519; p=%s", publicKeyB64),
		},
	}
	handler.SetFetcher(mockFetcher)

	tests := []struct {
		name            string
		domain          string
		timestamp       string
		signedTimestamp string
		setupMock       func(*MockHTTPKeyFetcher)
		expectError     bool
		errorContains   string
	}{
		{
			name:      "successful authentication",
			domain:    "example.com",
			timestamp: time.Now().UTC().Format(time.RFC3339),
			setupMock: func(_ *MockHTTPKeyFetcher) {
				// Mock is already set up with valid key
			},
			expectError: false,
		},
		{
			name:          "invalid domain format",
			domain:        "invalid..domain",
			timestamp:     time.Now().UTC().Format(time.RFC3339),
			expectError:   true,
			errorContains: "invalid domain format",
		},
		{
			name:          "invalid timestamp format",
			domain:        "example.com",
			timestamp:     "invalid-timestamp",
			expectError:   true,
			errorContains: "invalid timestamp format",
		},
		{
			name:          "timestamp too old",
			domain:        "example.com",
			timestamp:     time.Now().Add(-30 * time.Second).UTC().Format(time.RFC3339),
			expectError:   true,
			errorContains: "timestamp outside valid window",
		},
		{
			name:          "timestamp too far in the future",
			domain:        "example.com",
			timestamp:     time.Now().Add(30 * time.Second).UTC().Format(time.RFC3339),
			expectError:   true,
			errorContains: "timestamp outside valid window",
		},
		{
			name:            "invalid signature format",
			domain:          "example.com",
			timestamp:       time.Now().UTC().Format(time.RFC3339),
			signedTimestamp: "invalid-hex",
			expectError:     true,
			errorContains:   "invalid signature format",
		},
		{
			name:            "signature wrong length",
			domain:          "example.com",
			timestamp:       time.Now().UTC().Format(time.RFC3339),
			signedTimestamp: "abcdef", // too short
			expectError:     true,
			errorContains:   "invalid signature length",
		},
		{
			name:      "HTTP key fetch failure",
			domain:    "nonexistent.com",
			timestamp: time.Now().UTC().Format(time.RFC3339),
			setupMock: func(m *MockHTTPKeyFetcher) {
				m.err = fmt.Errorf("HTTP 404: not found")
			},
			expectError:   true,
			errorContains: "failed to fetch public key",
		},
		{
			name:      "invalid key format",
			domain:    "invalidkey.com",
			timestamp: time.Now().UTC().Format(time.RFC3339),
			setupMock: func(m *MockHTTPKeyFetcher) {
				m.keyResponses["invalidkey.com"] = "invalid key format"
				m.err = nil
			},
			expectError:   true,
			errorContains: "invalid key format",
		},
		{
			name:      "invalid base64 key",
			domain:    "badkey.com",
			timestamp: time.Now().UTC().Format(time.RFC3339),
			setupMock: func(m *MockHTTPKeyFetcher) {
				m.keyResponses["badkey.com"] = "v=MCPv1; k=ed25519; p=invalid-base64!!!"
				m.err = nil
			},
			expectError:   true,
			errorContains: "failed to decode base64 public key",
		},
		{
			name:      "wrong key size",
			domain:    "wrongsize.com",
			timestamp: time.Now().UTC().Format(time.RFC3339),
			setupMock: func(m *MockHTTPKeyFetcher) {
				// Generate a key that's too short
				shortKey := base64.StdEncoding.EncodeToString([]byte("short"))
				m.keyResponses["wrongsize.com"] = fmt.Sprintf("v=MCPv1; k=ed25519; p=%s", shortKey)
				m.err = nil
			},
			expectError:   true,
			errorContains: "invalid public key length",
		},
		{
			name:      "signature verification failure",
			domain:    "example.com",
			timestamp: time.Now().UTC().Format(time.RFC3339),
			setupMock: func(m *MockHTTPKeyFetcher) {
				// Generate different key pair for signature verification failure
				wrongPublicKey, _, err := ed25519.GenerateKey(nil)
				require.NoError(t, err)
				wrongPublicKeyB64 := base64.StdEncoding.EncodeToString(wrongPublicKey)
				m.keyResponses["example.com"] = fmt.Sprintf("v=MCPv1; k=ed25519; p=%s", wrongPublicKeyB64)
				m.err = nil
			},
			expectError:   true,
			errorContains: "signature verification failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset mock fetcher
			mockFetcher.err = nil
			if tt.setupMock != nil {
				tt.setupMock(mockFetcher)
			}

			// Generate signature if not provided
			signedTimestamp := tt.signedTimestamp
			if signedTimestamp == "" {
				// Generate a valid signature for all cases
				signature := ed25519.Sign(privateKey, []byte(tt.timestamp))
				signedTimestamp = hex.EncodeToString(signature)
			}

			// Call the handler
			result, err := handler.ExchangeToken(context.Background(), tt.domain, tt.timestamp, signedTimestamp)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.NotEmpty(t, result.RegistryToken)

				// Verify the token contains expected claims
				jwtManager := intauth.NewJWTManager(cfg)
				claims, err := jwtManager.ValidateToken(context.Background(), result.RegistryToken)
				require.NoError(t, err)

				assert.Equal(t, intauth.MethodHTTP, claims.AuthMethod)
				assert.Equal(t, tt.domain, claims.AuthMethodSubject)
				assert.Len(t, claims.Permissions, 1) // domain permissions only

				// Check permissions use reverse DNS patterns
				patterns := make([]string, len(claims.Permissions))
				for i, perm := range claims.Permissions {
					patterns[i] = perm.ResourcePattern
				}
				// Convert domain to reverse DNS for expected patterns
				parts := strings.Split(tt.domain, ".")
				for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
					parts[i], parts[j] = parts[j], parts[i]
				}
				reverseDomain := strings.Join(parts, ".")
				assert.Contains(t, patterns, fmt.Sprintf("%s/*", reverseDomain))
			}
		})
	}
}

func TestDefaultHTTPKeyFetcher_FetchKey(t *testing.T) {
	// This test would require a real HTTP server or more sophisticated mocking
	// For now, we'll test the basic structure
	fetcher := auth.NewDefaultHTTPKeyFetcher()
	assert.NotNil(t, fetcher)

	// Test that it returns an error for non-existent domains
	// (This will fail with network error, which is expected)
	_, err := fetcher.FetchKey(context.Background(), "nonexistent-test-domain-12345.com")
	assert.Error(t, err)
}
