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

const testDomain = "example.com"

func TestVerifyDNSRecordWithMockSuccess(t *testing.T) {
	token, err := verification.GenerateVerificationToken()
	if err != nil {
		t.Fatalf("Failed to generate test token: %v", err)
	}

	mockResolver := verification.NewMockDNSResolver()
	mockResolver.SetVerificationToken(testDomain, token)

	config := verification.DefaultDNSConfig()
	config.Resolver = mockResolver
	config.Timeout = 1 * time.Second

	result, err := verification.VerifyDNSRecordWithConfig(testDomain, token, config)

	if err != nil {
		t.Errorf("VerifyDNSRecord returned unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("VerifyDNSRecord returned nil result")
	}

	if !result.Success {
		t.Errorf("Expected success=true, got success=%t, message=%s", result.Success, result.Message)
	}

	if result.Domain != testDomain {
		t.Errorf("Result domain = %s, want %s", result.Domain, testDomain)
	}

	if result.Token != token {
		t.Errorf("Result token = %s, want %s", result.Token, token)
	}

	if mockResolver.CallCount != 1 {
		t.Errorf("Expected 1 DNS call, got %d", mockResolver.CallCount)
	}

	if mockResolver.LastDomain != testDomain {
		t.Errorf("Expected query for %s, got %s", testDomain, mockResolver.LastDomain)
	}
}

func TestVerifyDNSRecordWithMockTokenNotFound(t *testing.T) {
	token, err := verification.GenerateVerificationToken()
	if err != nil {
		t.Fatalf("Failed to generate test token: %v", err)
	}

	mockResolver := verification.NewMockDNSResolver()
	mockResolver.SetTXTRecord(testDomain, "v=spf1 -all", "some-other-record")

	config := verification.DefaultDNSConfig()
	config.Resolver = mockResolver

	result, err := verification.VerifyDNSRecordWithConfig(testDomain, token, config)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("Expected result but got nil")
	}

	if result.Success {
		t.Error("Expected verification to fail")
	}

	if !strings.Contains(result.Message, "verification token not found") {
		t.Errorf("Expected 'token not found' message, got: %s", result.Message)
	}

	if len(result.TXTRecords) != 2 {
		t.Errorf("Expected 2 TXT records, got %d", len(result.TXTRecords))
	}
}

func TestVerifyDNSRecordWithMockDNSError(t *testing.T) {
	token, err := verification.GenerateVerificationToken()
	if err != nil {
		t.Fatalf("Failed to generate test token: %v", err)
	}

	mockResolver := verification.NewMockDNSResolver()
	mockResolver.SetError(testDomain, &net.DNSError{
		Err:         "no such host",
		Name:        testDomain,
		Server:      "8.8.8.8:53",
		IsTimeout:   false,
		IsTemporary: false,
	})

	config := verification.DefaultDNSConfig()
	config.Resolver = mockResolver
	config.MaxRetries = 0

	result, err := verification.VerifyDNSRecordWithConfig(testDomain, token, config)

	var dnsErr *verification.DNSVerificationError
	if !errors.As(err, &dnsErr) {
		t.Errorf("Expected DNSVerificationError, got: %T", err)
	}

	if result == nil {
		t.Fatal("Expected result even on error")
	}

	if result.Success {
		t.Error("Expected verification to fail")
	}

	if !strings.Contains(result.Message, "failed to query DNS TXT records") {
		t.Errorf("Expected DNS query failure message, got: %s", result.Message)
	}
}

func TestVerifyDNSRecordWithMockTimeout(t *testing.T) {
	token, err := verification.GenerateVerificationToken()
	if err != nil {
		t.Fatalf("Failed to generate test token: %v", err)
	}

	mockResolver := verification.NewMockDNSResolver()
	mockResolver.Delay = 200 * time.Millisecond
	mockResolver.SetVerificationToken(testDomain, token)

	config := verification.DefaultDNSConfig()
	config.Resolver = mockResolver
	config.Timeout = 50 * time.Millisecond
	config.MaxRetries = 0

	_, err = verification.VerifyDNSRecordWithConfig(testDomain, token, config)

	if err == nil {
		t.Error("Expected timeout error")
	}

	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "timeout") {
		t.Errorf("Expected timeout-related error, got: %v", err)
	}
}

func TestMockDNSResolverHelperMethods(t *testing.T) {
	mock := verification.NewMockDNSResolver()

	token := "test-token-123"
	mock.SetVerificationToken(testDomain, token)

	records, err := mock.LookupTXT(context.Background(), testDomain)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	expected := fmt.Sprintf("mcp-verify=%s", token)
	if len(records) != 1 || records[0] != expected {
		t.Errorf("Expected [%s], got %v", expected, records)
	}

	mock.CallCount = 5
	mock.LastDomain = "test.com"
	mock.Reset()

	if mock.CallCount != 0 {
		t.Errorf("Expected CallCount=0 after reset, got %d", mock.CallCount)
	}

	if mock.LastDomain != "" {
		t.Errorf("Expected LastDomain='' after reset, got %s", mock.LastDomain)
	}

	records, err = mock.LookupTXT(context.Background(), testDomain)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(records) != 0 {
		t.Errorf("Expected no records after reset, got %v", records)
	}
}
