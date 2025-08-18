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
	"github.com/modelcontextprotocol/registry/internal/model"
)

// MockDNSResolver for testing
type MockDNSResolver struct {
	txtRecords map[string][]string
	err        error
}

func (m *MockDNSResolver) LookupTXT(_ context.Context, name string) ([]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.txtRecords[name], nil
}

func TestDNSAuthHandler_ExchangeToken(t *testing.T) {
	cfg := &config.Config{
		JWTPrivateKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
	handler := auth.NewDNSAuthHandler(cfg)

	// Generate a test key pair
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	// Create mock DNS resolver
	publicKeyB64 := base64.StdEncoding.EncodeToString(publicKey)
	mockResolver := &MockDNSResolver{
		txtRecords: map[string][]string{
			"example.com": {
				fmt.Sprintf("v=MCPv1; k=ed25519; p=%s", publicKeyB64),
			},
		},
	}
	handler.SetResolver(mockResolver)

	tests := []struct {
		name            string
		domain          string
		timestamp       string
		signedTimestamp string
		setupMock       func(*MockDNSResolver)
		expectError     bool
		errorContains   string
	}{
		{
			name:      "successful authentication",
			domain:    "example.com",
			timestamp: time.Now().UTC().Format(time.RFC3339),
			setupMock: func(_ *MockDNSResolver) {
				// Mock is already set up with valid key
			},
			expectError: false,
		},
		{
			name:      "multiple keys",
			domain:    "example.com",
			timestamp: time.Now().UTC().Format(time.RFC3339),
			setupMock: func(m *MockDNSResolver) {
				publicKey, _, err := ed25519.GenerateKey(nil)
				require.NoError(t, err)
				otherPublicKeyB64 := base64.StdEncoding.EncodeToString(publicKey)

				m.txtRecords["example.com"] = []string{
					fmt.Sprintf("v=MCPv1; k=ed25519; p=%s", "someNonsense"),
					fmt.Sprintf("v=MCPv1; k=ed25519; p=%s", publicKeyB64),
					fmt.Sprintf("v=MCPv1; k=ed25519; p=%s", otherPublicKeyB64),
				}
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
			name:      "DNS lookup failure",
			domain:    "nonexistent.com",
			timestamp: time.Now().UTC().Format(time.RFC3339),
			setupMock: func(m *MockDNSResolver) {
				m.err = fmt.Errorf("DNS lookup failed")
			},
			expectError:   true,
			errorContains: "failed to lookup DNS TXT records",
		},
		{
			name:      "no MCP TXT records",
			domain:    "nokeys.com",
			timestamp: time.Now().UTC().Format(time.RFC3339),
			setupMock: func(m *MockDNSResolver) {
				m.txtRecords["nokeys.com"] = []string{"v=spf1 include:_spf.google.com ~all"}
				m.err = nil
			},
			expectError:   true,
			errorContains: "no valid MCP public keys found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset mock resolver
			mockResolver.err = nil
			if tt.setupMock != nil {
				tt.setupMock(mockResolver)
			}

			// Generate signature if not provided
			signedTimestamp := tt.signedTimestamp
			if signedTimestamp == "" && !tt.expectError {
				signature := ed25519.Sign(privateKey, []byte(tt.timestamp))
				signedTimestamp = hex.EncodeToString(signature)
			} else if signedTimestamp == "" {
				// For error cases, generate a valid signature unless we're testing signature format
				if !strings.Contains(tt.errorContains, "signature") {
					signature := ed25519.Sign(privateKey, []byte(tt.timestamp))
					signedTimestamp = hex.EncodeToString(signature)
				} else {
					signedTimestamp = "invalid"
				}
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

				assert.Equal(t, model.AuthMethodDNS, claims.AuthMethod)
				assert.Equal(t, tt.domain, claims.AuthMethodSubject)
				assert.Len(t, claims.Permissions, 2) // domain and subdomain permissions

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
				assert.Contains(t, patterns, fmt.Sprintf("%s.*", reverseDomain))
			}
		})
	}
}
