package config

import (
	env "github.com/caarlos0/env/v11"
)

type DatabaseType string

const (
	DatabaseTypeMongoDB DatabaseType = "mongodb"
	DatabaseTypeMemory  DatabaseType = "memory"
)

// Config holds the application configuration
// See .env.example for more documentation
type Config struct {
	ServerAddress       string       `env:"SERVER_ADDRESS" envDefault:":8080"`
	DatabaseType        DatabaseType `env:"DATABASE_TYPE" envDefault:"mongodb"`
	DatabaseURL         string       `env:"DATABASE_URL" envDefault:"mongodb://localhost:27017"`
	DatabaseName        string       `env:"DATABASE_NAME" envDefault:"mcp-registry"`
	CollectionName      string       `env:"COLLECTION_NAME" envDefault:"servers_v2"`
	LogLevel            string       `env:"LOG_LEVEL" envDefault:"info"`
	SeedFrom            string       `env:"SEED_FROM" envDefault:""`
	Version             string       `env:"VERSION" envDefault:"dev"`
	GithubClientID      string       `env:"GITHUB_CLIENT_ID" envDefault:""`
	GithubClientSecret  string       `env:"GITHUB_CLIENT_SECRET" envDefault:""`
	JWTPrivateKey       string       `env:"JWT_PRIVATE_KEY" envDefault:""`
	EnableAnonymousAuth bool         `env:"ENABLE_ANONYMOUS_AUTH" envDefault:"false"`
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
