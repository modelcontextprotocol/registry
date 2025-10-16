package auth_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha512"
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

// generateECDSAP384KeyPair generates an ECDSA P-384 key pair for testing
func generateECDSAP384KeyPair(t *testing.T) ([]byte, *ecdsa.PrivateKey) {
	t.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	require.NoError(t, err)

	// Compress the public key
	compressedPubKey := elliptic.MarshalCompressed(elliptic.P384(), privateKey.X, privateKey.Y)
	return compressedPubKey, privateKey
}

// signWithECDSAP384 signs a message using ECDSA P-384
func signWithECDSAP384(t *testing.T, privateKey *ecdsa.PrivateKey, message []byte) []byte {
	t.Helper()
	digest := sha512.Sum384(message)
	r, s, err := ecdsa.Sign(rand.Reader, privateKey, digest[:])
	require.NoError(t, err)

	// Convert to R || S format (48 bytes each for P-384)
	signature := make([]byte, 96)
	r.FillBytes(signature[:48])
	s.FillBytes(signature[48:])
	return signature
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
			testDomain: {
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
			domain:    testDomain,
			timestamp: time.Now().UTC().Format(time.RFC3339),
			setupMock: func(_ *MockDNSResolver) {
				// Mock is already set up with valid key
			},
			expectError: false,
		},
		{
			name:      "multiple keys",
			domain:    testDomain,
			timestamp: time.Now().UTC().Format(time.RFC3339),
			setupMock: func(m *MockDNSResolver) {
				publicKey, _, err := ed25519.GenerateKey(nil)
				require.NoError(t, err)
				otherPublicKeyB64 := base64.StdEncoding.EncodeToString(publicKey)

				m.txtRecords[testDomain] = []string{
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
			domain:        testDomain,
			timestamp:     time.Now().Add(-30 * time.Second).UTC().Format(time.RFC3339),
			expectError:   true,
			errorContains: "timestamp outside valid window",
		},
		{
			name:          "timestamp too far in the future",
			domain:        testDomain,
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
			errorContains: "no MCP public key found in DNS TXT records",
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

				assert.Equal(t, intauth.MethodDNS, claims.AuthMethod)
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

func TestDNSAuthHandler_Permissions(t *testing.T) {
	cfg := &config.Config{
		JWTPrivateKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
	handler := auth.NewDNSAuthHandler(cfg)
	jwtManager := intauth.NewJWTManager(cfg)

	// Generate a test key pair
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	publicKeyB64 := base64.StdEncoding.EncodeToString(publicKey)

	tests := []struct {
		name               string
		domain             string
		expectedPatterns   []string
		unexpectedPatterns []string
	}{
		{
			name:   "simple domain",
			domain: testDomain,
			expectedPatterns: []string{
				"com.example/*", // exact domain pattern
				"com.example.*", // subdomain pattern (DNS includes subdomains)
			},
			unexpectedPatterns: []string{
				testDomain + "/*", // should be reversed
				"*.com.example",   // wrong wildcard position
			},
		},
		{
			name:   "subdomain",
			domain: "api.example.com",
			expectedPatterns: []string{
				"com.example.api/*", // exact subdomain pattern
				"com.example.api.*", // subdomain pattern
			},
			unexpectedPatterns: []string{
				"com.example/*",            // parent domain should not be included
				"api." + testDomain + "/*", // should be reversed
			},
		},
		{
			name:   "multi-level subdomain",
			domain: "v1.api.example.com",
			expectedPatterns: []string{
				"com.example.api.v1/*", // exact pattern
				"com.example.api.v1.*", // subdomain pattern
			},
			unexpectedPatterns: []string{
				"com.example/*",        // parent domain should not be included
				"com.example.api/*",    // intermediate domain should not be included
				"v1.api.example.com/*", // should be reversed
			},
		},
		{
			name:   "single part domain",
			domain: "localhost",
			expectedPatterns: []string{
				"localhost/*", // exact pattern (no reversal needed)
				"localhost.*", // subdomain pattern
			},
			unexpectedPatterns: []string{
				"*.localhost", // wrong wildcard position
			},
		},
		{
			name:   "hyphenated domain",
			domain: "my-app.example-site.com",
			expectedPatterns: []string{
				"com.example-site.my-app/*", // exact pattern
				"com.example-site.my-app.*", // subdomain pattern
			},
			unexpectedPatterns: []string{
				"my-app.example-site.com/*", // should be reversed
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up mock resolver
			mockResolver := &MockDNSResolver{
				txtRecords: map[string][]string{
					tt.domain: {
						fmt.Sprintf("v=MCPv1; k=ed25519; p=%s", publicKeyB64),
					},
				},
			}
			handler.SetResolver(mockResolver)

			// Generate signature
			timestamp := time.Now().UTC().Format(time.RFC3339)
			signature := ed25519.Sign(privateKey, []byte(timestamp))
			signedTimestamp := hex.EncodeToString(signature)

			// Exchange token
			result, err := handler.ExchangeToken(context.Background(), tt.domain, timestamp, signedTimestamp)
			require.NoError(t, err)
			require.NotNil(t, result)

			// Validate JWT token
			claims, err := jwtManager.ValidateToken(context.Background(), result.RegistryToken)
			require.NoError(t, err)

			// Verify claims structure
			assert.Equal(t, intauth.MethodDNS, claims.AuthMethod)
			assert.Equal(t, tt.domain, claims.AuthMethodSubject)
			assert.Len(t, claims.Permissions, 2) // DNS always grants both exact and subdomain permissions

			// Extract permission patterns
			patterns := make([]string, len(claims.Permissions))
			for i, perm := range claims.Permissions {
				patterns[i] = perm.ResourcePattern
				// All permissions should be for publish action
				assert.Equal(t, intauth.PermissionActionPublish, perm.Action)
			}

			// Check expected patterns are present
			for _, expectedPattern := range tt.expectedPatterns {
				assert.Contains(t, patterns, expectedPattern, "Expected pattern %s not found", expectedPattern)
			}

			// Check unexpected patterns are not present
			for _, unexpectedPattern := range tt.unexpectedPatterns {
				assert.NotContains(t, patterns, unexpectedPattern, "Unexpected pattern %s found", unexpectedPattern)
			}

			// Verify the permission patterns work correctly with the JWT manager's HasPermission method
			for _, expectedPattern := range tt.expectedPatterns {
				// Find the permission with this pattern
				var foundPerm *intauth.Permission
				for _, perm := range claims.Permissions {
					if perm.ResourcePattern == expectedPattern {
						foundPerm = &perm
						break
					}
				}
				require.NotNil(t, foundPerm, "Permission with pattern %s not found", expectedPattern)

				// Test various resource scenarios
				if strings.HasSuffix(expectedPattern, "/*") {
					// Exact domain permissions (e.g., "com.example/*")
					basePattern := strings.TrimSuffix(expectedPattern, "/*")
					testResource := basePattern + "/my-package"
					assert.True(t, jwtManager.HasPermission(testResource, intauth.PermissionActionPublish, claims.Permissions),
						"Should have permission for %s with pattern %s", testResource, expectedPattern)
				} else if strings.HasSuffix(expectedPattern, ".*") {
					// Subdomain permissions (e.g., "com.example.*")
					basePattern := strings.TrimSuffix(expectedPattern, ".*")
					testResource := basePattern + ".subdomain/my-package"
					assert.True(t, jwtManager.HasPermission(testResource, intauth.PermissionActionPublish, claims.Permissions),
						"Should have permission for %s with pattern %s", testResource, expectedPattern)
				}
			}
		})
	}
}

func TestDNSAuthHandler_PermissionValidation(t *testing.T) {
	cfg := &config.Config{
		JWTPrivateKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
	handler := auth.NewDNSAuthHandler(cfg)
	jwtManager := intauth.NewJWTManager(cfg)

	// Generate a test key pair
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	publicKeyB64 := base64.StdEncoding.EncodeToString(publicKey)
	domain := testDomain

	// Set up mock resolver
	mockResolver := &MockDNSResolver{
		txtRecords: map[string][]string{
			domain: {
				fmt.Sprintf("v=MCPv1; k=ed25519; p=%s", publicKeyB64),
			},
		},
	}
	handler.SetResolver(mockResolver)

	// Generate signature and exchange token
	timestamp := time.Now().UTC().Format(time.RFC3339)
	signature := ed25519.Sign(privateKey, []byte(timestamp))
	signedTimestamp := hex.EncodeToString(signature)

	result, err := handler.ExchangeToken(context.Background(), domain, timestamp, signedTimestamp)
	require.NoError(t, err)

	claims, err := jwtManager.ValidateToken(context.Background(), result.RegistryToken)
	require.NoError(t, err)

	// Test permission validation scenarios
	testCases := []struct {
		name       string
		resource   string
		action     intauth.PermissionAction
		shouldPass bool
	}{
		{
			name:       "exact domain resource with publish action",
			resource:   "com.example/my-package",
			action:     intauth.PermissionActionPublish,
			shouldPass: true,
		},
		{
			name:       "subdomain resource with publish action",
			resource:   "com.example.api/my-package",
			action:     intauth.PermissionActionPublish,
			shouldPass: true,
		},
		{
			name:       "deep subdomain resource with publish action",
			resource:   "com.example.v1.api/my-package",
			action:     intauth.PermissionActionPublish,
			shouldPass: true,
		},
		{
			name:       "different domain should fail",
			resource:   "com.otherdomain/my-package",
			action:     intauth.PermissionActionPublish,
			shouldPass: false,
		},
		{
			name:       "partial domain match should fail",
			resource:   "com.example-other/my-package",
			action:     intauth.PermissionActionPublish,
			shouldPass: false,
		},
		{
			name:       "parent domain should fail",
			resource:   "com/my-package",
			action:     intauth.PermissionActionPublish,
			shouldPass: false,
		},
		{
			name:       "edit action should fail (not granted)",
			resource:   "com.example/my-package",
			action:     intauth.PermissionActionEdit,
			shouldPass: false,
		},
		{
			name:       "resource without package separator should fail",
			resource:   "com.example",
			action:     intauth.PermissionActionPublish,
			shouldPass: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			hasPermission := jwtManager.HasPermission(tc.resource, tc.action, claims.Permissions)
			if tc.shouldPass {
				assert.True(t, hasPermission, "Expected permission for resource %s with action %s", tc.resource, tc.action)
			} else {
				assert.False(t, hasPermission, "Expected no permission for resource %s with action %s", tc.resource, tc.action)
			}
		})
	}
}

func TestDNSAuthHandler_ExchangeToken_ECDSAP384(t *testing.T) {
	cfg := &config.Config{
		JWTPrivateKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
	handler := auth.NewDNSAuthHandler(cfg)

	// Generate ECDSA P-384 test key pair
	compressedPubKey, privateKey := generateECDSAP384KeyPair(t)

	// Create mock DNS resolver
	publicKeyB64 := base64.StdEncoding.EncodeToString(compressedPubKey)
	mockResolver := &MockDNSResolver{
		txtRecords: map[string][]string{
			testDomain: {
				fmt.Sprintf("v=MCPv1; k=ecdsap384; p=%s", publicKeyB64),
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
			name:        "successful ECDSA P-384 authentication",
			domain:      testDomain,
			timestamp:   time.Now().UTC().Format(time.RFC3339),
			expectError: false,
		},
		{
			name:      "multiple keys with ECDSA P-384",
			domain:    testDomain,
			timestamp: time.Now().UTC().Format(time.RFC3339),
			setupMock: func(m *MockDNSResolver) {
				// Add another ECDSA key and some other formats
				otherCompressedPubKey, _ := generateECDSAP384KeyPair(t)
				otherPublicKeyB64 := base64.StdEncoding.EncodeToString(otherCompressedPubKey)

				m.txtRecords[testDomain] = []string{
					"v=MCPv1; k=ed25519; p=someNonsense",
					fmt.Sprintf("v=MCPv1; k=ecdsap384; p=%s", publicKeyB64),
					fmt.Sprintf("v=MCPv1; k=ecdsap384; p=%s", otherPublicKeyB64),
				}
			},
			expectError: false,
		},
		{
			name:            "invalid ECDSA P-384 signature length",
			domain:          testDomain,
			timestamp:       time.Now().UTC().Format(time.RFC3339),
			signedTimestamp: "abcdef1234", // too short for ECDSA P-384
			expectError:     true,
			errorContains:   "signature verification failed", // general error when trying all keys
		},
		{
			name:      "wrong ECDSA P-384 key for signature",
			domain:    testDomain,
			timestamp: time.Now().UTC().Format(time.RFC3339),
			setupMock: func(m *MockDNSResolver) {
				// Generate different key pair for signature verification failure
				wrongCompressedPubKey, _ := generateECDSAP384KeyPair(t)
				wrongPublicKeyB64 := base64.StdEncoding.EncodeToString(wrongCompressedPubKey)
				m.txtRecords[testDomain] = []string{
					fmt.Sprintf("v=MCPv1; k=ecdsap384; p=%s", wrongPublicKeyB64),
				}
				m.err = nil
			},
			expectError:   true,
			errorContains: "invalid signature size for ECDSA P-384", // specific error when only one key
		},
		{
			name:      "invalid ECDSA P-384 key format",
			domain:    "invalidkey.com",
			timestamp: time.Now().UTC().Format(time.RFC3339),
			setupMock: func(m *MockDNSResolver) {
				// Generate a key that's too short
				shortKey := base64.StdEncoding.EncodeToString([]byte("short"))
				m.txtRecords["invalidkey.com"] = []string{
					fmt.Sprintf("v=MCPv1; k=ecdsap384; p=%s", shortKey),
				}
				m.err = nil
			},
			expectError:   true,
			errorContains: "invalid ECDSA P-384 public key size",
		},
		{
			name:      "invalid ECDSA P-384 key compression format",
			domain:    "invalidkey.com",
			timestamp: time.Now().UTC().Format(time.RFC3339),
			setupMock: func(m *MockDNSResolver) {
				// Generate a key with wrong compression byte
				invalidKey := make([]byte, 49)
				invalidKey[0] = 0x04 // uncompressed format, should be 0x02 or 0x03
				invalidKeyB64 := base64.StdEncoding.EncodeToString(invalidKey)
				m.txtRecords["invalidkey.com"] = []string{
					fmt.Sprintf("v=MCPv1; k=ecdsap384; p=%s", invalidKeyB64),
				}
				m.err = nil
			},
			expectError:   true,
			errorContains: "invalid ECDSA P-384 public key format (must be compressed, with a leading 0x02 or 0x03 byte)",
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
				signature := signWithECDSAP384(t, privateKey, []byte(tt.timestamp))
				signedTimestamp = hex.EncodeToString(signature)
			} else if signedTimestamp == "" {
				// For error cases, generate a valid signature unless we're testing signature format
				if !strings.Contains(tt.errorContains, "signature") {
					signature := signWithECDSAP384(t, privateKey, []byte(tt.timestamp))
					signedTimestamp = hex.EncodeToString(signature)
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

				assert.Equal(t, intauth.MethodDNS, claims.AuthMethod)
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

func TestDNSAuthHandler_ECDSAP384_Permissions(t *testing.T) {
	cfg := &config.Config{
		JWTPrivateKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
	handler := auth.NewDNSAuthHandler(cfg)
	jwtManager := intauth.NewJWTManager(cfg)

	// Generate ECDSA P-384 test key pair
	compressedPubKey, privateKey := generateECDSAP384KeyPair(t)
	publicKeyB64 := base64.StdEncoding.EncodeToString(compressedPubKey)

	tests := []struct {
		name               string
		domain             string
		expectedPatterns   []string
		unexpectedPatterns []string
	}{
		{
			name:   "simple domain with ECDSA P-384",
			domain: testDomain,
			expectedPatterns: []string{
				"com.example/*", // exact domain pattern
				"com.example.*", // subdomain pattern (DNS includes subdomains)
			},
			unexpectedPatterns: []string{
				testDomain + "/*", // should be reversed
				"*.com.example",   // wrong wildcard position
			},
		},
		{
			name:   "subdomain with ECDSA P-384",
			domain: "api.example.com",
			expectedPatterns: []string{
				"com.example.api/*", // exact subdomain pattern
				"com.example.api.*", // subdomain pattern
			},
			unexpectedPatterns: []string{
				"com.example/*",            // parent domain should not be included
				"api." + testDomain + "/*", // should be reversed
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up mock resolver
			mockResolver := &MockDNSResolver{
				txtRecords: map[string][]string{
					tt.domain: {
						fmt.Sprintf("v=MCPv1; k=ecdsap384; p=%s", publicKeyB64),
					},
				},
			}
			handler.SetResolver(mockResolver)

			// Generate signature
			timestamp := time.Now().UTC().Format(time.RFC3339)
			signature := signWithECDSAP384(t, privateKey, []byte(timestamp))
			signedTimestamp := hex.EncodeToString(signature)

			// Exchange token
			result, err := handler.ExchangeToken(context.Background(), tt.domain, timestamp, signedTimestamp)
			require.NoError(t, err)
			require.NotNil(t, result)

			// Validate JWT token
			claims, err := jwtManager.ValidateToken(context.Background(), result.RegistryToken)
			require.NoError(t, err)

			// Verify claims structure
			assert.Equal(t, intauth.MethodDNS, claims.AuthMethod)
			assert.Equal(t, tt.domain, claims.AuthMethodSubject)
			assert.Len(t, claims.Permissions, 2) // DNS always grants both exact and subdomain permissions

			// Extract permission patterns
			patterns := make([]string, len(claims.Permissions))
			for i, perm := range claims.Permissions {
				patterns[i] = perm.ResourcePattern
				// All permissions should be for publish action
				assert.Equal(t, intauth.PermissionActionPublish, perm.Action)
			}

			// Check expected patterns are present
			for _, expectedPattern := range tt.expectedPatterns {
				assert.Contains(t, patterns, expectedPattern, "Expected pattern %s not found", expectedPattern)
			}

			// Check unexpected patterns are not present
			for _, unexpectedPattern := range tt.unexpectedPatterns {
				assert.NotContains(t, patterns, unexpectedPattern, "Unexpected pattern %s found", unexpectedPattern)
			}
		})
	}
}

func TestDNSAuthHandler_ECDSAP384_PermissionValidation(t *testing.T) {
	cfg := &config.Config{
		JWTPrivateKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
	handler := auth.NewDNSAuthHandler(cfg)
	jwtManager := intauth.NewJWTManager(cfg)

	// Generate ECDSA P-384 test key pair
	compressedPubKey, privateKey := generateECDSAP384KeyPair(t)
	publicKeyB64 := base64.StdEncoding.EncodeToString(compressedPubKey)
	domain := testDomain

	// Set up mock resolver
	mockResolver := &MockDNSResolver{
		txtRecords: map[string][]string{
			domain: {
				fmt.Sprintf("v=MCPv1; k=ecdsap384; p=%s", publicKeyB64),
			},
		},
	}
	handler.SetResolver(mockResolver)

	// Generate signature and exchange token
	timestamp := time.Now().UTC().Format(time.RFC3339)
	signature := signWithECDSAP384(t, privateKey, []byte(timestamp))
	signedTimestamp := hex.EncodeToString(signature)

	result, err := handler.ExchangeToken(context.Background(), domain, timestamp, signedTimestamp)
	require.NoError(t, err)

	claims, err := jwtManager.ValidateToken(context.Background(), result.RegistryToken)
	require.NoError(t, err)

	// Test permission validation scenarios (same as Ed25519, algorithm shouldn't affect permissions)
	testCases := []struct {
		name       string
		resource   string
		action     intauth.PermissionAction
		shouldPass bool
	}{
		{
			name:       "exact domain resource with publish action",
			resource:   "com.example/my-package",
			action:     intauth.PermissionActionPublish,
			shouldPass: true,
		},
		{
			name:       "subdomain resource with publish action",
			resource:   "com.example.api/my-package",
			action:     intauth.PermissionActionPublish,
			shouldPass: true,
		},
		{
			name:       "deep subdomain resource with publish action",
			resource:   "com.example.v1.api/my-package",
			action:     intauth.PermissionActionPublish,
			shouldPass: true,
		},
		{
			name:       "different domain should fail",
			resource:   "com.otherdomain/my-package",
			action:     intauth.PermissionActionPublish,
			shouldPass: false,
		},
		{
			name:       "edit action should fail (not granted)",
			resource:   "com.example/my-package",
			action:     intauth.PermissionActionEdit,
			shouldPass: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			hasPermission := jwtManager.HasPermission(tc.resource, tc.action, claims.Permissions)
			if tc.shouldPass {
				assert.True(t, hasPermission, "Expected permission for resource %s with action %s", tc.resource, tc.action)
			} else {
				assert.False(t, hasPermission, "Expected no permission for resource %s with action %s", tc.resource, tc.action)
			}
		})
	}
}

func TestDNSAuthHandler_Ed25519_vs_ECDSAP384_Equivalence(t *testing.T) {
	cfg := &config.Config{
		JWTPrivateKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
	handler := auth.NewDNSAuthHandler(cfg)
	jwtManager := intauth.NewJWTManager(cfg)

	// Generate Ed25519 key pair
	ed25519PubKey, ed25519PrivKey, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	ed25519PubKeyB64 := base64.StdEncoding.EncodeToString(ed25519PubKey)

	// Generate ECDSA P-384 key pair
	ecdsaCompressedPubKey, ecdsaPrivKey := generateECDSAP384KeyPair(t)
	ecdsaPubKeyB64 := base64.StdEncoding.EncodeToString(ecdsaCompressedPubKey)

	testDomains := []string{testDomain, "api.example.com", "v1.api.example.com"}

	for _, domain := range testDomains {
		t.Run(fmt.Sprintf("domain_%s", domain), func(t *testing.T) {
			timestamp := time.Now().UTC().Format(time.RFC3339)

			// Test Ed25519
			mockResolver := &MockDNSResolver{
				txtRecords: map[string][]string{
					domain: {
						fmt.Sprintf("v=MCPv1; k=ed25519; p=%s", ed25519PubKeyB64),
					},
				},
			}
			handler.SetResolver(mockResolver)

			ed25519Signature := ed25519.Sign(ed25519PrivKey, []byte(timestamp))
			ed25519Result, err := handler.ExchangeToken(context.Background(), domain, timestamp, hex.EncodeToString(ed25519Signature))
			require.NoError(t, err)

			// Test ECDSA P-384
			mockResolver.txtRecords[domain] = []string{
				fmt.Sprintf("v=MCPv1; k=ecdsap384; p=%s", ecdsaPubKeyB64),
			}

			ecdsaSignature := signWithECDSAP384(t, ecdsaPrivKey, []byte(timestamp))
			ecdsaResult, err := handler.ExchangeToken(context.Background(), domain, timestamp, hex.EncodeToString(ecdsaSignature))
			require.NoError(t, err)

			// Validate both tokens produce identical claims structure
			ed25519Claims, err := jwtManager.ValidateToken(context.Background(), ed25519Result.RegistryToken)
			require.NoError(t, err)

			ecdsaClaims, err := jwtManager.ValidateToken(context.Background(), ecdsaResult.RegistryToken)
			require.NoError(t, err)

			// Compare claims (excluding token-specific fields like timestamps)
			assert.Equal(t, ed25519Claims.AuthMethod, ecdsaClaims.AuthMethod)
			assert.Equal(t, ed25519Claims.AuthMethodSubject, ecdsaClaims.AuthMethodSubject)
			assert.Equal(t, len(ed25519Claims.Permissions), len(ecdsaClaims.Permissions))

			// Compare permission patterns
			ed25519Patterns := make([]string, len(ed25519Claims.Permissions))
			ecdsaPatterns := make([]string, len(ecdsaClaims.Permissions))

			for i, perm := range ed25519Claims.Permissions {
				ed25519Patterns[i] = perm.ResourcePattern
			}
			for i, perm := range ecdsaClaims.Permissions {
				ecdsaPatterns[i] = perm.ResourcePattern
			}

			assert.ElementsMatch(t, ed25519Patterns, ecdsaPatterns, "Permission patterns should be identical for both algorithms")

			// Test that both tokens grant identical permissions for various resources
			testResources := []string{
				"com.example/package1",
				"com.example.api/package2",
				"com.example.v1.api/package3",
				"com.otherdomain/package4",
			}

			for _, resource := range testResources {
				ed25519HasPerm := jwtManager.HasPermission(resource, intauth.PermissionActionPublish, ed25519Claims.Permissions)
				ecdsaHasPerm := jwtManager.HasPermission(resource, intauth.PermissionActionPublish, ecdsaClaims.Permissions)
				assert.Equal(t, ed25519HasPerm, ecdsaHasPerm, "Permission mismatch for resource %s between Ed25519 and ECDSA P-384", resource)
			}
		})
	}
}

func TestDNSAuthHandler_Mixed_Algorithm_Support(t *testing.T) {
	cfg := &config.Config{
		JWTPrivateKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
	handler := auth.NewDNSAuthHandler(cfg)

	// Generate both key pairs
	ed25519PubKey, ed25519PrivKey, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	ed25519PubKeyB64 := base64.StdEncoding.EncodeToString(ed25519PubKey)

	ecdsaCompressedPubKey, ecdsaPrivKey := generateECDSAP384KeyPair(t)
	ecdsaPubKeyB64 := base64.StdEncoding.EncodeToString(ecdsaCompressedPubKey)

	t.Run("multiple_algorithms_in_dns", func(t *testing.T) {
		// Set up DNS records with both algorithms
		mockResolver := &MockDNSResolver{
			txtRecords: map[string][]string{
				testDomain: {
					fmt.Sprintf("v=MCPv1; k=ed25519; p=%s", ed25519PubKeyB64),
					fmt.Sprintf("v=MCPv1; k=ecdsap384; p=%s", ecdsaPubKeyB64),
					"v=spf1 include:_spf.google.com ~all", // unrelated record
				},
			},
		}
		handler.SetResolver(mockResolver)

		timestamp := time.Now().UTC().Format(time.RFC3339)

		// Test that Ed25519 signature works when both keys are present
		ed25519Signature := ed25519.Sign(ed25519PrivKey, []byte(timestamp))
		result, err := handler.ExchangeToken(context.Background(), testDomain, timestamp, hex.EncodeToString(ed25519Signature))
		require.NoError(t, err)
		assert.NotNil(t, result)

		// Test that ECDSA P-384 signature works when both keys are present
		ecdsaSignature := signWithECDSAP384(t, ecdsaPrivKey, []byte(timestamp))
		result, err = handler.ExchangeToken(context.Background(), testDomain, timestamp, hex.EncodeToString(ecdsaSignature))
		require.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("wrong_signature_for_algorithm", func(t *testing.T) {
		// Set up DNS records with both algorithms
		mockResolver := &MockDNSResolver{
			txtRecords: map[string][]string{
				testDomain: {
					fmt.Sprintf("v=MCPv1; k=ed25519; p=%s", ed25519PubKeyB64),
					fmt.Sprintf("v=MCPv1; k=ecdsap384; p=%s", ecdsaPubKeyB64),
				},
			},
		}
		handler.SetResolver(mockResolver)

		timestamp := time.Now().UTC().Format(time.RFC3339)

		// Use ECDSA signature with domain that has both keys - should still work
		// because the system tries all available keys
		ecdsaSignature := signWithECDSAP384(t, ecdsaPrivKey, []byte(timestamp))
		result, err := handler.ExchangeToken(context.Background(), testDomain, timestamp, hex.EncodeToString(ecdsaSignature))
		require.NoError(t, err)
		assert.NotNil(t, result)
	})
}
