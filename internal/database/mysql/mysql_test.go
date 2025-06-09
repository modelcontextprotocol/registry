package mysql

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/modelcontextprotocol/registry/internal/model"
)

var mysqlDB *MySQLDB
var mysqlContainer testcontainers.Container

func TestMain(m *testing.M) {
	ctx := context.Background()

	// Start MySQL container
	req := testcontainers.ContainerRequest{
		Image:        "mysql:8.0",
		ExposedPorts: []string{"3306/tcp"},
		Env: map[string]string{
			"MYSQL_ROOT_PASSWORD": "root",
			"MYSQL_DATABASE":      "mcp_registry",
			"MYSQL_USER":          "mcp_user",
			"MYSQL_PASSWORD":      "mcp_password",
		},
		WaitingFor: wait.ForAll(
			wait.ForLog("ready for connections"),
			wait.ForListeningPort("3306/tcp"),
		),
	}

	var err error
	mysqlContainer, err = testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:         true,
	})
	if err != nil {
		panic(fmt.Sprintf("Failed to start MySQL container: %v", err))
	}

	// Get container host and port
	host, err := mysqlContainer.Host(ctx)
	if err != nil {
		panic(fmt.Sprintf("Failed to get container host: %v", err))
	}
	port, err := mysqlContainer.MappedPort(ctx, "3306")
	if err != nil {
		panic(fmt.Sprintf("Failed to get container port: %v", err))
	}

	// Initialize MySQL connection
	dsn := fmt.Sprintf("mcp_user:mcp_password@tcp(%s:%s)/mcp_registry?parseTime=true", host, port.Port())
	mysqlDB, err = NewMySQLDB(ctx, dsn)
	if err != nil {
		panic(fmt.Sprintf("Failed to connect to MySQL: %v", err))
	}

	// Create table schema
	_, err = mysqlDB.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS servers (
			id VARCHAR(255) PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			description TEXT,
			repository_url VARCHAR(255) NOT NULL,
			repository_source VARCHAR(50) NOT NULL,
			repository_id VARCHAR(255) NOT NULL,
			version_detail_version VARCHAR(50) NOT NULL,
			version_detail_release_date VARCHAR(50) NOT NULL,
			version_detail_is_latest BOOLEAN NOT NULL DEFAULT TRUE,
			packages JSON,
			remotes JSON,
			INDEX idx_name_version (name, version_detail_version)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
	`)
	if err != nil {
		panic(fmt.Sprintf("Failed to create table schema: %v", err))
	}

	// Run tests
	code := m.Run()

	// Cleanup
	if mysqlDB != nil {
		mysqlDB.Close()
	}
	if mysqlContainer != nil {
		mysqlContainer.Terminate(ctx)
	}

	os.Exit(code)
}

func cleanupServersTable(t *testing.T) {
	_, err := mysqlDB.db.Exec("DELETE FROM servers")
	require.NoError(t, err)
}

func TestMySQLDB_List(t *testing.T) {
	ctx := context.Background()

	// Test empty list
	servers, nextCursor, err := mysqlDB.List(ctx, nil, "", 10)
	require.NoError(t, err)
	assert.Empty(t, servers)
	assert.Empty(t, nextCursor)

	// Test with data
	server := &model.ServerDetail{
		Server: model.Server{
			ID:          "test-server-1",
			Name:        "Test Server 1",
			Description: "Test Description",
			Repository: model.Repository{
				URL:    "https://github.com/test/repo",
				Source: "github",
				ID:     "test/repo",
			},
			VersionDetail: model.VersionDetail{
				Version:     "1.0.0",
				ReleaseDate: time.Now().Format(time.RFC3339),
				IsLatest:    true,
			},
		},
		Packages: []model.Package{
			{
				Name:    "test-package",
				Version: "1.0.0",
			},
		},
		Remotes: []model.Remote{
			{
				TransportType: "http",
				URL:          "https://test.com",
			},
		},
	}

	err = mysqlDB.Publish(ctx, server)
	require.NoError(t, err)

	servers, nextCursor, err = mysqlDB.List(ctx, nil, "", 10)
	require.NoError(t, err)
	assert.Len(t, servers, 1)
	assert.Empty(t, nextCursor)
	assert.Equal(t, server.ID, servers[0].ID)
	assert.Equal(t, server.Name, servers[0].Name)
}

func TestMySQLDB_GetByID(t *testing.T) {
	ctx := context.Background()

	// Test non-existent server
	_, err := mysqlDB.GetByID(ctx, "non-existent")
	assert.Error(t, err)

	// Test existing server
	server := &model.ServerDetail{
		Server: model.Server{
			ID:          "test-server-2",
			Name:        "Test Server 2",
			Description: "Test Description",
			Repository: model.Repository{
				URL:    "https://github.com/test/repo2",
				Source: "github",
				ID:     "test/repo2",
			},
			VersionDetail: model.VersionDetail{
				Version:     "1.0.0",
				ReleaseDate: time.Now().Format(time.RFC3339),
				IsLatest:    true,
			},
		},
		Packages: []model.Package{
			{
				Name:    "test-package",
				Version: "1.0.0",
			},
		},
		Remotes: []model.Remote{
			{
				TransportType: "http",
				URL:          "https://test.com",
			},
		},
	}

	err = mysqlDB.Publish(ctx, server)
	require.NoError(t, err)

	retrieved, err := mysqlDB.GetByID(ctx, server.ID)
	require.NoError(t, err)
	assert.Equal(t, server.ID, retrieved.ID)
	assert.Equal(t, server.Name, retrieved.Name)
}

func TestMySQLDB_Publish(t *testing.T) {
	ctx := context.Background()

	server := &model.ServerDetail{
		Server: model.Server{
			ID:          "test-server-3",
			Name:        "Test Server 3",
			Description: "Test Description",
			Repository: model.Repository{
				URL:    "https://github.com/test/repo3",
				Source: "github",
				ID:     "test/repo3",
			},
			VersionDetail: model.VersionDetail{
				Version:     "1.0.0",
				ReleaseDate: time.Now().Format(time.RFC3339),
				IsLatest:    true,
			},
		},
		Packages: []model.Package{
			{
				Name:    "test-package",
				Version: "1.0.0",
			},
		},
		Remotes: []model.Remote{
			{
				TransportType: "http",
				URL:          "https://test.com",
			},
		},
	}

	// Test publishing new server
	err := mysqlDB.Publish(ctx, server)
	require.NoError(t, err)

	// Test updating existing server
	server.Description = "Updated Description"
	server.VersionDetail.Version = "1.0.1" // Increment version for update
	err = mysqlDB.Publish(ctx, server)
	require.NoError(t, err)

	retrieved, err := mysqlDB.GetByID(ctx, server.ID)
	require.NoError(t, err)
	assert.Equal(t, "Updated Description", retrieved.Description)
}

func TestMySQLDB_Connection(t *testing.T) {
	conn := mysqlDB.Connection()
	assert.NotNil(t, conn)
}

func TestMySQLDB_List_Pagination(t *testing.T) {
	cleanupServersTable(t)
	ctx := context.Background()

	// Create multiple test servers
	servers := []*model.ServerDetail{
		{
			Server: model.Server{
				ID:          "test-server-pag-1",
				Name:        "Test Server Pag 1",
				Description: "Test Description 1",
				Repository: model.Repository{
					URL:    "https://github.com/test/repo1",
					Source: "github",
					ID:     "test/repo1",
				},
				VersionDetail: model.VersionDetail{
					Version:     "1.0.0",
					ReleaseDate: time.Now().Format(time.RFC3339),
					IsLatest:    true,
				},
			},
		},
		{
			Server: model.Server{
				ID:          "test-server-pag-2",
				Name:        "Test Server Pag 2",
				Description: "Test Description 2",
				Repository: model.Repository{
					URL:    "https://github.com/test/repo2",
					Source: "github",
					ID:     "test/repo2",
				},
				VersionDetail: model.VersionDetail{
					Version:     "1.0.0",
					ReleaseDate: time.Now().Format(time.RFC3339),
					IsLatest:    true,
				},
			},
		},
		{
			Server: model.Server{
				ID:          "test-server-pag-3",
				Name:        "Test Server Pag 3",
				Description: "Test Description 3",
				Repository: model.Repository{
					URL:    "https://github.com/test/repo3",
					Source: "github",
					ID:     "test/repo3",
				},
				VersionDetail: model.VersionDetail{
					Version:     "1.0.0",
					ReleaseDate: time.Now().Format(time.RFC3339),
					IsLatest:    true,
				},
			},
		},
	}

	// Insert test servers
	for _, server := range servers {
		err := mysqlDB.Publish(ctx, server)
		require.NoError(t, err)
	}

	// Test pagination with limit 2
	results, nextCursor, err := mysqlDB.List(ctx, nil, "", 2)
	require.NoError(t, err)
	assert.Len(t, results, 2)
	assert.NotEmpty(t, nextCursor)

	// Test second page
	results2, nextCursor2, err := mysqlDB.List(ctx, nil, nextCursor, 2)
	require.NoError(t, err)
	assert.Len(t, results2, 1)
	assert.Empty(t, nextCursor2)

	// Verify no overlap between pages
	ids := make(map[string]bool)
	for _, r := range results {
		ids[r.ID] = true
	}
	for _, r := range results2 {
		assert.False(t, ids[r.ID], "Found duplicate ID in second page")
	}
}

func TestMySQLDB_List_Filtering(t *testing.T) {
	cleanupServersTable(t)
	ctx := context.Background()

	// Create test servers with different names and versions
	servers := []*model.ServerDetail{
		{
			Server: model.Server{
				ID:          "test-server-filter-1",
				Name:        "Filter Test 1",
				Description: "Test Description 1",
				Repository: model.Repository{
					URL:    "https://github.com/test/repo1",
					Source: "github",
					ID:     "test/repo1",
				},
				VersionDetail: model.VersionDetail{
					Version:     "1.0.0",
					ReleaseDate: time.Now().Format(time.RFC3339),
					IsLatest:    true,
				},
			},
		},
		{
			Server: model.Server{
				ID:          "test-server-filter-2",
				Name:        "Filter Test 2",
				Description: "Test Description 2",
				Repository: model.Repository{
					URL:    "https://github.com/test/repo2",
					Source: "github",
					ID:     "test/repo2",
				},
				VersionDetail: model.VersionDetail{
					Version:     "2.0.0",
					ReleaseDate: time.Now().Format(time.RFC3339),
					IsLatest:    true,
				},
			},
		},
	}

	// Insert test servers
	for _, server := range servers {
		err := mysqlDB.Publish(ctx, server)
		require.NoError(t, err)
	}

	// Test filtering by name
	results, _, err := mysqlDB.List(ctx, map[string]interface{}{"name": "Filter Test 1"}, "", 10)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "Filter Test 1", results[0].Name)

	// Test filtering by version
	results, _, err = mysqlDB.List(ctx, map[string]interface{}{"version": "2.0.0"}, "", 10)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "2.0.0", results[0].VersionDetail.Version)

	// Test filtering with non-existent values
	results, _, err = mysqlDB.List(ctx, map[string]interface{}{"name": "Non Existent"}, "", 10)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestMySQLDB_List_Limit(t *testing.T) {
	cleanupServersTable(t)
	ctx := context.Background()

	// Create multiple test servers
	servers := []*model.ServerDetail{
		{
			Server: model.Server{
				ID:          "test-server-limit-1",
				Name:        "Test Server Limit 1",
				Description: "Test Description 1",
				Repository: model.Repository{
					URL:    "https://github.com/test/repo1",
					Source: "github",
					ID:     "test/repo1",
				},
				VersionDetail: model.VersionDetail{
					Version:     "1.0.0",
					ReleaseDate: time.Now().Format(time.RFC3339),
					IsLatest:    true,
				},
			},
		},
		{
			Server: model.Server{
				ID:          "test-server-limit-2",
				Name:        "Test Server Limit 2",
				Description: "Test Description 2",
				Repository: model.Repository{
					URL:    "https://github.com/test/repo2",
					Source: "github",
					ID:     "test/repo2",
				},
				VersionDetail: model.VersionDetail{
					Version:     "1.0.0",
					ReleaseDate: time.Now().Format(time.RFC3339),
					IsLatest:    true,
				},
			},
		},
	}

	// Insert test servers
	for _, server := range servers {
		err := mysqlDB.Publish(ctx, server)
		require.NoError(t, err)
	}

	// Test with limit 1
	results, nextCursor, err := mysqlDB.List(ctx, nil, "", 1)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.NotEmpty(t, nextCursor)

	// Test with limit 0 (should use default)
	results, _, err = mysqlDB.List(ctx, nil, "", 0)
	require.NoError(t, err)
	assert.Len(t, results, 2) // We only have 2 servers in the test data
}

func TestMySQLDB_Publish_VersionValidation(t *testing.T) {
	cleanupServersTable(t)
	ctx := context.Background()

	// Create initial server
	server := &model.ServerDetail{
		Server: model.Server{
			ID:          "test-server-version-1",
			Name:        "Test Server Version",
			Description: "Test Description",
			Repository: model.Repository{
				URL:    "https://github.com/test/repo",
				Source: "github",
				ID:     "test/repo",
			},
			VersionDetail: model.VersionDetail{
				Version:     "1.0.0",
				ReleaseDate: time.Now().Format(time.RFC3339),
				IsLatest:    true,
			},
		},
	}

	// Publish initial version
	err := mysqlDB.Publish(ctx, server)
	require.NoError(t, err)

	// Try to publish same version
	server.ID = "test-server-version-2" // New ID to avoid duplicate ID error
	err = mysqlDB.Publish(ctx, server)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid version: cannot publish older version after newer version")

	// Try to publish lower version
	server.VersionDetail.Version = "0.9.0"
	err = mysqlDB.Publish(ctx, server)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid version: cannot publish older version after newer version")

	// Publish higher version
	server.VersionDetail.Version = "2.0.0"
	err = mysqlDB.Publish(ctx, server)
	require.NoError(t, err)

	// Verify latest version flag
	results, _, err := mysqlDB.List(ctx, map[string]interface{}{"name": "Test Server Version"}, "", 10)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "2.0.0", results[0].VersionDetail.Version)
	assert.True(t, results[0].VersionDetail.IsLatest)
}

func TestMySQLDB_Publish_DuplicateID(t *testing.T) {
	cleanupServersTable(t)
	ctx := context.Background()

	// Create initial server
	server := &model.ServerDetail{
		Server: model.Server{
			ID:          "test-server-duplicate",
			Name:        "Test Server Duplicate",
			Description: "Test Description",
			Repository: model.Repository{
				URL:    "https://github.com/test/repo",
				Source: "github",
				ID:     "test/repo",
			},
			VersionDetail: model.VersionDetail{
				Version:     "1.0.0",
				ReleaseDate: time.Now().Format(time.RFC3339),
				IsLatest:    true,
			},
		},
	}

	// Publish initial version
	err := mysqlDB.Publish(ctx, server)
	require.NoError(t, err)

	// Try to publish with same ID and version
	err = mysqlDB.Publish(ctx, server)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid version: cannot publish older version after newer version")
} 