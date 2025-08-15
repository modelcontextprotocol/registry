# Authentication System

The publisher tool now uses an interface-based authentication system that allows for multiple authentication mechanisms.

## Architecture

### Provider Interface

The `Provider` interface is defined in `auth/interface.go` and provides the following methods:

- `GetToken(ctx context.Context) (string, error)` - Retrieves or generates an authentication token
- `NeedsLogin() bool` - Checks if a new login flow is required
- `Login(ctx context.Context) error` - Performs the authentication flow
- `Name() string` - Returns the name of the authentication provider

### Available Authentication Providers

#### 1. GitHub OAuth Provider
- **Location**: `auth/github/oauth.go`
- **Usage**: Uses GitHub's device flow for authentication
- **Example**: `github.NewOAuthProvider(forceLogin, registryURL)`


## How to Add New Authentication Providers

1. Create a new package under `auth/` directory (e.g., `auth/custom/`)
2. Implement the `Provider` interface
3. Add any necessary configuration or initialization functions
4. Update the main application to use the new provider

### Example Implementation

```go
package custom

import (
    "context"
    "fmt"
)

type CustomProvider struct {
    // your custom fields
}

func NewCustomProvider(config string) *CustomProvider {
    return &CustomProvider{
        // initialize your provider
    }
}

func (cp *CustomProvider) GetToken(ctx context.Context) (string, error) {
    // implement token retrieval logic
    return "custom-token", nil
}

func (cp *CustomProvider) NeedsLogin() bool {
    // implement login check logic
    return false
}

func (cp *CustomProvider) Login(ctx context.Context) error {
    // implement authentication flow
    return nil
}

func (cp *CustomProvider) Name() string {
    return "custom-auth"
}
```

## Usage in Main Application

The main application automatically selects the appropriate authentication provider:

1. Uses `GitHub OAuth Provider` by default
2. Future providers can be added by extending the provider selection logic

```go
// Create the appropriate auth provider based on configuration
var authProvider auth.Provider
switch authMethod {
case "github":
    log.Println("Using GitHub OAuth for authentication")
    authProvider = github.NewOAuthProvider(forceLogin, registryURL)
default:
    log.Printf("Unsupported authentication method: %s\n", authMethod)
    return
}

// Check if login is needed and perform authentication
ctx := context.Background()
if authProvider.NeedsLogin() {
    err := authProvider.Login(ctx)
    if err != nil {
        log.Printf("Failed to authenticate with %s: %s\n", authProvider.Name(), err.Error())
        return
    }
}

// Get the token
token, err := authProvider.GetToken(ctx)
```

This design allows for easy extension and testing of different authentication mechanisms while maintaining a clean separation of concerns.
