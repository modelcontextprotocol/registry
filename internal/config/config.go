package config

import (
	"github.com/caarlos0/env/v11"
)

// Config holds the application configuration
type Config struct {
	ServerAddress  string `env:"MCP_REGISTRY_SERVER_ADDRESS" envDefault:":8080"`
	DatabaseURL    string `env:"MCP_REGISTRY_DATABASE_URL" envDefault:"mongodb://localhost:27017"`
	DatabaseName   string `env:"MCP_REGISTRY_DATABASE_NAME" envDefault:"mcp-registry"`
	CollectionName string `env:"MCP_REGISTRY_COLLECTION_NAME" envDefault:"servers_v2"`
	LogLevel       string `env:"MCP_REGISTRY_LOG_LEVEL" envDefault:"info"`
	Environment    string `env:"MCP_REGISTRY_ENVIRONMENT" envDefault:"production"`
	Version        string `env:"MCP_REGISTRY_VERSION" envDefault:"dev"`
}

// NewConfig creates a new configuration with default values
func NewConfig() *Config {
	var cfg Config
	err := env.Parse(&cfg)
	if err != nil {
		panic(err)
	}
	return &cfg
}
