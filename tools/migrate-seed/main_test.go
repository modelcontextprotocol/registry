package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/registry/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)


// createTestLegacySeed creates a small test file with legacy format data
func createTestLegacySeed(t *testing.T) string {
	t.Helper()
	testData := []OldServerFormat{
		{
			ID:          "4e9cf4cf-71f6-4aca-bae8-2d10a29ca2e0",
			Name:        "io.github.21st-dev/magic-mcp",
			Description: "It's like v0 but in your Cursor/WindSurf/Cline. 21st dev Magic MCP server for working with your frontend like Magic",
			Repository: model.Repository{
				URL:    "https://github.com/21st-dev/magic-mcp",
				Source: "github",
				ID:     "935450522",
			},
			VersionDetail: OldVersionDetail{
				Version:     "0.0.1-seed",
				ReleaseDate: "2025-05-16T18:56:49Z",
				IsLatest:    true,
			},
			Packages: []model.Package{
				{
					RegistryName: "npm",
					Name:         "@21st-dev/magic",
					Version:      "0.0.46",
					EnvironmentVariables: []model.KeyValueInput{
						{
							Name: "API_KEY",
							InputWithVariables: model.InputWithVariables{
								Input: model.Input{
									Description: "${input:apiKey}",
								},
							},
						},
					},
				},
			},
		},
		{
			ID:          "d3669201-252f-403c-944b-c3ec0845782b",
			Name:        "io.github.adfin-engineering/mcp-server-adfin",
			Description: "A Model Context Protocol Server for connecting with Adfin APIs",
			Repository: model.Repository{
				URL:    "https://github.com/Adfin-Engineering/mcp-server-adfin",
				Source: "github",
				ID:     "951338147",
			},
			VersionDetail: OldVersionDetail{
				Version:     "0.0.1-seed",
				ReleaseDate: "2025-05-16T18:56:52Z",
				IsLatest:    true,
			},
			Packages: []model.Package{
				{
					RegistryName: "pypi",
					Name:         "adfinmcp",
					Version:      "0.1.0",
				},
			},
		},
	}

	tempFile, err := os.CreateTemp("", "test-legacy-seed-*.json")
	require.NoError(t, err)
	defer tempFile.Close()

	jsonData, err := json.MarshalIndent(testData, "", "  ")
	require.NoError(t, err)

	_, err = tempFile.Write(jsonData)
	require.NoError(t, err)

	return tempFile.Name()
}

func TestMigrationCLI(t *testing.T) {
	// Create test legacy seed file
	legacyFile := createTestLegacySeed(t)
	defer os.Remove(legacyFile)

	// Create output file path
	outputFile := filepath.Join(t.TempDir(), "migrated-output.json")

	// Run migration (simulate command line execution)
	err := migrateSeed(legacyFile, outputFile)
	require.NoError(t, err, "Migration should succeed")

	// Read the output file
	outputData, err := os.ReadFile(outputFile)
	require.NoError(t, err, "Should be able to read output file")

	// Parse as ServerResponse array (new format)
	var migratedServers []model.ServerResponse
	err = json.Unmarshal(outputData, &migratedServers)
	require.NoError(t, err, "Output should be valid JSON in ServerResponse format")

	// Verify we have the expected number of servers
	assert.Len(t, migratedServers, 2, "Should have migrated 2 servers")

	// Verify first server structure
	server1 := migratedServers[0]
	assert.Equal(t, "io.github.21st-dev/magic-mcp", server1.Server.Name)
	assert.Equal(t, "It's like v0 but in your Cursor/WindSurf/Cline. 21st dev Magic MCP server for working with your frontend like Magic", server1.Server.Description)
	assert.Equal(t, "https://github.com/21st-dev/magic-mcp", server1.Server.Repository.URL)
	assert.Equal(t, "0.0.1-seed", server1.Server.VersionDetail.Version)
	assert.Len(t, server1.Server.Packages, 1)
	assert.Equal(t, "npm", server1.Server.Packages[0].RegistryName)
	assert.Equal(t, "@21st-dev/magic", server1.Server.Packages[0].Name)

	// Verify registry metadata extension
	assert.NotNil(t, server1.XIOModelContextProtocolRegistry, "Should have registry metadata extension")
	registryMeta1, ok := server1.XIOModelContextProtocolRegistry.(map[string]interface{})
	require.True(t, ok, "Registry metadata should be a map")
	assert.Equal(t, "4e9cf4cf-71f6-4aca-bae8-2d10a29ca2e0", registryMeta1["id"])
	assert.Equal(t, true, registryMeta1["is_latest"])
	assert.Equal(t, "2025-05-16T18:56:49Z", registryMeta1["release_date"])

	// Verify second server structure
	server2 := migratedServers[1]
	assert.Equal(t, "io.github.adfin-engineering/mcp-server-adfin", server2.Server.Name)
	assert.Equal(t, "A Model Context Protocol Server for connecting with Adfin APIs", server2.Server.Description)
	assert.Equal(t, "pypi", server2.Server.Packages[0].RegistryName)
	assert.Equal(t, "adfinmcp", server2.Server.Packages[0].Name)

	// Verify registry metadata for second server
	registryMeta2, ok := server2.XIOModelContextProtocolRegistry.(map[string]interface{})
	require.True(t, ok, "Registry metadata should be a map")
	assert.Equal(t, "d3669201-252f-403c-944b-c3ec0845782b", registryMeta2["id"])
	assert.Equal(t, true, registryMeta2["is_latest"])
	assert.Equal(t, "2025-05-16T18:56:52Z", registryMeta2["release_date"])

	// Verify no x-publisher extensions (should be empty/nil for seed data)
	assert.Nil(t, server1.XPublisher, "Seed data should not have x-publisher extensions")
	assert.Nil(t, server2.XPublisher, "Seed data should not have x-publisher extensions")
}

// migrateSeed is a testable version of the main migration logic
func migrateSeed(inputFile, outputFile string) error {
	// Read source data
	data, err := os.ReadFile(inputFile)
	if err != nil {
		return err
	}

	// Parse old format
	var oldServers []OldServerFormat
	if err := json.Unmarshal(data, &oldServers); err != nil {
		return err
	}

	// Convert to new format
	var newServers []model.ServerResponse
	for _, old := range oldServers {
		converted := convertServer(old)
		newServers = append(newServers, converted)
	}

	// Write migrated data
	migratedData, err := json.MarshalIndent(newServers, "", "  ")
	if err != nil {
		return err
	}

	// Ensure output directory exists
	if err := os.MkdirAll(filepath.Dir(outputFile), 0755); err != nil {
		return err
	}

	return os.WriteFile(outputFile, migratedData, 0600)
}

func TestMigrationCLI_CommandLine(t *testing.T) {
	// Create test legacy seed file
	legacyFile := createTestLegacySeed(t)
	defer os.Remove(legacyFile)

	// Create output file path
	outputFile := filepath.Join(t.TempDir(), "cli-migrated-output.json")

	// Simulate command line arguments
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{"migrate-seed", legacyFile, outputFile}

	// Capture any panics or exits from main()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("main() panicked: %v", r)
		}
	}()

	// Test the main function (Note: this would normally call os.Exit, but in tests it should return)
	main()

	// Verify the output file was created and has correct content
	require.FileExists(t, outputFile, "Output file should be created")

	outputData, err := os.ReadFile(outputFile)
	require.NoError(t, err, "Should be able to read output file")

	var migratedServers []model.ServerResponse
	err = json.Unmarshal(outputData, &migratedServers)
	require.NoError(t, err, "Output should be valid JSON in ServerResponse format")

	// Basic verification
	assert.Len(t, migratedServers, 2, "Should have migrated 2 servers")
	assert.Equal(t, "io.github.21st-dev/magic-mcp", migratedServers[0].Server.Name)
	assert.Equal(t, "io.github.adfin-engineering/mcp-server-adfin", migratedServers[1].Server.Name)
}

func TestMigrationCLI_ErrorHandling(t *testing.T) {
	t.Run("Invalid JSON input", func(t *testing.T) {
		// Create invalid JSON file
		invalidFile, err := os.CreateTemp("", "invalid-*.json")
		require.NoError(t, err)
		defer os.Remove(invalidFile.Name())
		
		_, err = invalidFile.WriteString(`{"invalid": json}`)
		require.NoError(t, err)
		invalidFile.Close()

		outputFile := filepath.Join(t.TempDir(), "output.json")
		err = migrateSeed(invalidFile.Name(), outputFile)
		assert.Error(t, err, "Should error on invalid JSON")
	})

	t.Run("Missing input file", func(t *testing.T) {
		outputFile := filepath.Join(t.TempDir(), "output.json")
		err := migrateSeed("/nonexistent/file.json", outputFile)
		assert.Error(t, err, "Should error on missing input file")
	})

	t.Run("Empty input file", func(t *testing.T) {
		// Create empty JSON array
		emptyFile, err := os.CreateTemp("", "empty-*.json")
		require.NoError(t, err)
		defer os.Remove(emptyFile.Name())
		
		_, err = emptyFile.WriteString("[]")
		require.NoError(t, err)
		emptyFile.Close()

		outputFile := filepath.Join(t.TempDir(), "output.json")
		err = migrateSeed(emptyFile.Name(), outputFile)
		assert.NoError(t, err, "Empty array should be valid")

		// Verify empty output
		outputData, err := os.ReadFile(outputFile)
		require.NoError(t, err)
		
		var result []model.ServerResponse
		err = json.Unmarshal(outputData, &result)
		require.NoError(t, err, "Should be valid JSON")
		assert.Len(t, result, 0, "Should have no servers")
	})
}

func TestMigrationCLI_GoldenFile(t *testing.T) {
	// Test migration with predefined input and expected output files
	inputFile := "testdata/legacy-input.json"
	expectedFile := "testdata/expected-output.json"
	
	// Ensure test files exist
	require.FileExists(t, inputFile, "Input test file should exist")
	require.FileExists(t, expectedFile, "Expected output test file should exist")
	
	// Run migration on test input
	outputFile := filepath.Join(t.TempDir(), "golden-output.json")
	err := migrateSeed(inputFile, outputFile)
	require.NoError(t, err, "Migration should succeed")
	
	// Read expected and actual output
	expectedData, err := os.ReadFile(expectedFile)
	require.NoError(t, err, "Should be able to read expected output")
	
	actualData, err := os.ReadFile(outputFile)
	require.NoError(t, err, "Should be able to read actual output")
	
	// Parse both as JSON to normalize formatting and compare semantically
	var expectedJSON, actualJSON []model.ServerResponse
	
	err = json.Unmarshal(expectedData, &expectedJSON)
	require.NoError(t, err, "Expected output should be valid JSON")
	
	err = json.Unmarshal(actualData, &actualJSON)
	require.NoError(t, err, "Actual output should be valid JSON")
	
	// Compare the parsed structures
	assert.Equal(t, expectedJSON, actualJSON, "Migration output should match expected golden file")
	
	// Also compare server count
	assert.Len(t, actualJSON, len(expectedJSON), "Should have same number of servers")
	
	// Verify specific server names to ensure ordering is preserved
	if len(expectedJSON) >= 2 && len(actualJSON) >= 2 {
		assert.Equal(t, expectedJSON[0].Server.Name, actualJSON[0].Server.Name, "First server name should match")
		assert.Equal(t, expectedJSON[1].Server.Name, actualJSON[1].Server.Name, "Second server name should match")
	}
}

func TestGoldenFiles_Validation(t *testing.T) {
	// Validate that the golden files themselves are valid
	t.Run("Input file is valid legacy format", func(t *testing.T) {
		inputData, err := os.ReadFile("testdata/legacy-input.json")
		require.NoError(t, err, "Should be able to read input file")
		
		var legacyServers []OldServerFormat
		err = json.Unmarshal(inputData, &legacyServers)
		require.NoError(t, err, "Input file should be valid legacy format JSON")
		
		assert.Len(t, legacyServers, 2, "Input should contain exactly 2 servers")
		
		// Verify it contains expected server names
		names := []string{legacyServers[0].Name, legacyServers[1].Name}
		assert.Contains(t, names, "io.github.21st-dev/magic-mcp")
		assert.Contains(t, names, "io.github.adfin-engineering/mcp-server-adfin")
	})
	
	t.Run("Expected output is valid extension wrapper format", func(t *testing.T) {
		expectedData, err := os.ReadFile("testdata/expected-output.json")
		require.NoError(t, err, "Should be able to read expected output file")
		
		var expectedServers []model.ServerResponse
		err = json.Unmarshal(expectedData, &expectedServers)
		require.NoError(t, err, "Expected output should be valid ServerResponse format JSON")
		
		assert.Len(t, expectedServers, 2, "Expected output should contain exactly 2 servers")
		
		// Verify structure of first server
		server1 := expectedServers[0]
		assert.NotEmpty(t, server1.Server.Name, "Server should have a name")
		assert.NotEmpty(t, server1.Server.Description, "Server should have a description")
		assert.NotNil(t, server1.XIOModelContextProtocolRegistry, "Should have registry metadata")
		assert.Nil(t, server1.XPublisher, "Seed data should not have publisher extensions")
		
		// Verify registry metadata contains required fields
		registryMeta, ok := server1.XIOModelContextProtocolRegistry.(map[string]interface{})
		require.True(t, ok, "Registry metadata should be a map")
		assert.Contains(t, registryMeta, "id", "Registry metadata should have id")
		assert.Contains(t, registryMeta, "is_latest", "Registry metadata should have is_latest")
		assert.Contains(t, registryMeta, "release_date", "Registry metadata should have release_date")
	})
}