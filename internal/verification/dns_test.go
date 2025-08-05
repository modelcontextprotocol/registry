package verification_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/registry/internal/verification"
)

func TestVerifyDNSRecordSuccess(t *testing.T) {
	token, err := verification.GenerateVerificationToken()
	if err != nil {
		t.Fatalf("Failed to generate test token: %v", err)
	}

	domain := testDomain

	// Create mock resolver with the verification token
	mockResolver := verification.NewMockDNSResolver()
	mockResolver.SetVerificationToken(domain, token)

	// Use custom config with mock resolver
	config := verification.DefaultDNSConfig()
	config.Resolver = mockResolver

	result, err := verification.VerifyDNSRecordWithConfig(domain, token, config)
	if err != nil {
		t.Errorf("VerifyDNSRecord returned unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("VerifyDNSRecord returned nil result")
	}

	if !result.Success {
		t.Errorf("Expected successful verification, got: %s", result.Message)
	}

	if result.Domain != domain {
		t.Errorf("Result domain = %s, want %s", result.Domain, domain)
	}

	if result.Token != token {
		t.Errorf("Result token = %s, want %s", result.Token, token)
	}

	// Verify the mock was called
	if mockResolver.CallCount != 1 {
		t.Errorf("Expected 1 DNS call, got %d", mockResolver.CallCount)
	}

	if mockResolver.LastDomain != domain {
		t.Errorf("Expected DNS query for %s, got %s", domain, mockResolver.LastDomain)
	}

	t.Logf("DNS verification result: %+v", result)
}

func TestVerifyDNSRecordTokenNotFound(t *testing.T) {
	token, err := verification.GenerateVerificationToken()
	if err != nil {
		t.Fatalf("Failed to generate test token: %v", err)
	}

	domain := testDomain

	// Create mock resolver with different TXT records (no verification token)
	mockResolver := verification.NewMockDNSResolver()
	mockResolver.SetTXTRecord(domain, "v=spf1 -all", "some-other-record")

	// Use custom config with mock resolver
	config := verification.DefaultDNSConfig()
	config.Resolver = mockResolver

	result, err := verification.VerifyDNSRecordWithConfig(domain, token, config)
	if err != nil {
		t.Errorf("VerifyDNSRecord returned unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("VerifyDNSRecord returned nil result")
	}

	if result.Success {
		t.Error("Expected verification to fail when token is not found")
	}

	if !strings.Contains(result.Message, "verification token not found") {
		t.Errorf("Expected 'token not found' message, got: %s", result.Message)
	}

	if result.Domain != domain {
		t.Errorf("Result domain = %s, want %s", result.Domain, domain)
	}

	if result.Token != token {
		t.Errorf("Result token = %s, want %s", result.Token, token)
	}

	// Verify TXT records are included in result
	if len(result.TXTRecords) != 2 {
		t.Errorf("Expected 2 TXT records in result, got %d", len(result.TXTRecords))
	}

	t.Logf("DNS verification result: %+v", result)
}

func TestVerifyDNSRecordInvalidInputs(t *testing.T) {
	tests := []struct {
		name          string
		domain        string
		token         string
		expectError   bool
		errorContains string
	}{
		{
			name:          "empty domain",
			domain:        "",
			token:         "validtoken123456789012",
			expectError:   true,
			errorContains: "domain cannot be empty",
		},
		{
			name:          "empty token",
			domain:        testDomain,
			token:         "",
			expectError:   true,
			errorContains: "token cannot be empty",
		},
		{
			name:          "invalid token format",
			domain:        testDomain,
			token:         "invalid-token!@#",
			expectError:   true,
			errorContains: "invalid token format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := verification.VerifyDNSRecord(tt.domain, tt.token)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Error message %q does not contain %q", err.Error(), tt.errorContains)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}

			t.Logf("Result: %+v, Error: %v", result, err)
		})
	}
}

func TestVerifyDNSRecordTokenFormatValidation(t *testing.T) {
	token, err := verification.GenerateVerificationToken()
	if err != nil {
		t.Fatalf("Failed to generate test token: %v", err)
	}

	domain := testDomain

	// Create mock resolver with the verification token
	mockResolver := verification.NewMockDNSResolver()
	mockResolver.SetVerificationToken(domain, token)

	// Use custom config with mock resolver
	config := verification.DefaultDNSConfig()
	config.Resolver = mockResolver

	result, err := verification.VerifyDNSRecordWithConfig(domain, token, config)

	if err != nil {
		var dnsErr *verification.DNSVerificationError
		if errors.As(err, &dnsErr) {
			if strings.Contains(dnsErr.Message, "invalid token format") {
				t.Errorf("Unexpected token format validation error: %v", err)
			}
		}
	}

	if result == nil {
		t.Fatal("Expected result but got nil")
	}

	if !result.Success {
		t.Errorf("Expected successful verification, got: %s", result.Message)
	}

	if result.Domain != domain {
		t.Errorf("Result domain = %s, want %s", result.Domain, domain)
	}

	if result.Token != token {
		t.Errorf("Result token = %s, want %s", result.Token, token)
	}
}

func TestVerifyDNSRecordWithConfigTimeout(t *testing.T) {
	token, err := verification.GenerateVerificationToken()
	if err != nil {
		t.Fatalf("Failed to generate test token: %v", err)
	}

	// Create mock resolver that simulates a timeout
	mockResolver := verification.NewMockDNSResolver()
	mockResolver.Delay = 200 * time.Millisecond // Longer than the config timeout

	config := &verification.DNSVerificationConfig{
		Timeout:            100 * time.Millisecond,
		MaxRetries:         0,
		RetryDelay:         0,
		UseSecureResolvers: false,
		CustomResolvers:    []string{},
		Resolver:           mockResolver,
	}

	domain := testDomain
	result, err := verification.VerifyDNSRecordWithConfig(domain, token, config)

	if err == nil {
		t.Error("Expected timeout error but got none")
	} else {
		t.Logf("DNS query failed as expected: %v", err)
		// Verify it's a context timeout error
		if !strings.Contains(err.Error(), "context deadline exceeded") {
			t.Errorf("Expected context deadline exceeded error, got: %v", err)
		}
	}

	if result == nil {
		t.Fatal("Expected result but got nil")
	}

	if result.Duration == "" {
		t.Error("Expected duration to be populated")
	}

	t.Logf("Verification completed in: %s", result.Duration)
}

func TestDefaultDNSConfig(t *testing.T) {
	config := verification.DefaultDNSConfig()

	if config == nil {
		t.Fatal("DefaultDNSConfig returned nil")
	}

	if config.Timeout <= 0 {
		t.Error("Default timeout should be positive")
	}

	if config.MaxRetries < 0 {
		t.Error("Default max retries should be non-negative")
	}

	if config.RetryDelay <= 0 {
		t.Error("Default retry delay should be positive")
	}

	if !config.UseSecureResolvers {
		t.Error("Default should use secure resolvers")
	}

	if len(config.CustomResolvers) == 0 {
		t.Error("Default should have custom resolvers configured")
	}

	t.Logf("Default DNS config: %+v", config)
}

func TestVerifyDNSRecordWithCustomPrefix(t *testing.T) {
	token, err := verification.GenerateVerificationToken()
	if err != nil {
		t.Fatalf("Failed to generate test token: %v", err)
	}

	domain := testDomain
	customPrefix := "my-custom-prefix"

	// Create mock resolver with custom prefix verification token
	mockResolver := verification.NewMockDNSResolver()
	customRecord := fmt.Sprintf("%s=%s", customPrefix, token)
	mockResolver.SetTXTRecord(domain, customRecord)

	// Use custom config with custom record prefix
	config := verification.DefaultDNSConfig()
	config.Resolver = mockResolver
	config.RecordPrefix = customPrefix

	result, err := verification.VerifyDNSRecordWithConfig(domain, token, config)
	if err != nil {
		t.Errorf("VerifyDNSRecord returned unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("VerifyDNSRecord returned nil result")
	}

	if !result.Success {
		t.Errorf("Expected successful verification with custom prefix, got: %s", result.Message)
	}

	if result.Domain != domain {
		t.Errorf("Result domain = %s, want %s", result.Domain, domain)
	}

	if result.Token != token {
		t.Errorf("Result token = %s, want %s", result.Token, token)
	}

	// Verify the mock was called
	if mockResolver.CallCount != 1 {
		t.Errorf("Expected 1 DNS call, got %d", mockResolver.CallCount)
	}

	if mockResolver.LastDomain != domain {
		t.Errorf("Expected DNS query for %s, got %s", domain, mockResolver.LastDomain)
	}

	t.Logf("DNS verification with custom prefix '%s' successful: %+v", customPrefix, result)
}

func TestVerifyDNSRecordCustomPrefixFailsWithWrongRecord(t *testing.T) {
	token, err := verification.GenerateVerificationToken()
	if err != nil {
		t.Fatalf("Failed to generate test token: %v", err)
	}

	domain := testDomain
	customPrefix := "my-custom-prefix"

	// Create mock resolver with default prefix (should fail with custom prefix config)
	mockResolver := verification.NewMockDNSResolver()
	defaultRecord := fmt.Sprintf("mcp-verify=%s", token)
	mockResolver.SetTXTRecord(domain, defaultRecord)

	// Use custom config with custom record prefix
	config := verification.DefaultDNSConfig()
	config.Resolver = mockResolver
	config.RecordPrefix = customPrefix

	result, err := verification.VerifyDNSRecordWithConfig(domain, token, config)
	if err != nil {
		t.Errorf("VerifyDNSRecord returned unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("VerifyDNSRecord returned nil result")
	}

	if result.Success {
		t.Error("Expected verification to fail when custom prefix doesn't match record")
	}

	if !strings.Contains(result.Message, "verification token not found") {
		t.Errorf("Expected 'token not found' message, got: %s", result.Message)
	}

	t.Logf("DNS verification correctly failed with custom prefix when record has default prefix: %+v", result)
}

func TestDNSVerificationError(t *testing.T) {
	baseErr := errors.New("base network error")
	dnsErr := &verification.DNSVerificationError{
		Domain:  testDomain,
		Token:   "test-token",
		Message: "DNS query failed",
		Cause:   baseErr,
	}

	errMsg := dnsErr.Error()
	if !strings.Contains(errMsg, testDomain) {
		t.Errorf("Error message should contain domain: %s", errMsg)
	}

	if !strings.Contains(errMsg, "DNS query failed") {
		t.Errorf("Error message should contain message: %s", errMsg)
	}

	if !strings.Contains(errMsg, "base network error") {
		t.Errorf("Error message should contain cause: %s", errMsg)
	}

	unwrapped := errors.Unwrap(dnsErr)
	if !errors.Is(unwrapped, baseErr) {
		t.Errorf("Unwrap should return base error, got: %v", unwrapped)
	}
}

func TestIsRetryableDNSError(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		shouldRetry bool
	}{
		{
			name:        "nil error",
			err:         nil,
			shouldRetry: false,
		},
		{
			name:        "context deadline exceeded",
			err:         context.DeadlineExceeded,
			shouldRetry: true,
		},
		{
			name:        "temporary DNS error",
			err:         &net.DNSError{Err: "server failure", IsTemporary: true},
			shouldRetry: true,
		},
		{
			name:        "non-temporary DNS error",
			err:         &net.DNSError{Err: "no such host", IsTemporary: false},
			shouldRetry: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := verification.IsRetryableDNSError(tt.err)
			if result != tt.shouldRetry {
				t.Errorf("isRetryableDNSError(%v) = %t, want %t", tt.err, result, tt.shouldRetry)
			}
		})
	}
}

func TestDNSRecordFormat(t *testing.T) {
	tokenInfo, err := verification.GenerateTokenWithInfo()
	if err != nil {
		t.Fatalf("Failed to generate token info: %v", err)
	}

	expectedFormat := "mcp-verify=" + tokenInfo.Token
	if tokenInfo.DNSRecord != expectedFormat {
		t.Errorf("DNS record format mismatch: got %s, want %s", tokenInfo.DNSRecord, expectedFormat)
	}

	t.Logf("Expected DNS record format: %s", expectedFormat)
	t.Logf("Generated DNS record format: %s", tokenInfo.DNSRecord)
}
