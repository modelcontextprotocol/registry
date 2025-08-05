package verification

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"time"
)

// DNSVerificationError represents errors that can occur during DNS verification
type DNSVerificationError struct {
	Domain  string
	Token   string
	Message string
	Cause   error
}

func (e *DNSVerificationError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("DNS verification failed for domain %s: %s (caused by: %v)", e.Domain, e.Message, e.Cause)
	}
	return fmt.Sprintf("DNS verification failed for domain %s: %s", e.Domain, e.Message)
}

func (e *DNSVerificationError) Unwrap() error {
	return e.Cause
}

// DNSVerificationResult represents the result of a DNS verification attempt
type DNSVerificationResult struct {
	Success    bool     `json:"success"`
	Domain     string   `json:"domain"`
	Token      string   `json:"token"`
	Message    string   `json:"message"`
	TXTRecords []string `json:"txt_records,omitempty"`
	Duration   string   `json:"duration"`
}

// DNSVerificationConfig holds configuration for DNS verification
type DNSVerificationConfig struct {
	// Timeout for DNS queries (default: 10 seconds)
	Timeout time.Duration

	// MaxRetries for transient failures (default: 3)
	MaxRetries int

	// RetryDelay base delay between retries (default: 1 second)
	RetryDelay time.Duration

	// UseSecureResolvers enables use of secure DNS resolvers
	UseSecureResolvers bool

	// CustomResolvers allows specifying custom DNS servers
	CustomResolvers []string

	// RecordPrefix specifies the prefix for DNS TXT records (default: "mcp-verify")
	RecordPrefix string

	// Resolver allows injecting a custom DNS resolver (primarily for testing)
	Resolver DNSResolver
}

// DefaultDNSConfig returns the default configuration for DNS verification
func DefaultDNSConfig() *DNSVerificationConfig {
	return &DNSVerificationConfig{
		Timeout:            10 * time.Second,
		MaxRetries:         3,
		RetryDelay:         1 * time.Second,
		UseSecureResolvers: true,
		CustomResolvers:    []string{"8.8.8.8:53", "1.1.1.1:53"}, // Google and Cloudflare DNS
		RecordPrefix:       "mcp-verify",
	}
}

// VerifyDNSRecord verifies domain ownership by checking for a specific TXT record
// containing the expected verification token.
//
// This function implements the DNS TXT record verification method described in
// the Server Name Verification system. It looks for a TXT record with the format:
// "<prefix>=<token>" where prefix defaults to "mcp-verify"
//
// Security considerations:
// - Uses secure DNS resolvers to prevent spoofing attacks
// - Implements retry logic with exponential backoff for transient failures
// - Validates token format before verification
// - Logs all verification attempts for audit purposes
//
// Parameters:
// - domain: The domain name to verify (e.g., "example.com")
// - expectedToken: The 128-bit token that should be present in the DNS record
//
// Returns:
// - DNSVerificationResult with verification status and details
// - An error if the verification process fails critically
//
// The default configuration uses "mcp-verify" as the record prefix. To use a custom
// prefix, use VerifyDNSRecordWithConfig with a configured DNSVerificationConfig.
//
// Example usage:
//
//	result, err := VerifyDNSRecord("example.com", "TBeVXe_X4npM6p8vpzStnA")
//	if err != nil {
//	    log.Printf("DNS verification error: %v", err)
//	    return err
//	}
//	if result.Success {
//	    log.Printf("Domain %s verified successfully", result.Domain)
//	} else {
//	    log.Printf("Domain %s verification failed: %s", result.Domain, result.Message)
//	}
func VerifyDNSRecord(domain, expectedToken string) (*DNSVerificationResult, error) {
	return VerifyDNSRecordWithConfig(domain, expectedToken, DefaultDNSConfig())
}

// VerifyDNSRecordWithConfig performs DNS verification with custom configuration
func VerifyDNSRecordWithConfig(domain, expectedToken string, config *DNSVerificationConfig) (*DNSVerificationResult, error) {
	startTime := time.Now()

	// Input validation
	if domain == "" {
		return nil, &DNSVerificationError{
			Domain:  domain,
			Token:   expectedToken,
			Message: "domain cannot be empty",
		}
	}

	if expectedToken == "" {
		return nil, &DNSVerificationError{
			Domain:  domain,
			Token:   expectedToken,
			Message: "token cannot be empty",
		}
	}

	// Validate token format
	if !ValidateTokenFormat(expectedToken) {
		return nil, &DNSVerificationError{
			Domain:  domain,
			Token:   expectedToken,
			Message: "invalid token format",
		}
	}

	// Normalize domain (remove trailing dots, convert to lowercase)
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))

	log.Printf("Starting DNS verification for domain: %s with token: %s", domain, expectedToken)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()

	// Perform verification with retries
	result, err := performDNSVerificationWithRetries(ctx, domain, expectedToken, config)

	// Calculate duration
	duration := time.Since(startTime)
	if result != nil {
		result.Duration = duration.String()
	}

	log.Printf("DNS verification completed for domain %s in %v: success=%t",
		domain, duration, result != nil && result.Success)

	return result, err
}

// performDNSVerificationWithRetries implements the retry logic for DNS verification
func performDNSVerificationWithRetries(
	ctx context.Context,
	domain, expectedToken string,
	config *DNSVerificationConfig,
) (*DNSVerificationResult, error) {
	var lastErr error
	var lastResult *DNSVerificationResult

	retryDelay := config.RetryDelay

	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("DNS verification retry %d/%d for domain %s after %v delay",
				attempt+1, config.MaxRetries, domain, retryDelay)

			// Wait before retry with context cancellation support
			timer := time.NewTimer(retryDelay)
			select {
			case <-timer.C:
				// Timer fired normally, continue with retry
			case <-ctx.Done():
				// Context canceled, stop timer to prevent leak
				timer.Stop()
				return nil, &DNSVerificationError{
					Domain:  domain,
					Token:   expectedToken,
					Message: "verification canceled",
					Cause:   ctx.Err(),
				}
			}

			// Exponential backoff
			retryDelay *= 2
		}

		result, err := performDNSVerification(ctx, domain, expectedToken, config)
		if err == nil {
			return result, nil
		}

		lastErr = err
		lastResult = result

		// Check if error is retryable
		if !IsRetryableDNSError(err) {
			log.Printf("Non-retryable DNS error for domain %s: %v", domain, err)
			break
		}

		log.Printf("Retryable DNS error for domain %s (attempt %d/%d): %v",
			domain, attempt+1, config.MaxRetries, err)
	}

	// All retries exhausted
	return lastResult, lastErr
}

// performDNSVerification performs a single DNS verification attempt
func performDNSVerification(ctx context.Context, domain, expectedToken string, config *DNSVerificationConfig) (*DNSVerificationResult, error) {
	// Get resolver (either injected or create default)
	var resolver DNSResolver
	if config.Resolver != nil {
		resolver = config.Resolver
	} else {
		resolver = NewDefaultDNSResolver(config)
	}

	// Query TXT records
	txtRecords, err := resolver.LookupTXT(ctx, domain)
	if err != nil {
		dnsErr := &DNSVerificationError{
			Domain:  domain,
			Token:   expectedToken,
			Message: "failed to query DNS TXT records",
			Cause:   err,
		}

		result := &DNSVerificationResult{
			Success: false,
			Domain:  domain,
			Token:   expectedToken,
			Message: dnsErr.Message,
		}

		return result, dnsErr
	}

	log.Printf("Found %d TXT records for domain %s", len(txtRecords), domain)

	// Check for verification token
	expectedRecord := fmt.Sprintf("%s=%s", config.RecordPrefix, expectedToken)

	for _, record := range txtRecords {
		log.Printf("Checking TXT record: %s", record)
		if record == expectedRecord {
			result := &DNSVerificationResult{
				Success:    true,
				Domain:     domain,
				Token:      expectedToken,
				Message:    "domain verification successful",
				TXTRecords: txtRecords,
			}

			log.Printf("DNS verification successful for domain %s", domain)
			return result, nil
		}
	}

	// Token not found
	result := &DNSVerificationResult{
		Success:    false,
		Domain:     domain,
		Token:      expectedToken,
		Message:    fmt.Sprintf("verification token not found in DNS TXT records (expected: %s)", expectedRecord),
		TXTRecords: txtRecords,
	}

	log.Printf("DNS verification failed for domain %s: token not found", domain)
	return result, nil
}

// IsRetryableDNSError determines if a DNS error should be retried
func IsRetryableDNSError(err error) bool {
	if err == nil {
		return false
	}

	// Use iterative approach to prevent stack overflow with deeply nested errors
	const maxIterations = 100
	iterationCount := 0
	for err != nil {
		// Prevent infinite loop in case of circular error chain
		if iterationCount >= maxIterations {
			log.Printf("Exceeded maximum error unwrapping iterations (%d); possible circular error chain", maxIterations)
			return false
		}
		iterationCount++
		// Check for temporary network errors
		var netErr *net.OpError
		if errors.As(err, &netErr) {
			return netErr.Temporary()
		}

		// Check for context timeout (might be temporary)
		if errors.Is(err, context.DeadlineExceeded) {
			return true
		}

		// Check for DNS-specific temporary failures
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) {
			return dnsErr.Temporary()
		}

		// Move to next error in chain
		err = errors.Unwrap(err)
	}

	return false
}
