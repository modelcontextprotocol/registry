package database_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/registry/internal/database"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testVersion100 = "1.0.0"

func TestPostgreSQL_CreateServer(t *testing.T) {
	db := database.NewTestDB(t)
	ctx := context.Background()
	timeNow := time.Now()

	tests := []struct {
		name         string
		serverJSON   *apiv0.ServerJSON
		officialMeta *apiv0.RegistryExtensions
		expectError  bool
		errorType    error
	}{
		{
			name: "successful server creation",
			serverJSON: &apiv0.ServerJSON{
				Name:        "com.example/test-server",
				Description: "A test server",
				Version:     "1.0.0",
				Remotes: []model.Transport{
					{Type: "http", URL: "https://api.example.com/mcp"},
				},
			},
			officialMeta: &apiv0.RegistryExtensions{
				Status:          model.StatusActive,
				StatusChangedAt: timeNow,
				PublishedAt:     timeNow,
				UpdatedAt:       timeNow,
				IsLatest:        true,
			},
			expectError: false,
		},
		{
			name: "duplicate server version should fail",
			serverJSON: &apiv0.ServerJSON{
				Name:        "com.example/duplicate-server",
				Description: "A duplicate test server",
				Version:     "1.0.0",
			},
			officialMeta: &apiv0.RegistryExtensions{
				Status:          model.StatusActive,
				StatusChangedAt: timeNow,
				PublishedAt:     timeNow,
				UpdatedAt:       timeNow,
				IsLatest:        true,
			},
			expectError: true,
			// Note: Expecting generic database error for constraint violation
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create the first server to test duplicates
			if tt.name == "duplicate server version should fail" {
				_, err := db.CreateServer(ctx, nil, tt.serverJSON, tt.officialMeta)
				require.NoError(t, err, "First creation should succeed")
			}

			result, err := db.CreateServer(ctx, nil, tt.serverJSON, tt.officialMeta)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorType != nil {
					assert.ErrorIs(t, err, tt.errorType)
				}
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, tt.serverJSON.Name, result.Server.Name)
				assert.Equal(t, tt.serverJSON.Version, result.Server.Version)
				assert.Equal(t, tt.serverJSON.Description, result.Server.Description)
				assert.NotNil(t, result.Meta.Official)
				assert.Equal(t, tt.officialMeta.Status, result.Meta.Official.Status)
				assert.Equal(t, tt.officialMeta.IsLatest, result.Meta.Official.IsLatest)
			}
		})
	}
}

func TestPostgreSQL_GetServerByName(t *testing.T) {
	db := database.NewTestDB(t)
	ctx := context.Background()
	timeNow := time.Now()

	// Setup test data
	serverJSON := &apiv0.ServerJSON{
		Name:        "com.example/get-test-server",
		Description: "A server for get testing",
		Version:     "1.0.0",
	}
	officialMeta := &apiv0.RegistryExtensions{
		Status:          model.StatusActive,
		StatusChangedAt: timeNow,
		PublishedAt:     timeNow,
		UpdatedAt:       timeNow,
		IsLatest:        true,
	}

	// Create the server
	_, err := db.CreateServer(ctx, nil, serverJSON, officialMeta)
	require.NoError(t, err)

	tests := []struct {
		name        string
		serverName  string
		expectError bool
		errorType   error
	}{
		{
			name:       "get existing server",
			serverName: "com.example/get-test-server",
		},
		{
			name:        "get non-existent server",
			serverName:  "com.example/non-existent",
			expectError: true,
			errorType:   database.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := db.GetServerByName(ctx, nil, tt.serverName, false)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorType != nil {
					assert.ErrorIs(t, err, tt.errorType)
				}
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, tt.serverName, result.Server.Name)
				assert.NotNil(t, result.Meta.Official)
			}
		})
	}
}

func TestPostgreSQL_GetServerByNameAndVersion(t *testing.T) {
	db := database.NewTestDB(t)
	ctx := context.Background()
	timeNow := time.Now()

	// Setup test data with multiple versions
	serverName := "com.example/version-test-server"
	versions := []string{"1.0.0", "1.1.0", "2.0.0"}

	for i, version := range versions {
		serverJSON := &apiv0.ServerJSON{
			Name:        serverName,
			Description: "A server for version testing",
			Version:     version,
		}
		officialMeta := &apiv0.RegistryExtensions{
			Status:          model.StatusActive,
			StatusChangedAt: timeNow,
			PublishedAt:     timeNow,
			UpdatedAt:       timeNow,
			IsLatest:        i == len(versions)-1, // Only last version is latest
		}

		_, err := db.CreateServer(ctx, nil, serverJSON, officialMeta)
		require.NoError(t, err)
	}

	tests := []struct {
		name        string
		serverName  string
		version     string
		expectError bool
		errorType   error
	}{
		{
			name:       "get existing server version",
			serverName: serverName,
			version:    "1.1.0",
		},
		{
			name:        "get non-existent version",
			serverName:  serverName,
			version:     "3.0.0",
			expectError: true,
			errorType:   database.ErrNotFound,
		},
		{
			name:        "get non-existent server",
			serverName:  "com.example/non-existent",
			version:     "1.0.0",
			expectError: true,
			errorType:   database.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := db.GetServerByNameAndVersion(ctx, nil, tt.serverName, tt.version, false)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorType != nil {
					assert.ErrorIs(t, err, tt.errorType)
				}
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, tt.serverName, result.Server.Name)
				assert.Equal(t, tt.version, result.Server.Version)
				assert.NotNil(t, result.Meta.Official)
			}
		})
	}
}

func TestPostgreSQL_ListServers(t *testing.T) {
	db := database.NewTestDB(t)
	ctx := context.Background()

	// Setup test data
	testServers := []struct {
		name        string
		version     string
		status      model.Status
		remoteURL   string
		isLatest    bool
		publishedAt time.Time
	}{
		{
			name:        "com.example/server-a",
			version:     "1.0.0",
			status:      model.StatusActive,
			remoteURL:   "https://api-a.example.com/mcp",
			isLatest:    true,
			publishedAt: time.Now().Add(-2 * time.Hour),
		},
		{
			name:        "com.example/server-b",
			version:     "2.0.0",
			status:      model.StatusActive,
			remoteURL:   "https://api-b.example.com/mcp",
			isLatest:    true,
			publishedAt: time.Now().Add(-1 * time.Hour),
		},
		{
			name:        "com.example/server-c",
			version:     "1.0.0",
			status:      model.StatusDeprecated,
			remoteURL:   "https://api-c.example.com/mcp",
			isLatest:    true,
			publishedAt: time.Now().Add(-30 * time.Minute),
		},
	}

	// Create test servers
	for _, server := range testServers {
		serverJSON := &apiv0.ServerJSON{
			Name:        server.name,
			Description: "Test server for listing",
			Version:     server.version,
			Remotes: []model.Transport{
				{Type: "http", URL: server.remoteURL},
			},
		}
		officialMeta := &apiv0.RegistryExtensions{
			Status:          server.status,
			StatusChangedAt: server.publishedAt,
			PublishedAt:     server.publishedAt,
			UpdatedAt:       server.publishedAt,
			IsLatest:        server.isLatest,
		}

		_, err := db.CreateServer(ctx, nil, serverJSON, officialMeta)
		require.NoError(t, err)
	}

	tests := []struct {
		name          string
		filter        *database.ServerFilter
		cursor        string
		limit         int
		expectedCount int
		expectedNames []string
		expectError   bool
	}{
		{
			name:          "list all servers",
			filter:        nil,
			limit:         10,
			expectedCount: 3,
			expectedNames: []string{"com.example/server-a", "com.example/server-b", "com.example/server-c"},
		},
		{
			name: "filter by name",
			filter: &database.ServerFilter{
				Name: stringPtr("com.example/server-a"),
			},
			limit:         10,
			expectedCount: 1,
			expectedNames: []string{"com.example/server-a"},
		},
		{
			name: "filter by remote URL",
			filter: &database.ServerFilter{
				RemoteURL: stringPtr("https://api-b.example.com/mcp"),
			},
			limit:         10,
			expectedCount: 1,
			expectedNames: []string{"com.example/server-b"},
		},
		{
			name: "filter by substring name",
			filter: &database.ServerFilter{
				SubstringName: stringPtr("server-"),
			},
			limit:         10,
			expectedCount: 3,
		},
		{
			name: "filter by version",
			filter: &database.ServerFilter{
				Version: stringPtr("1.0.0"),
			},
			limit:         10,
			expectedCount: 2,
		},
		{
			name: "filter by isLatest",
			filter: &database.ServerFilter{
				IsLatest: boolPtr(true),
			},
			limit:         10,
			expectedCount: 3,
		},
		{
			name: "filter by updatedSince",
			filter: &database.ServerFilter{
				UpdatedSince: timePtr(time.Now().Add(-45 * time.Minute)),
			},
			limit:         10,
			expectedCount: 1, // Only server-c was updated in the last 45 minutes
		},
		{
			name:          "test pagination with limit",
			filter:        nil,
			limit:         2,
			expectedCount: 2,
		},
		{
			name:   "test cursor pagination",
			filter: nil,
			cursor: "com.example/server-a",
			limit:  10,
			// Should return servers after 'server-a' alphabetically
			expectedCount: 2,
			expectedNames: []string{"com.example/server-b", "com.example/server-c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, nextCursor, err := db.ListServers(ctx, nil, tt.filter, tt.cursor, tt.limit)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Len(t, results, tt.expectedCount)

			if len(tt.expectedNames) > 0 {
				actualNames := make([]string, len(results))
				for i, result := range results {
					actualNames[i] = result.Server.Name
				}
				assert.Subset(t, tt.expectedNames, actualNames)
			}

			// Test cursor behavior
			if tt.limit < len(testServers) && len(results) == tt.limit {
				assert.NotEmpty(t, nextCursor, "Should return next cursor when results are limited")
			}
		})
	}
}

func TestPostgreSQL_UpdateServer(t *testing.T) {
	db := database.NewTestDB(t)
	ctx := context.Background()
	timeNow := time.Now()

	// Setup test data
	serverName := "com.example/update-test-server"
	version := testVersion100
	serverJSON := &apiv0.ServerJSON{
		Name:        serverName,
		Description: "Original description",
		Version:     version,
	}
	officialMeta := &apiv0.RegistryExtensions{
		Status:          model.StatusActive,
		StatusChangedAt: timeNow,
		PublishedAt:     timeNow,
		UpdatedAt:       timeNow,
		IsLatest:        true,
	}

	_, err := db.CreateServer(ctx, nil, serverJSON, officialMeta)
	require.NoError(t, err)

	tests := []struct {
		name          string
		serverName    string
		version       string
		updatedServer *apiv0.ServerJSON
		expectError   bool
		errorType     error
	}{
		{
			name:       "successful server update",
			serverName: serverName,
			version:    version,
			updatedServer: &apiv0.ServerJSON{
				Name:        serverName,
				Description: "Updated description",
				Version:     version,
				Remotes: []model.Transport{
					{Type: "http", URL: "https://updated.example.com/mcp"},
				},
			},
		},
		{
			name:       "update non-existent server",
			serverName: "com.example/non-existent",
			version:    testVersion100,
			updatedServer: &apiv0.ServerJSON{
				Name:        "com.example/non-existent",
				Description: "Should fail",
				Version:     testVersion100,
			},
			expectError: true,
			errorType:   database.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := db.UpdateServer(ctx, nil, tt.serverName, tt.version, tt.updatedServer)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorType != nil {
					assert.ErrorIs(t, err, tt.errorType)
				}
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, tt.updatedServer.Description, result.Server.Description)
				assert.NotNil(t, result.Meta.Official)
				assert.NotZero(t, result.Meta.Official.UpdatedAt)
			}
		})
	}
}

func TestPostgreSQL_SetServerStatus(t *testing.T) {
	db := database.NewTestDB(t)
	ctx := context.Background()
	timeNow := time.Now()

	// Setup test data
	serverName := "com.example/status-test-server"
	version := testVersion100
	serverJSON := &apiv0.ServerJSON{
		Name:        serverName,
		Description: "A server for status testing",
		Version:     version,
	}
	officialMeta := &apiv0.RegistryExtensions{
		Status:          model.StatusActive,
		StatusChangedAt: timeNow,
		PublishedAt:     timeNow,
		UpdatedAt:       timeNow,
		IsLatest:        true,
	}

	_, err := db.CreateServer(ctx, nil, serverJSON, officialMeta)
	require.NoError(t, err)

	tests := []struct {
		name        string
		serverName  string
		version     string
		newStatus   string
		expectError bool
		errorType   error
	}{
		{
			name:       "active to deprecated",
			serverName: serverName,
			version:    version,
			newStatus:  string(model.StatusDeprecated),
		},
		{
			name:        "invalid status",
			serverName:  serverName,
			version:     version,
			newStatus:   "invalid_status",
			expectError: true,
		},
		{
			name:        "non-existent server",
			serverName:  "com.example/non-existent",
			version:     "1.0.1",
			newStatus:   string(model.StatusDeprecated),
			expectError: true,
			errorType:   database.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := db.SetServerStatus(ctx, nil, tt.serverName, tt.version, model.Status(tt.newStatus), nil)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorType != nil {
					assert.ErrorIs(t, err, tt.errorType)
				}
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, model.Status(tt.newStatus), result.Meta.Official.Status)
				assert.NotZero(t, result.Meta.Official.UpdatedAt)
			}
		})
	}
}

func TestPostgreSQL_StatusChangedAtBehavior(t *testing.T) {
	db := database.NewTestDB(t)
	ctx := context.Background()

	t.Run("status_changed_at updates when status changes", func(t *testing.T) {
		timeNow := time.Now()
		serverJSON := &apiv0.ServerJSON{
			Name:        "com.example/status-changed-at-test",
			Description: "Test server",
			Version:     "1.0.0",
		}
		officialMeta := &apiv0.RegistryExtensions{
			Status:          model.StatusActive,
			StatusChangedAt: timeNow,
			PublishedAt:     timeNow,
			UpdatedAt:       timeNow,
			IsLatest:        true,
		}

		_, err := db.CreateServer(ctx, nil, serverJSON, officialMeta)
		require.NoError(t, err)

		// Wait a bit to ensure time difference
		time.Sleep(10 * time.Millisecond)

		// Change status from active to deprecated
		result, err := db.SetServerStatus(ctx, nil, serverJSON.Name, serverJSON.Version, model.StatusDeprecated, nil)
		require.NoError(t, err)

		assert.Equal(t, model.StatusDeprecated, result.Meta.Official.Status)
		assert.True(t, result.Meta.Official.StatusChangedAt.After(timeNow), "status_changed_at should be updated")
	})

	t.Run("status_changed_at preserved when only message changes", func(t *testing.T) {
		timeNow := time.Now()
		serverJSON := &apiv0.ServerJSON{
			Name:        "com.example/message-only-test",
			Description: "Test server",
			Version:     "1.0.0",
		}
		officialMeta := &apiv0.RegistryExtensions{
			Status:          model.StatusDeprecated,
			StatusChangedAt: timeNow,
			PublishedAt:     timeNow,
			UpdatedAt:       timeNow,
			IsLatest:        true,
		}

		_, err := db.CreateServer(ctx, nil, serverJSON, officialMeta)
		require.NoError(t, err)

		// Wait a bit to ensure time difference
		time.Sleep(10 * time.Millisecond)

		// Update only the message, keep status the same
		newMessage := "Updated message"
		result, err := db.SetServerStatus(ctx, nil, serverJSON.Name, serverJSON.Version, model.StatusDeprecated, &newMessage)
		require.NoError(t, err)

		assert.Equal(t, model.StatusDeprecated, result.Meta.Official.Status)
		assert.Equal(t, "Updated message", *result.Meta.Official.StatusMessage)
		// status_changed_at should NOT be updated since status didn't change
		assert.True(t, result.Meta.Official.StatusChangedAt.Before(result.Meta.Official.UpdatedAt), "status_changed_at should be older than updated_at")
	})
}

func TestPostgreSQL_TransactionHandling(t *testing.T) {
	db := database.NewTestDB(t)
	ctx := context.Background()

	t.Run("successful transaction", func(t *testing.T) {
		timeNow := time.Now()
		err := db.InTransaction(ctx, func(ctx context.Context, tx pgx.Tx) error {
			serverJSON := &apiv0.ServerJSON{
				Name:        "com.example/transaction-success",
				Description: "Transaction test server",
				Version:     "1.0.0",
			}
			officialMeta := &apiv0.RegistryExtensions{
				Status:          model.StatusActive,
				StatusChangedAt: timeNow,
				PublishedAt:     timeNow,
				UpdatedAt:       timeNow,
				IsLatest:        true,
			}

			_, err := db.CreateServer(ctx, tx, serverJSON, officialMeta)
			return err
		})

		assert.NoError(t, err)

		// Verify server was created
		result, err := db.GetServerByName(ctx, nil, "com.example/transaction-success", false)
		assert.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("failed transaction rollback", func(t *testing.T) {
		timeNow := time.Now()
		err := db.InTransaction(ctx, func(ctx context.Context, tx pgx.Tx) error {
			serverJSON := &apiv0.ServerJSON{
				Name:        "com.example/transaction-rollback",
				Description: "Transaction rollback test server",
				Version:     "1.0.0",
			}
			officialMeta := &apiv0.RegistryExtensions{
				Status:          model.StatusActive,
				StatusChangedAt: timeNow,
				PublishedAt:     timeNow,
				UpdatedAt:       timeNow,
				IsLatest:        true,
			}

			_, err := db.CreateServer(ctx, tx, serverJSON, officialMeta)
			if err != nil {
				return err
			}

			// Force an error to trigger rollback
			return assert.AnError
		})

		assert.Error(t, err)
		assert.Equal(t, assert.AnError, err)

		// Verify server was NOT created due to rollback
		result, err := db.GetServerByName(ctx, nil, "com.example/transaction-rollback", false)
		assert.Error(t, err)
		assert.ErrorIs(t, err, database.ErrNotFound)
		assert.Nil(t, result)
	})
}

func TestPostgreSQL_ConcurrencyAndLocking(t *testing.T) {
	db := database.NewTestDB(t)
	ctx := context.Background()

	serverName := "com.example/concurrent-server"

	// Test advisory locking prevents concurrent publishes
	t.Run("advisory locking prevents race conditions", func(t *testing.T) {
		published := make(chan bool, 2)
		errors := make(chan error, 2)

		// Launch two concurrent publish operations
		for i := 0; i < 2; i++ {
			go func(version string) {
				timeNow := time.Now()
				err := db.InTransaction(ctx, func(ctx context.Context, tx pgx.Tx) error {
					// Acquire lock
					if err := db.AcquirePublishLock(ctx, tx, serverName); err != nil {
						return err
					}

					// Simulate some processing time
					time.Sleep(100 * time.Millisecond)

					serverJSON := &apiv0.ServerJSON{
						Name:        serverName,
						Description: "Concurrent test server",
						Version:     version,
					}
					officialMeta := &apiv0.RegistryExtensions{
						Status:          model.StatusActive,
						StatusChangedAt: timeNow,
						PublishedAt:     timeNow,
						UpdatedAt:       timeNow,
						IsLatest:        true,
					}

					_, err := db.CreateServer(ctx, tx, serverJSON, officialMeta)
					if err != nil {
						return err
					}

					published <- true
					return nil
				})
				errors <- err
			}(time.Now().Format("20060102150405.000000"))
		}

		// Wait for both goroutines to complete
		err1 := <-errors
		err2 := <-errors

		// One should succeed, one should wait (or fail if timeout)
		successCount := 0
		if err1 == nil {
			successCount++
		}
		if err2 == nil {
			successCount++
		}

		// At least one should succeed (both can succeed if advisory lock works properly)
		assert.GreaterOrEqual(t, successCount, 1, "At least one concurrent operation should succeed")

		close(published)
		close(errors)
	})
}

func TestPostgreSQL_HelperMethods(t *testing.T) {
	db := database.NewTestDB(t)
	ctx := context.Background()
	timeNow := time.Now()

	serverName := "com.example/helper-test-server"

	// Setup test data with multiple versions
	versions := []string{"1.0.0", "1.1.0", "2.0.0"}
	for _, version := range versions {
		serverJSON := &apiv0.ServerJSON{
			Name:        serverName,
			Description: "Helper methods test server",
			Version:     version,
		}
		officialMeta := &apiv0.RegistryExtensions{
			Status:          model.StatusActive,
			StatusChangedAt: timeNow,
			PublishedAt:     timeNow,
			UpdatedAt:       timeNow,
			IsLatest:        version == "2.0.0",
		}

		_, err := db.CreateServer(ctx, nil, serverJSON, officialMeta)
		require.NoError(t, err)
	}

	t.Run("CountServerVersions", func(t *testing.T) {
		count, err := db.CountServerVersions(ctx, nil, serverName)
		assert.NoError(t, err)
		assert.Equal(t, 3, count)

		// Test non-existent server
		count, err = db.CountServerVersions(ctx, nil, "com.example/non-existent")
		assert.NoError(t, err)
		assert.Equal(t, 0, count)
	})

	t.Run("CheckVersionExists", func(t *testing.T) {
		exists, err := db.CheckVersionExists(ctx, nil, serverName, "1.1.0")
		assert.NoError(t, err)
		assert.True(t, exists)

		exists, err = db.CheckVersionExists(ctx, nil, serverName, "3.0.0")
		assert.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("GetCurrentLatestVersion", func(t *testing.T) {
		latest, err := db.GetCurrentLatestVersion(ctx, nil, serverName)
		assert.NoError(t, err)
		assert.NotNil(t, latest)
		assert.Equal(t, "2.0.0", latest.Server.Version)
		assert.True(t, latest.Meta.Official.IsLatest)
	})

	t.Run("GetAllVersionsByServerName", func(t *testing.T) {
		allVersions, err := db.GetAllVersionsByServerName(ctx, nil, serverName, false)
		assert.NoError(t, err)
		assert.Len(t, allVersions, 3)

		// Check versions are present
		versionSet := make(map[string]bool)
		for _, server := range allVersions {
			versionSet[server.Server.Version] = true
		}
		for _, expectedVersion := range versions {
			assert.True(t, versionSet[expectedVersion], "Version %s should be present", expectedVersion)
		}
	})

	t.Run("UnmarkAsLatest", func(t *testing.T) {
		err := db.UnmarkAsLatest(ctx, nil, serverName)
		assert.NoError(t, err)

		// Verify no version is marked as latest
		latest, err := db.GetCurrentLatestVersion(ctx, nil, serverName)
		assert.Error(t, err)
		assert.ErrorIs(t, err, database.ErrNotFound)
		assert.Nil(t, latest)
	})
}

func TestPostgreSQL_EdgeCases(t *testing.T) {
	db := database.NewTestDB(t)
	ctx := context.Background()
	timeNow := time.Now()

	t.Run("input validation", func(t *testing.T) {
		// Test nil inputs
		_, err := db.CreateServer(ctx, nil, nil, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "serverJSON and officialMeta are required")

		// Test empty required fields
		_, err = db.CreateServer(ctx, nil, &apiv0.ServerJSON{}, &apiv0.RegistryExtensions{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "server name and version are required")
	})

	t.Run("database constraints", func(t *testing.T) {
		// Test server name format constraint (should be caught by database constraint)
		invalidServer := &apiv0.ServerJSON{
			Name:        "invalid-name-format", // Missing namespace/name format
			Description: "Invalid server",
			Version:     "1.0.0",
		}
		officialMeta := &apiv0.RegistryExtensions{
			Status:          model.StatusActive,
			StatusChangedAt: timeNow,
			PublishedAt:     timeNow,
			UpdatedAt:       timeNow,
			IsLatest:        true,
		}

		_, err := db.CreateServer(ctx, nil, invalidServer, officialMeta)
		assert.Error(t, err, "Should fail due to server name format constraint")
	})

	t.Run("pagination edge cases", func(t *testing.T) {
		// Test pagination with no results
		results, cursor, err := db.ListServers(ctx, nil, &database.ServerFilter{
			Name: stringPtr("com.example/non-existent-server"),
		}, "", 10)
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.Empty(t, cursor)

		// Test pagination with limit 0 (should use default)
		_, _, err = db.ListServers(ctx, nil, nil, "", 0)
		assert.NoError(t, err)
		// Should still work with default limit
	})

	t.Run("complex filtering", func(t *testing.T) {
		// Setup test data
		serverName := "com.example/complex-filter-server"
		testTime := time.Now().Add(-1 * time.Hour)

		_, err := db.CreateServer(ctx, nil, &apiv0.ServerJSON{
			Name:        serverName,
			Description: "Complex filter test server",
			Version:     "1.0.0",
			Remotes: []model.Transport{
				{Type: "streamable-http", URL: "https://complex.example.com/mcp"},
			},
		}, &apiv0.RegistryExtensions{
			Status:          model.StatusActive,
			StatusChangedAt: testTime,
			PublishedAt:     testTime,
			UpdatedAt:       testTime,
			IsLatest:        true,
		})
		require.NoError(t, err)

		// Test multiple filters combined
		filter := &database.ServerFilter{
			SubstringName: stringPtr("complex"),
			UpdatedSince:  timePtr(testTime.Add(-30 * time.Minute)),
			IsLatest:      boolPtr(true),
			Version:       stringPtr("1.0.0"),
		}

		results, _, err := db.ListServers(ctx, nil, filter, "", 10)
		assert.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, serverName, results[0].Server.Name)
	})

	t.Run("status transitions", func(t *testing.T) {
		serverName := "com.example/status-transition-server"
		version := "1.0.2"

		// Create server
		_, err := db.CreateServer(ctx, nil, &apiv0.ServerJSON{
			Name:        serverName,
			Description: "Status transition test",
			Version:     version,
		}, &apiv0.RegistryExtensions{
			Status:          model.StatusActive,
			StatusChangedAt: timeNow,
			PublishedAt:     timeNow,
			UpdatedAt:       timeNow,
			IsLatest:        true,
		})
		require.NoError(t, err)

		// Test all valid status transitions
		statuses := []string{
			string(model.StatusDeprecated),
			string(model.StatusDeleted),
			string(model.StatusActive), // Can transition back
		}

		for _, status := range statuses {
			result, err := db.SetServerStatus(ctx, nil, serverName, version, model.Status(status), nil)
			assert.NoError(t, err, "Should allow transition to %s", status)
			assert.Equal(t, model.Status(status), result.Meta.Official.Status)
		}
	})

	// Test status transitions with additional fields
	t.Run("status transitions with message", func(t *testing.T) {
		testServerName := "com.example/status-fields-test"
		testVersion := "1.0.0"

		// Create a test server
		_, err := db.CreateServer(ctx, nil, &apiv0.ServerJSON{
			Name:        testServerName,
			Description: "Status fields test",
			Version:     testVersion,
		}, &apiv0.RegistryExtensions{
			Status:          model.StatusActive,
			StatusChangedAt: timeNow,
			PublishedAt:     timeNow,
			UpdatedAt:       timeNow,
			IsLatest:        true,
		})
		require.NoError(t, err)

		statusMessage := "This server has been deprecated. Please use the new version."

		// Test setting status with message
		result, err := db.SetServerStatus(ctx, nil, testServerName, testVersion, model.StatusDeprecated, &statusMessage)
		assert.NoError(t, err)
		assert.Equal(t, model.StatusDeprecated, result.Meta.Official.Status)
		assert.NotNil(t, result.Meta.Official.StatusMessage)
		assert.Equal(t, statusMessage, *result.Meta.Official.StatusMessage)
		assert.NotZero(t, result.Meta.Official.StatusChangedAt)

		// Test clearing status message
		result, err = db.SetServerStatus(ctx, nil, testServerName, testVersion, model.StatusActive, nil)
		assert.NoError(t, err)
		assert.Equal(t, model.StatusActive, result.Meta.Official.Status)
		assert.Nil(t, result.Meta.Official.StatusMessage)
	})

	// Test comprehensive status transitions as per user requirements
	t.Run("comprehensive status transitions", func(t *testing.T) {
		testServerName := "com.example/comprehensive-transitions-test"
		testVersion := "1.0.0"

		// Create a test server in active status
		_, err := db.CreateServer(ctx, nil, &apiv0.ServerJSON{
			Name:        testServerName,
			Description: "Comprehensive transitions test",
			Version:     testVersion,
		}, &apiv0.RegistryExtensions{
			Status:          model.StatusActive,
			StatusChangedAt: timeNow,
			PublishedAt:     timeNow,
			UpdatedAt:       timeNow,
			IsLatest:        true,
		})
		require.NoError(t, err)

		// Define all valid transitions based on user requirements
		transitionTests := []struct {
			name        string
			fromStatus  model.Status
			toStatus    model.Status
			description string
		}{
			// Active ↔ Deprecated
			{"active to deprecated", model.StatusActive, model.StatusDeprecated, "Deprecating active server"},
			{"deprecated to active", model.StatusDeprecated, model.StatusActive, "Reactivating deprecated server"},

			// Active ↔ Deleted
			{"active to deleted", model.StatusActive, model.StatusDeleted, "Deleting active server"},
			{"deleted to active", model.StatusDeleted, model.StatusActive, "Restoring to active server"},

			// Deprecated ↔ Deleted
			{"deprecated to deleted", model.StatusDeprecated, model.StatusDeleted, "Deleting deprecated server"},
			{"deleted to deprecated", model.StatusDeleted, model.StatusDeprecated, "Moving deleted to deprecated"},
		}

		for _, tt := range transitionTests {
			t.Run(tt.name, func(t *testing.T) {
				// First ensure the server is in the expected starting status
				if tt.fromStatus != model.StatusActive {
					_, err := db.SetServerStatus(ctx, nil, testServerName, testVersion, tt.fromStatus, nil)
					require.NoError(t, err, "failed to set initial status to %s", tt.fromStatus)
				}

				// Verify the server is in the expected starting status
				// Use includeDeleted=true since we test transitions involving deleted status
				currentServer, err := db.GetServerByNameAndVersion(ctx, nil, testServerName, testVersion, true)
				require.NoError(t, err)
				assert.Equal(t, tt.fromStatus, currentServer.Meta.Official.Status, "server should be in %s status before transition", tt.fromStatus)

				// Perform the transition
				result, err := db.SetServerStatus(ctx, nil, testServerName, testVersion, tt.toStatus, &tt.description)
				assert.NoError(t, err, "should allow transition from %s to %s", tt.fromStatus, tt.toStatus)
				assert.NotNil(t, result)
				assert.Equal(t, tt.toStatus, result.Meta.Official.Status, "status should be %s after transition", tt.toStatus)
				assert.NotNil(t, result.Meta.Official.StatusMessage)
				assert.Equal(t, tt.description, *result.Meta.Official.StatusMessage)
				assert.NotZero(t, result.Meta.Official.StatusChangedAt)
			})
		}
	})
}

func TestPostgreSQL_PerformanceScenarios(t *testing.T) {
	db := database.NewTestDB(t)
	ctx := context.Background()
	timeNow := time.Now()

	t.Run("many versions management", func(t *testing.T) {
		serverName := "com.example/many-versions-server"

		// Create many versions (but stay under the limit)
		versionCount := 50
		for i := 0; i < versionCount; i++ {
			_, err := db.CreateServer(ctx, nil, &apiv0.ServerJSON{
				Name:        serverName,
				Description: fmt.Sprintf("Version %d", i),
				Version:     fmt.Sprintf("1.0.%d", i),
			}, &apiv0.RegistryExtensions{
				Status:          model.StatusActive,
				StatusChangedAt: timeNow,
				PublishedAt:     timeNow,
				UpdatedAt:       timeNow,
				IsLatest:        i == versionCount-1, // Only last one is latest
			})
			require.NoError(t, err)
		}

		// Test counting versions
		count, err := db.CountServerVersions(ctx, nil, serverName)
		assert.NoError(t, err)
		assert.Equal(t, versionCount, count)

		// Test getting all versions
		allVersions, err := db.GetAllVersionsByServerName(ctx, nil, serverName, false)
		assert.NoError(t, err)
		assert.Len(t, allVersions, versionCount)

		// Test only one is marked as latest
		latestCount := 0
		for _, version := range allVersions {
			if version.Meta.Official.IsLatest {
				latestCount++
			}
		}
		assert.Equal(t, 1, latestCount)
	})

	t.Run("large result pagination", func(t *testing.T) {
		// Create multiple servers for pagination testing
		serverCount := 25
		for i := 0; i < serverCount; i++ {
			_, err := db.CreateServer(ctx, nil, &apiv0.ServerJSON{
				Name:        fmt.Sprintf("com.example/pagination-server-%02d", i),
				Description: "Pagination test server",
				Version:     "1.0.0",
			}, &apiv0.RegistryExtensions{
				Status:          model.StatusActive,
				StatusChangedAt: timeNow,
				PublishedAt:     timeNow,
				UpdatedAt:       timeNow,
				IsLatest:        true,
			})
			require.NoError(t, err)
		}

		// Test paginated retrieval
		allResults := []*apiv0.ServerResponse{}
		cursor := ""
		pageSize := 10

		for {
			results, nextCursor, err := db.ListServers(ctx, nil, nil, cursor, pageSize)
			assert.NoError(t, err)
			allResults = append(allResults, results...)

			if nextCursor == "" || len(results) < pageSize {
				break
			}
			cursor = nextCursor
		}

		// Should have retrieved all servers including the ones we just created
		assert.GreaterOrEqual(t, len(allResults), serverCount)
	})
}

func TestPostgreSQL_NewStatusFields(t *testing.T) {
	db := database.NewTestDB(t)
	ctx := context.Background()
	timeNow := time.Now()

	t.Run("status_changed_at field functionality", func(t *testing.T) {
		serverJSON := &apiv0.ServerJSON{
			Name:        "com.example/status-changed-at-test",
			Description: "Test server for status_changed_at field",
			Version:     "1.0.0",
		}

		// Create server with specific status_changed_at
		officialMeta := &apiv0.RegistryExtensions{
			Status:          model.StatusActive,
			StatusChangedAt: timeNow,
			PublishedAt:     timeNow,
			UpdatedAt:       timeNow,
			IsLatest:        true,
		}

		_, err := db.CreateServer(ctx, nil, serverJSON, officialMeta)
		require.NoError(t, err)

		// Retrieve and verify status_changed_at
		result, err := db.GetServerByNameAndVersion(ctx, nil, serverJSON.Name, serverJSON.Version, false)
		require.NoError(t, err)
		assert.NotNil(t, result.Meta.Official)
		assert.Equal(t, timeNow.Unix(), result.Meta.Official.StatusChangedAt.Unix())
	})

	t.Run("status_message field functionality", func(t *testing.T) {
		serverJSON := &apiv0.ServerJSON{
			Name:        "com.example/status-message-test",
			Description: "Test server for status_message field",
			Version:     "1.0.0",
		}

		statusMessage := "This server is deprecated due to security issues. Please migrate to v2.0.0"
		officialMeta := &apiv0.RegistryExtensions{
			Status:          model.StatusDeprecated,
			StatusChangedAt: timeNow,
			StatusMessage:   &statusMessage,
			PublishedAt:     timeNow,
			UpdatedAt:       timeNow,
			IsLatest:        true,
		}

		_, err := db.CreateServer(ctx, nil, serverJSON, officialMeta)
		require.NoError(t, err)

		// Retrieve and verify status_message
		result, err := db.GetServerByNameAndVersion(ctx, nil, serverJSON.Name, serverJSON.Version, false)
		require.NoError(t, err)
		assert.NotNil(t, result.Meta.Official)
		assert.NotNil(t, result.Meta.Official.StatusMessage)
		assert.Equal(t, statusMessage, *result.Meta.Official.StatusMessage)
	})

	t.Run("deleted status functionality", func(t *testing.T) {
		serverJSON := &apiv0.ServerJSON{
			Name:        "com.example/deleted-status-test",
			Description: "Test server for deleted status",
			Version:     "1.0.0",
		}

		statusMessage := "This version has critical security vulnerabilities and has been deleted"

		officialMeta := &apiv0.RegistryExtensions{
			Status:          model.StatusDeleted,
			StatusChangedAt: timeNow,
			StatusMessage:   &statusMessage,
			PublishedAt:     timeNow,
			UpdatedAt:       timeNow,
			IsLatest:        false, // Deleted versions should not be latest
		}

		_, err := db.CreateServer(ctx, nil, serverJSON, officialMeta)
		require.NoError(t, err)

		// Retrieve and verify deleted status with all fields (need includeDeleted=true)
		result, err := db.GetServerByNameAndVersion(ctx, nil, serverJSON.Name, serverJSON.Version, true)
		require.NoError(t, err)
		assert.NotNil(t, result.Meta.Official)
		assert.Equal(t, model.StatusDeleted, result.Meta.Official.Status)
		assert.NotNil(t, result.Meta.Official.StatusMessage)
		assert.Equal(t, statusMessage, *result.Meta.Official.StatusMessage)
		assert.False(t, result.Meta.Official.IsLatest)
	})

	t.Run("nil status_message", func(t *testing.T) {
		serverJSON := &apiv0.ServerJSON{
			Name:        "com.example/nil-fields-test",
			Description: "Test server for nil optional fields",
			Version:     "1.0.0",
		}

		officialMeta := &apiv0.RegistryExtensions{
			Status:          model.StatusActive,
			StatusChangedAt: timeNow,
			StatusMessage:   nil, // Explicitly nil
			PublishedAt:     timeNow,
			UpdatedAt:       timeNow,
			IsLatest:        true,
		}

		_, err := db.CreateServer(ctx, nil, serverJSON, officialMeta)
		require.NoError(t, err)

		// Retrieve and verify nil fields are handled correctly
		result, err := db.GetServerByNameAndVersion(ctx, nil, serverJSON.Name, serverJSON.Version, false)
		require.NoError(t, err)
		assert.NotNil(t, result.Meta.Official)
		assert.Nil(t, result.Meta.Official.StatusMessage)
	})

	t.Run("status_changed_at constraint enforcement", func(t *testing.T) {
		serverJSON := &apiv0.ServerJSON{
			Name:        "com.example/constraint-test",
			Description: "Test server for constraint validation",
			Version:     "1.0.0",
		}

		// Try to create server with status_changed_at before published_at (should fail)
		earlierTime := timeNow.Add(-1 * time.Hour)
		officialMeta := &apiv0.RegistryExtensions{
			Status:          model.StatusActive,
			StatusChangedAt: earlierTime, // Before published_at
			PublishedAt:     timeNow,
			UpdatedAt:       timeNow,
			IsLatest:        true,
		}

		_, err := db.CreateServer(ctx, nil, serverJSON, officialMeta)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "check_status_changed_at_after_published")
	})

	t.Run("all status transitions work", func(t *testing.T) {
		// Test that we can create servers with all valid status values
		statuses := []model.Status{
			model.StatusActive,
			model.StatusDeprecated,
			model.StatusDeleted,
		}

		for i, status := range statuses {
			serverJSON := &apiv0.ServerJSON{
				Name:        fmt.Sprintf("com.example/status-test-%d", i),
				Description: fmt.Sprintf("Test server for status %s", status),
				Version:     "1.0.0",
			}

			officialMeta := &apiv0.RegistryExtensions{
				Status:          status,
				StatusChangedAt: timeNow,
				PublishedAt:     timeNow,
				UpdatedAt:       timeNow,
				IsLatest:        true,
			}

			_, err := db.CreateServer(ctx, nil, serverJSON, officialMeta)
			assert.NoError(t, err, "Should be able to create server with status: %s", status)

			// Verify the status was set correctly (use includeDeleted=true since we test all statuses including deleted)
			result, err := db.GetServerByNameAndVersion(ctx, nil, serverJSON.Name, serverJSON.Version, true)
			require.NoError(t, err)
			assert.Equal(t, status, result.Meta.Official.Status)
		}
	})
}

func TestPostgreSQL_StatusFieldsInListOperations(t *testing.T) {
	db := database.NewTestDB(t)
	ctx := context.Background()
	timeNow := time.Now()

	// Create test servers with different statuses and status fields
	testServers := []struct {
		name          string
		status        model.Status
		statusMessage *string
	}{
		{
			name:          "com.example/active-server",
			status:        model.StatusActive,
			statusMessage: nil,
		},
		{
			name:          "com.example/deprecated-server",
			status:        model.StatusDeprecated,
			statusMessage: stringPtr("Deprecated in favor of v2"),
		},
		{
			name:          "com.example/deleted-server",
			status:        model.StatusDeleted,
			statusMessage: stringPtr("Security vulnerability found"),
		},
	}

	// Create all test servers
	for _, server := range testServers {
		serverJSON := &apiv0.ServerJSON{
			Name:        server.name,
			Description: "Test server for list operations",
			Version:     "1.0.0",
		}

		officialMeta := &apiv0.RegistryExtensions{
			Status:          server.status,
			StatusChangedAt: timeNow,
			StatusMessage:   server.statusMessage,
			PublishedAt:     timeNow,
			UpdatedAt:       timeNow,
			IsLatest:        true,
		}

		_, err := db.CreateServer(ctx, nil, serverJSON, officialMeta)
		require.NoError(t, err)
	}

	t.Run("ListServers includes new status fields", func(t *testing.T) {
		results, _, err := db.ListServers(ctx, nil, nil, "", 10)
		require.NoError(t, err)

		// Find our test servers in the results
		foundServers := make(map[string]*apiv0.ServerResponse)
		for _, result := range results {
			for _, testServer := range testServers {
				if result.Server.Name == testServer.name {
					foundServers[testServer.name] = result
				}
			}
		}

		// Verify all test servers were found with correct status fields
		for _, testServer := range testServers {
			result, found := foundServers[testServer.name]
			assert.True(t, found, "Server %s should be found in list results", testServer.name)
			if !found {
				continue
			}

			assert.NotNil(t, result.Meta.Official)
			assert.Equal(t, testServer.status, result.Meta.Official.Status)
			assert.Equal(t, timeNow.Unix(), result.Meta.Official.StatusChangedAt.Unix())

			if testServer.statusMessage != nil {
				assert.NotNil(t, result.Meta.Official.StatusMessage)
				assert.Equal(t, *testServer.statusMessage, *result.Meta.Official.StatusMessage)
			} else {
				assert.Nil(t, result.Meta.Official.StatusMessage)
			}
		}
	})

	t.Run("GetServerByName includes new status fields", func(t *testing.T) {
		for _, testServer := range testServers {
			// Use includeDeleted=true since testServers includes deleted status
			result, err := db.GetServerByName(ctx, nil, testServer.name, true)
			require.NoError(t, err)

			assert.NotNil(t, result.Meta.Official)
			assert.Equal(t, testServer.status, result.Meta.Official.Status)
			assert.Equal(t, timeNow.Unix(), result.Meta.Official.StatusChangedAt.Unix())

			if testServer.statusMessage != nil {
				assert.NotNil(t, result.Meta.Official.StatusMessage)
				assert.Equal(t, *testServer.statusMessage, *result.Meta.Official.StatusMessage)
			} else {
				assert.Nil(t, result.Meta.Official.StatusMessage)
			}
		}
	})
}

func TestPostgreSQL_SetAllVersionsStatus(t *testing.T) {
	db := database.NewTestDB(t)
	ctx := context.Background()
	timeNow := time.Now()

	t.Run("update all versions status successfully", func(t *testing.T) {
		serverName := "com.example/all-versions-status-test"
		versions := []string{"1.0.0", "1.1.0", "2.0.0"}

		// Create multiple versions
		for i, version := range versions {
			serverJSON := &apiv0.ServerJSON{
				Name:        serverName,
				Description: "Test server for all-versions status update",
				Version:     version,
			}
			officialMeta := &apiv0.RegistryExtensions{
				Status:          model.StatusActive,
				StatusChangedAt: timeNow,
				PublishedAt:     timeNow,
				UpdatedAt:       timeNow,
				IsLatest:        i == len(versions)-1,
			}

			_, err := db.CreateServer(ctx, nil, serverJSON, officialMeta)
			require.NoError(t, err)
		}

		// Update all versions to deprecated
		statusMessage := "All versions deprecated"

		results, err := db.SetAllVersionsStatus(ctx, nil, serverName, model.StatusDeprecated, &statusMessage)
		assert.NoError(t, err)
		assert.Len(t, results, 3)

		// Verify all versions were updated
		for _, result := range results {
			assert.Equal(t, model.StatusDeprecated, result.Meta.Official.Status)
			assert.NotNil(t, result.Meta.Official.StatusMessage)
			assert.Equal(t, statusMessage, *result.Meta.Official.StatusMessage)
		}

		// Verify by fetching each version individually
		for _, version := range versions {
			server, err := db.GetServerByNameAndVersion(ctx, nil, serverName, version, false)
			require.NoError(t, err)
			assert.Equal(t, model.StatusDeprecated, server.Meta.Official.Status)
			assert.NotNil(t, server.Meta.Official.StatusMessage)
			assert.Equal(t, statusMessage, *server.Meta.Official.StatusMessage)
		}
	})

	t.Run("update non-existent server returns error", func(t *testing.T) {
		results, err := db.SetAllVersionsStatus(ctx, nil, "com.example/non-existent-server", model.StatusDeprecated, nil)
		assert.Error(t, err)
		assert.ErrorIs(t, err, database.ErrNotFound)
		assert.Nil(t, results)
	})

	t.Run("update all versions to deleted", func(t *testing.T) {
		serverName := "com.example/all-versions-deleted-test"

		// Create multiple versions
		for i, version := range []string{"1.0.0", "1.1.0"} {
			serverJSON := &apiv0.ServerJSON{
				Name:        serverName,
				Description: "Test server for deleting",
				Version:     version,
			}
			officialMeta := &apiv0.RegistryExtensions{
				Status:          model.StatusActive,
				StatusChangedAt: timeNow,
				PublishedAt:     timeNow,
				UpdatedAt:       timeNow,
				IsLatest:        i == 1,
			}

			_, err := db.CreateServer(ctx, nil, serverJSON, officialMeta)
			require.NoError(t, err)
		}

		// Delete all versions
		statusMessage := "Critical security vulnerability"
		results, err := db.SetAllVersionsStatus(ctx, nil, serverName, model.StatusDeleted, &statusMessage)
		assert.NoError(t, err)
		assert.Len(t, results, 2)

		// Verify all versions are deleted
		for _, result := range results {
			assert.Equal(t, model.StatusDeleted, result.Meta.Official.Status)
			assert.NotNil(t, result.Meta.Official.StatusMessage)
			assert.Equal(t, statusMessage, *result.Meta.Official.StatusMessage)
		}
	})

	t.Run("transition from deleted back to active", func(t *testing.T) {
		serverName := "com.example/all-versions-reactivate-test"

		// Create server in deleted state
		for i, version := range []string{"1.0.0", "2.0.0"} {
			serverJSON := &apiv0.ServerJSON{
				Name:        serverName,
				Description: "Test server for reactivation",
				Version:     version,
			}
			officialMeta := &apiv0.RegistryExtensions{
				Status:          model.StatusDeleted,
				StatusChangedAt: timeNow,
				PublishedAt:     timeNow,
				UpdatedAt:       timeNow,
				IsLatest:        i == 1,
			}

			_, err := db.CreateServer(ctx, nil, serverJSON, officialMeta)
			require.NoError(t, err)
		}

		// Reactivate all versions
		results, err := db.SetAllVersionsStatus(ctx, nil, serverName, model.StatusActive, nil)
		assert.NoError(t, err)
		assert.Len(t, results, 2)

		// Verify all versions are active and metadata is cleared
		for _, result := range results {
			assert.Equal(t, model.StatusActive, result.Meta.Official.Status)
			assert.Nil(t, result.Meta.Official.StatusMessage)
		}
	})
}

func TestPostgreSQL_IncludeDeletedFilter(t *testing.T) {
	db := database.NewTestDB(t)
	ctx := context.Background()
	timeNow := time.Now()

	// Create test servers with different statuses
	testServers := []struct {
		name    string
		version string
		status  model.Status
	}{
		{
			name:    "com.example/deleted-filter-active",
			version: "1.0.0",
			status:  model.StatusActive,
		},
		{
			name:    "com.example/deleted-filter-deprecated",
			version: "1.0.0",
			status:  model.StatusDeprecated,
		},
		{
			name:    "com.example/deleted-filter-deleted",
			version: "1.0.0",
			status:  model.StatusDeleted,
		},
	}

	// Create all test servers
	for _, server := range testServers {
		serverJSON := &apiv0.ServerJSON{
			Name:        server.name,
			Description: "Test server for include deleted filter",
			Version:     server.version,
		}
		officialMeta := &apiv0.RegistryExtensions{
			Status:          server.status,
			StatusChangedAt: timeNow,
			PublishedAt:     timeNow,
			UpdatedAt:       timeNow,
			IsLatest:        true,
		}

		_, err := db.CreateServer(ctx, nil, serverJSON, officialMeta)
		require.NoError(t, err)
	}

	t.Run("excludes deleted by default (nil IncludeDeleted)", func(t *testing.T) {
		filter := &database.ServerFilter{
			SubstringName: stringPtr("deleted-filter"),
		}

		results, _, err := db.ListServers(ctx, nil, filter, "", 10)
		require.NoError(t, err)

		// Should only get active and deprecated servers
		assert.Len(t, results, 2)

		for _, result := range results {
			assert.NotEqual(t, model.StatusDeleted, result.Meta.Official.Status,
				"Deleted servers should be excluded by default")
		}

		// Verify we got the expected servers
		names := make([]string, len(results))
		for i, r := range results {
			names[i] = r.Server.Name
		}
		assert.Contains(t, names, "com.example/deleted-filter-active")
		assert.Contains(t, names, "com.example/deleted-filter-deprecated")
	})

	t.Run("excludes deleted when IncludeDeleted is false", func(t *testing.T) {
		filter := &database.ServerFilter{
			SubstringName:  stringPtr("deleted-filter"),
			IncludeDeleted: boolPtr(false),
		}

		results, _, err := db.ListServers(ctx, nil, filter, "", 10)
		require.NoError(t, err)

		// Should only get active and deprecated servers
		assert.Len(t, results, 2)

		for _, result := range results {
			assert.NotEqual(t, model.StatusDeleted, result.Meta.Official.Status,
				"Deleted servers should be excluded when IncludeDeleted is false")
		}
	})

	t.Run("includes deleted when IncludeDeleted is true", func(t *testing.T) {
		filter := &database.ServerFilter{
			SubstringName:  stringPtr("deleted-filter"),
			IncludeDeleted: boolPtr(true),
		}

		results, _, err := db.ListServers(ctx, nil, filter, "", 10)
		require.NoError(t, err)

		// Should get all servers including deleted
		assert.Len(t, results, 3)

		// Verify we got all statuses
		statuses := make(map[model.Status]bool)
		for _, result := range results {
			statuses[result.Meta.Official.Status] = true
		}

		assert.True(t, statuses[model.StatusActive], "Should include active servers")
		assert.True(t, statuses[model.StatusDeprecated], "Should include deprecated servers")
		assert.True(t, statuses[model.StatusDeleted], "Should include deleted servers")
	})

	t.Run("combined filters with include deleted", func(t *testing.T) {
		// Test that IncludeDeleted works correctly with other filters
		filter := &database.ServerFilter{
			SubstringName:  stringPtr("deleted-filter"),
			Version:        stringPtr("1.0.0"),
			IsLatest:       boolPtr(true),
			IncludeDeleted: boolPtr(true),
		}

		results, _, err := db.ListServers(ctx, nil, filter, "", 10)
		require.NoError(t, err)

		// Should get all 3 servers (all match version and isLatest criteria)
		assert.Len(t, results, 3)
	})

	t.Run("multiple versions with deleted filtering", func(t *testing.T) {
		serverName := "com.example/multi-version-deleted-test"

		// Create server with multiple versions, one deleted
		versionsData := []struct {
			version string
			status  model.Status
		}{
			{"1.0.0", model.StatusDeleted}, // Old version, deleted
			{"1.1.0", model.StatusActive},  // Current stable
			{"2.0.0", model.StatusActive},  // Latest
		}

		for i, v := range versionsData {
			serverJSON := &apiv0.ServerJSON{
				Name:        serverName,
				Description: "Multi-version server",
				Version:     v.version,
			}
			officialMeta := &apiv0.RegistryExtensions{
				Status:          v.status,
				StatusChangedAt: timeNow,
				PublishedAt:     timeNow,
				UpdatedAt:       timeNow,
				IsLatest:        i == len(versionsData)-1,
			}

			_, err := db.CreateServer(ctx, nil, serverJSON, officialMeta)
			require.NoError(t, err)
		}

		// Without IncludeDeleted - should get only active versions
		filter := &database.ServerFilter{
			Name:           stringPtr(serverName),
			IncludeDeleted: boolPtr(false),
		}
		results, _, err := db.ListServers(ctx, nil, filter, "", 10)
		require.NoError(t, err)
		assert.Len(t, results, 2, "Should only get non-deleted versions")

		// With IncludeDeleted - should get all versions
		filter.IncludeDeleted = boolPtr(true)
		results, _, err = db.ListServers(ctx, nil, filter, "", 10)
		require.NoError(t, err)
		assert.Len(t, results, 3, "Should get all versions including deleted")
	})
}

// Helper functions for creating pointers to basic types
func stringPtr(s string) *string {
	return &s
}

func boolPtr(b bool) *bool {
	return &b
}

func timePtr(t time.Time) *time.Time {
	return &t
}
