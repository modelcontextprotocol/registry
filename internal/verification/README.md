# Domain Verification Package

This package provides cryptographically secure token generation for domain ownership verification in the MCP Registry. It implements the requirements specified in the Server Name Verification system.

## Overview

The verification package generates 128-bit cryptographically secure random tokens used for proving domain ownership through two verification methods:

1. **DNS TXT Record Verification**: Add `mcp-verify=<token>` to your domain's DNS
2. **HTTP-01 Web Challenge**: Serve the token at `https://domain/.well-known/mcp-verify`

## Functions

### GenerateVerificationToken()

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

## Security Considerations

### Cryptographic Security
- Uses `crypto/rand` which provides cryptographically secure random numbers
- 128 bits provides 2^128 possible values (negligible collision probability)
- Suitable for cryptographic applications requiring unpredictable tokens

### Token Properties
- **Single-use**: Tokens should be used only once for verification
- **Time-limited**: Implement appropriate expiration policies
- **Secure transmission**: Always use HTTPS when transmitting tokens
- **Secure storage**: Store tokens securely on both client and server side

### Platform Compatibility
- Works on all platforms supported by Go's `crypto/rand`
- Automatically uses platform-appropriate entropy sources:
  - Linux/Unix: `/dev/urandom`
  - Windows: CryptGenRandom
  - macOS: SecRandomCopyBytes

## Usage Examples

### DNS Verification Setup
```go
package main

import (
    "fmt"
    "github.com/modelcontextprotocol/registry/internal/verification"
)

func setupDNSVerification(domain string) error {
    token, err := verification.GenerateVerificationToken()
    if err != nil {
        return err
    }
    
    fmt.Printf("Add this TXT record to %s:\n", domain)
    fmt.Printf("Record: mcp-verify=%s\n", token)
    fmt.Printf("Value: %s\n", token)
    
    return nil
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

Benchmark results on Apple M4 Max:

```
BenchmarkGenerateVerificationToken-16    5726528    196.1 ns/op
```

Note: These benchmark results are provided as examples and were obtained on an Apple M4 Max system. Performance may vary significantly on different hardware configurations.
Token generation is fast enough for real-time use in web applications.

## Testing

The package includes comprehensive tests covering:

- Token generation and uniqueness
- Entropy validation (exactly 128 bits)
- URL and DNS safety
- Error handling
- Performance benchmarks

Run tests with:
```bash
go test ./internal/verification -v
go test ./internal/verification -bench=.
```

## Integration

This package is designed to integrate with the MCP Registry's domain verification system as specified in `server-name-verification.md`. It provides the foundational token generation capability required for both DNS and HTTP verification methods.
