package config

import (
	env "github.com/caarlos0/env/v11"
	"strings"
)

type DatabaseType string

const (
	DatabaseTypePostgreSQL DatabaseType = "postgresql"
	DatabaseTypeMemory     DatabaseType = "memory"
)

// Config holds the application configuration
// See .env.example for more documentation
type Config struct {
	ServerAddress            string       `env:"SERVER_ADDRESS" envDefault:":8080"`
	DatabaseType             DatabaseType `env:"DATABASE_TYPE" envDefault:"postgresql"`
	DatabaseURL              string       `env:"DATABASE_URL" envDefault:"postgres://localhost:5432/mcp-registry?sslmode=disable"`
	SeedFrom                 string       `env:"SEED_FROM" envDefault:""`
	Version                  string       `env:"VERSION" envDefault:"dev"`
	GithubClientID           string       `env:"GITHUB_CLIENT_ID" envDefault:""`
	GithubClientSecret       string       `env:"GITHUB_CLIENT_SECRET" envDefault:""`
	JWTPrivateKey            string       `env:"JWT_PRIVATE_KEY" envDefault:""`
	EnableAnonymousAuth      bool         `env:"ENABLE_ANONYMOUS_AUTH" envDefault:"false"`
	EnableRegistryValidation bool         `env:"ENABLE_REGISTRY_VALIDATION" envDefault:"true"`

	// OCI registry auth: comma separated list of host=token pairs used for validating private images
	// Example: "ghcr.io=ghp_xxx,docker.io=abcdef"
	OCIRegistryAuth string `env:"OCI_REGISTRY_AUTH" envDefault:""` // Added for parsing OCI registry auth tokens

	// OIDC Configuration
	OIDCEnabled      bool   `env:"OIDC_ENABLED" envDefault:"false"`
	OIDCIssuer       string `env:"OIDC_ISSUER" envDefault:""`
	OIDCClientID     string `env:"OIDC_CLIENT_ID" envDefault:""`
	OIDCClientSecret string `env:"OIDC_CLIENT_SECRET" envDefault:""`
	OIDCExtraClaims  string `env:"OIDC_EXTRA_CLAIMS" envDefault:""`
	OIDCEditPerms    string `env:"OIDC_EDIT_PERMISSIONS" envDefault:""`
	OIDCPublishPerms string `env:"OIDC_PUBLISH_PERMISSIONS" envDefault:""`
}

// NewConfig creates a new configuration with default values
func NewConfig() *Config {
	var cfg Config
	err := env.ParseWithOptions(&cfg, env.Options{
		Prefix: "MCP_REGISTRY_",
	})
	if err != nil {
		panic(err)
	}
	return &cfg
}

// ParseOCIRegistryAuth converts OCIRegistryAuth string into map[host]token
// Format: "host=token,otherhost=othertoken". Whitespace is trimmed. Invalid entries are ignored.
func (c *Config) ParseOCIRegistryAuth() map[string]string {
	authMap := make(map[string]string)
	if c == nil || c.OCIRegistryAuth == "" {
		return authMap
	}
	items := strings.Split(c.OCIRegistryAuth, ",")
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 {
			continue
		}
		host := strings.TrimSpace(parts[0])
		token := strings.TrimSpace(parts[1])
		if host != "" && token != "" {
			authMap[host] = token
		}
	}
	return authMap
}
