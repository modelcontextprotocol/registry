package config

import (
	"fmt"

	env "github.com/caarlos0/env/v11"
)

// What the malware check does, on publish and edit, with a package that
// OSV.dev lists as malware.
const (
	// MalwareCheckOff does not ask OSV at all.
	MalwareCheckOff = "off"
	// MalwareCheckReport logs the match and lets the request through.
	MalwareCheckReport = "report"
	// MalwareCheckEnforce logs the match and refuses the request.
	MalwareCheckEnforce = "enforce"
)

// ValidateMalwareCheckMode rejects a mode that is none of the three, so that
// a misspelt one cannot quietly leave the check off.
func (c *Config) ValidateMalwareCheckMode() error {
	switch c.MalwareCheckMode {
	case MalwareCheckOff, MalwareCheckReport, MalwareCheckEnforce:
		return nil
	}
	return fmt.Errorf("MCP_REGISTRY_MALWARE_CHECK_MODE must be %q, %q or %q, not %q",
		MalwareCheckOff, MalwareCheckReport, MalwareCheckEnforce, c.MalwareCheckMode)
}

// Config holds the application configuration
// See .env.example for more documentation
type Config struct {
	ServerAddress            string `env:"SERVER_ADDRESS" envDefault:":8080"`
	DatabaseURL              string `env:"DATABASE_URL" envDefault:"postgres://localhost:5432/mcp-registry?sslmode=disable"`
	DatabaseMaxConnections   int32  `env:"DATABASE_MAX_CONNECTIONS" envDefault:"60"`
	DatabaseMinConnections   int32  `env:"DATABASE_MIN_CONNECTIONS" envDefault:"10"`
	PreparedListingJSON      bool   `env:"PREPARED_LISTING_JSON" envDefault:"false"`
	SeedFrom                 string `env:"SEED_FROM" envDefault:""`
	Version                  string `env:"VERSION" envDefault:"dev"`
	GithubClientID           string `env:"GITHUB_CLIENT_ID" envDefault:""`
	GithubClientSecret       string `env:"GITHUB_CLIENT_SECRET" envDefault:""`
	JWTPrivateKey            string `env:"JWT_PRIVATE_KEY" envDefault:""`
	EnableAnonymousAuth      bool   `env:"ENABLE_ANONYMOUS_AUTH" envDefault:"false"`
	EnableRegistryValidation bool   `env:"ENABLE_REGISTRY_VALIDATION" envDefault:"true"`
	AllowPrivateRemoteURLs   bool   `env:"ALLOW_PRIVATE_REMOTE_URLS" envDefault:"false"`

	// Malware checks against OSV.dev. MalwareCheckMode applies on publish and
	// edit, and only where EnableRegistryValidation is on.
	MalwareCheckMode        string `env:"MALWARE_CHECK_MODE" envDefault:"report"`
	OSVAPIURL               string `env:"OSV_API_URL" envDefault:"https://api.osv.dev"`
	MalwareSweepRegistryURL string `env:"MALWARE_SWEEP_REGISTRY_URL" envDefault:"https://registry.modelcontextprotocol.io"`
	// MalwareSweepMissingPackages has the sweep also ask each package's own
	// registry whether the package still exists.
	MalwareSweepMissingPackages bool `env:"MALWARE_SWEEP_MISSING_PACKAGES" envDefault:"true"`

	GitHubOIDCAudience string `env:"GITHUB_OIDC_AUDIENCE" envDefault:""`

	// OIDC Configuration
	OIDCEnabled      bool   `env:"OIDC_ENABLED" envDefault:"false"`
	OIDCIssuer       string `env:"OIDC_ISSUER" envDefault:""`
	OIDCClientID     string `env:"OIDC_CLIENT_ID" envDefault:""`
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
