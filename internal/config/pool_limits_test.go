package config_test

import (
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestDatabasePoolEnvironment(t *testing.T) {
	t.Setenv("MCP_REGISTRY_DATABASE_MAX_CONNECTIONS", "30")
	t.Setenv("MCP_REGISTRY_DATABASE_MIN_CONNECTIONS", "5")
	cfg := config.NewConfig()
	require.EqualValues(t, 30, cfg.DatabaseMaxConnections)
	require.EqualValues(t, 5, cfg.DatabaseMinConnections)
	t.Setenv("MCP_REGISTRY_DATABASE_MAX_CONNECTIONS", "not-an-integer")
	require.Panics(t, func() { config.NewConfig() })
}
