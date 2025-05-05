package config

import (
	"github.com/caarlos0/env/v11"
)

// Config holds the application configuration
type Config struct {
	ServerAddress      string `env:"SERVER_ADDRESS" envDefault:":8080"`
	DatabaseURL        string `env:"DATABASE_URL" envDefault:"mongodb://localhost:27017"`
	DatabaseName       string `env:"DATABASE_NAME" envDefault:"mcp-registry"`
	CollectionName     string `env:"COLLECTION_NAME" envDefault:"servers_v2"`
	LogLevel           string `env:"LOG_LEVEL" envDefault:"info"`
	SeedFilePath       string `env:"SEED_FILE_PATH" envDefault:"data/seed.json"`
	Version            string `env:"VERSION" envDefault:"dev"`
	GithubClientID     string `env:"GITHUB_CLIENT_ID" envDefault:""`
	GithubClientSecret string `env:"GITHUB_CLIENT_SECRET" envDefault:""`
	Import             bool   `env:"IMPORT" envDefault:"true"`
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
