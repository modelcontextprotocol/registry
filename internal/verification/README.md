# Domain Verification Package

This package provides cryptographically secure token generation and DNS verification for domain ownership verification in the MCP Registry. It implements the requirements specified in the Server Name Verification system.

## Overview

The verification package generates 128-bit cryptographically secure random tokens used for proving domain ownership through two verification methods:

1. **DNS TXT Record Verification**: Add `mcp-verify=<token>` to your domain's DNS
2. **HTTP-01 Web Challenge**: Serve the token at `https://domain/.well-known/mcp-challenge/<token>`

## Functions

### Token Generation

#### GenerateVerificationToken()

Generates a cryptographically secure 128-bit random token encoded in base64url format.

```go
token, err := verification.GenerateVerificationToken()
if err != nil {
    return fmt.Errorf("failed to generate token: %w", err)
}
// token: "TBeVXe_X4npM6p8vpzStnA" (22 characters)
```

**Features:**
- Uses `crypto/rand` for cryptographically secure randomness
- 128 bits (16 bytes) of entropy
- Base64url encoding (URL-safe and DNS-safe)
- No padding characters
- 22-character output length

#### GenerateTokenWithInfo()

Generates a token with additional metadata about how to use it.

```go
tokenInfo, err := verification.GenerateTokenWithInfo()
if err != nil {
    return fmt.Errorf("failed to generate token info: %w", err)
}

fmt.Printf("Token: %s\n", tokenInfo.Token)
fmt.Printf("DNS Record: %s\n", tokenInfo.DNSRecord)
fmt.Printf("HTTP Path: %s\n", tokenInfo.HTTPPath)
```

**Output:**
```
Token: TBeVXe_X4npM6p8vpzStnA
DNS Record: mcp-verify=TBeVXe_X4npM6p8vpzStnA
HTTP Path: /.well-known/mcp-challenge/TBeVXe_X4npM6p8vpzStnA
```

### DNS Verification

#### VerifyDNSRecord(domain, expectedToken string)

Verifies domain ownership by checking for a specific TXT record containing the expected verification token.

```go
result, err := verification.VerifyDNSRecord("example.com", "TBeVXe_X4npM6p8vpzStnA")
if err != nil {
    log.Printf("DNS verification error: %v", err)
    return err
}

if result.Success {
    log.Printf("Domain %s verified successfully", result.Domain)
} else {
    log.Printf("Domain %s verification failed: %s", result.Domain, result.Message)
}
```

**Features:**
- Queries DNS TXT records for verification tokens
- Uses secure DNS resolvers (8.8.8.8, 1.1.1.1) by default
- Implements retry logic with exponential backoff
- Supports custom DNS resolver configuration
- Validates token format before verification
- Comprehensive error handling and logging

#### VerifyDNSRecordWithConfig(domain, expectedToken string, config *DNSVerificationConfig)

Performs DNS verification with custom configuration.

```go
config := &verification.DNSVerificationConfig{
    Timeout:            5 * time.Second,
    MaxRetries:         2,
    RetryDelay:         1 * time.Second,
    UseSecureResolvers: true,
    CustomResolvers:    []string{"8.8.8.8:53", "1.1.1.1:53"},
    RecordPrefix:       "my-custom-prefix", // Custom prefix instead of "mcp-verify"
}

result, err := verification.VerifyDNSRecordWithConfig("example.com", token, config)
```

#### DefaultDNSConfig()

Returns the default configuration for DNS verification.

```go
config := verification.DefaultDNSConfig()
// Returns: &DNSVerificationConfig{
//     Timeout:            10 * time.Second,
//     MaxRetries:         3,
//     RetryDelay:         1 * time.Second,
//     UseSecureResolvers: true,
//     CustomResolvers:    []string{"8.8.8.8:53", "1.1.1.1:53"},
//     RecordPrefix:       "mcp-verify",
// }
```

## Types and Structures

### DNSVerificationResult

```go
type DNSVerificationResult struct {
    Success    bool     `json:"success"`
    Domain     string   `json:"domain"`
    Token      string   `json:"token"`
    Message    string   `json:"message"`
    TXTRecords []string `json:"txt_records,omitempty"`
    Duration   string   `json:"duration"`
}
```

### DNSVerificationConfig

```go
type DNSVerificationConfig struct {
    Timeout            time.Duration // Default: 10 seconds
    MaxRetries         int           // Default: 3
    RetryDelay         time.Duration // Default: 1 second
    UseSecureResolvers bool          // Default: true
    CustomResolvers    []string      // Default: ["8.8.8.8:53", "1.1.1.1:53"]
    RecordPrefix       string        // Default: "mcp-verify"
}
```

### DNSVerificationError

```go
type DNSVerificationError struct {
    Domain  string
    Token   string
    Message string
    Cause   error
}
```

## Security Considerations

### Cryptographic Security
- Uses `crypto/rand` which provides cryptographically secure random numbers
- 128 bits provides 2^128 possible values (negligible collision probability)
- Suitable for cryptographic applications requiring unpredictable tokens

### DNS Security
- Uses secure DNS resolvers (8.8.8.8, 1.1.1.1) by default to prevent DNS spoofing
- Implements retry logic for transient DNS failures
- Validates domain ownership through industry-standard DNS TXT records
- Supports DNSSEC-aware resolvers

### Token Properties
- **Single-use**: Tokens should be used only once for verification
- **Time-limited**: Implement appropriate expiration policies
- **Secure transmission**: Always use HTTPS when transmitting tokens
- **Secure storage**: Store tokens securely on both client and server side

## Usage Examples

### Complete DNS Verification Workflow

```go
package main

import (
    "errors"
    "fmt"
    "log"
    "time"
    "github.com/modelcontextprotocol/registry/internal/verification"
)

func verifyDomainOwnership(domain string) error {
    // 1. Generate verification token
    tokenInfo, err := verification.GenerateTokenWithInfo()
    if err != nil {
        return fmt.Errorf("failed to generate token: %w", err)
    }
    
    // 2. Instruct user to add DNS record
    fmt.Printf("Add this TXT record to %s:\n", domain)
    fmt.Printf("Name: %s\n", domain)
    fmt.Printf("Type: TXT\n")
    fmt.Printf("Value: %s\n", tokenInfo.DNSRecord)
    fmt.Println("Press Enter after adding the DNS record...")
    fmt.Scanln()
    
    // 3. Verify the DNS record
    result, err := verification.VerifyDNSRecord(domain, tokenInfo.Token)
    if err != nil {
        return fmt.Errorf("DNS verification failed: %w", err)
    }
    
    if result.Success {
        log.Printf("✅ Domain %s verified successfully!", domain)
        log.Printf("Verification completed in %s", result.Duration)
        return nil
    } else {
        return fmt.Errorf("❌ Domain verification failed: %s", result.Message)
    }
}
```

### Custom DNS Configuration

```go
func verifyWithCustomConfig(domain, token string) error {
    config := &verification.DNSVerificationConfig{
        Timeout:            5 * time.Second,
        MaxRetries:         2,
        RetryDelay:         500 * time.Millisecond,
        UseSecureResolvers: true,
        CustomResolvers:    []string{"1.1.1.1:53", "8.8.8.8:53"},
    }
    
    result, err := verification.VerifyDNSRecordWithConfig(domain, token, config)
    if err != nil {
        return err
    }
    
    log.Printf("Verification result: %+v", result)
    return nil
}
```

### Custom Record Prefix

You can configure a custom DNS record prefix instead of the default "mcp-verify":

```go
func verifyWithCustomPrefix(domain, token string) error {
    config := verification.DefaultDNSConfig()
    config.RecordPrefix = "my-service-verify"
    
    // This will look for: my-service-verify=<token>
    result, err := verification.VerifyDNSRecordWithConfig(domain, token, config)
    if err != nil {
        return err
    }
    
    if result.Success {
        log.Printf("✅ Domain verified with custom prefix!")
    } else {
        log.Printf("❌ Verification failed: %s", result.Message)
    }
    
    return nil
}
```

### Error Handling and Retry Logic

```go
func robustDNSVerification(domain, token string) error {
    maxAttempts := 3
    
    for attempt := 1; attempt <= maxAttempts; attempt++ {
        log.Printf("DNS verification attempt %d/%d for domain %s", attempt, maxAttempts, domain)
        
        result, err := verification.VerifyDNSRecord(domain, token)
        if err != nil {
            var dnsErr *verification.DNSVerificationError
            if errors.As(err, &dnsErr) {
                log.Printf("DNS error: %s", dnsErr.Message)
                if attempt < maxAttempts {
                    time.Sleep(time.Duration(attempt) * time.Second)
                    continue
                }
            }
            return err
        }
        
        if result.Success {
            log.Printf("✅ Domain verified on attempt %d", attempt)
            return nil
        }
        
        log.Printf("❌ Verification failed: %s", result.Message)
        if attempt < maxAttempts {
            time.Sleep(time.Duration(attempt) * time.Second)
        }
    }
    
    return fmt.Errorf("domain verification failed after %d attempts", maxAttempts)
}
```

### HTTP-01 Challenge Setup
```go
func setupHTTPChallenge(domain string) error {
    token, err := verification.GenerateVerificationToken()
    if err != nil {
        return err
    }
    
    fmt.Printf("Serve the token at: https://%s/.well-known/mcp-challenge/%s\n", domain, token)
    fmt.Printf("Content: %s\n", token)
    
    return nil
}
```

### Token String Comparison
```go
func validateUserToken(userToken, expectedToken string) bool {
    // For verification, simply compare the token strings
    // No format validation needed - just string comparison
    return userToken == expectedToken
}
```

## Constants

- `TokenLength`: 16 bytes (128 bits) - the entropy size of generated tokens

## Error Handling

### DNS Verification Errors

The DNS verification functions can return various types of errors:

- **Input validation errors**: Invalid domain or token format
- **Network errors**: DNS resolution failures, timeouts
- **Verification errors**: Token not found in DNS records

```go
result, err := verification.VerifyDNSRecord(domain, token)
if err != nil {
    var dnsErr *verification.DNSVerificationError
    if errors.As(err, &dnsErr) {
        log.Printf("DNS verification failed for domain %s: %s", dnsErr.Domain, dnsErr.Message)
        if dnsErr.Cause != nil {
            log.Printf("Underlying cause: %v", dnsErr.Cause)
        }
    } else {
        log.Printf("Unexpected error: %v", err)
    }
    return err
}
```

### Token Generation Errors
The function returns errors in the following case:

- `GenerateVerificationToken()`: When the system's entropy source is unavailable

Always check for errors and handle them appropriately:

```go
token, err := verification.GenerateVerificationToken()
if err != nil {
    log.Printf("Failed to generate verification token: %v", err)
    // Handle error appropriately (retry, fallback, etc.)
    return err
}
```

## Performance

The DNS verification system is designed for real-world performance:

- **Token generation**: Sub-microsecond performance
- **DNS queries**: Typically 10-100ms depending on network conditions
- **Retry logic**: Exponential backoff prevents overwhelming DNS servers
- **Concurrent verification**: Safe for use in goroutines

## Testing

The package includes comprehensive tests covering:

- Token generation and uniqueness
- Entropy validation (exactly 128 bits)
- Format validation
- URL and DNS safety
- DNS verification functionality
- Error handling scenarios
- Performance benchmarks

Run tests with:
```bash
go test ./internal/verification -v
go test ./internal/verification -bench=.
```

## Integration

This package is designed to integrate with the MCP Registry's domain verification system as specified in `server-name-verification.md`. It provides both token generation and DNS verification capabilities required for the dual-method verification approach.

### Integration Points

1. **Registry API**: Use for generating tokens when users claim domain namespaces
2. **Background verification**: Use for continuous verification of existing domains
3. **CLI tools**: Use for domain verification during package publishing
4. **Admin tools**: Use for debugging verification issues

````
