package model_test

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPackage_PostInstallInstructions_RoundTrip ensures the PostInstallInstructions
// field on Package serializes and deserializes losslessly.
func TestPackage_PostInstallInstructions_RoundTrip(t *testing.T) {
	pkg := model.Package{
		RegistryType: "mcpb",
		Identifier:   "https://github.com/aniongithub/mind-map/releases/download/v1.0.0/mind-map.mcpb",
		Transport:    model.Transport{Type: "stdio"},
		PostInstallInstructions: []model.PostInstallInstruction{
			{
				Description:   "Install as a system service for the web UI",
				Command:       "mind-map service install --addr 127.0.0.1:4242",
				Documentation: "https://github.com/aniongithub/mind-map#service-management",
				Optional:      true,
			},
		},
	}

	data, err := json.Marshal(pkg)
	require.NoError(t, err)

	var decoded model.Package
	require.NoError(t, json.Unmarshal(data, &decoded))

	require.Len(t, decoded.PostInstallInstructions, 1)
	instruction := decoded.PostInstallInstructions[0]
	assert.Equal(t, "Install as a system service for the web UI", instruction.Description)
	assert.Equal(t, "mind-map service install --addr 127.0.0.1:4242", instruction.Command)
	assert.Equal(t, "https://github.com/aniongithub/mind-map#service-management", instruction.Documentation)
	assert.True(t, instruction.Optional)
}

// TestPackage_PostInstallInstructions_OmittedWhenEmpty ensures the field is omitted
// from JSON output when unset, keeping it optional.
func TestPackage_PostInstallInstructions_OmittedWhenEmpty(t *testing.T) {
	pkg := model.Package{
		RegistryType: "npm",
		Identifier:   "@example/server",
		Version:      "1.0.0",
		Transport:    model.Transport{Type: "stdio"},
	}

	data, err := json.Marshal(pkg)
	require.NoError(t, err)

	assert.NotContains(t, string(data), "postInstallInstructions")
}
