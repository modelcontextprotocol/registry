// This tool was created by Claude Code as a simple way to kick the tires on data migrations
// by loading production data into a test database for migration testing.
// It is not intended for production use.
//
//go:build ignore

package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"path/filepath"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

func main() {
	// Database connection
	// WARNING: This connection string contains a hardcoded password for testing only.
	// This tool is not intended for production use (see //go:build ignore directive).
	db, err := sql.Open("postgres", "postgres://postgres:testpass@localhost:5433/registry_test?sslmode=disable") //nolint:gosec // test tool only
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer db.Close()

	// Run migrations up to a specific point (configure as needed)
	maxMigration := 7 // Change this to test different migration states
	fmt.Printf("Running migrations 001-%03d...\n", maxMigration)
	migrationsDir := "internal/database/migrations"
	for i := 1; i <= maxMigration; i++ {
		migrationFile := filepath.Join(migrationsDir, fmt.Sprintf("%03d_*.sql", i))
		files, err := filepath.Glob(migrationFile)
		if err != nil || len(files) == 0 {
			log.Fatalf("Migration file %d not found", i)
		}

		// Security Note: This tool reads SQL migration files from the controlled migrations directory
		// and executes them. This is safe because:
		// 1. This is a test-only tool (//go:build ignore prevents production builds)
		// 2. Migration files are part of the codebase and controlled by developers
		// 3. The migrations directory path is hardcoded and validated
		// This is not a SQL injection vulnerability as no user input is involved.
		content, err := ioutil.ReadFile(files[0]) //nolint:gosec // reading trusted migration files
		if err != nil {
			log.Fatalf("Failed to read migration %d: %v", i, err)
		}

		fmt.Printf("  Applying migration %d: %s\n", i, filepath.Base(files[0]))
		// Executing SQL from trusted migration files - not user input
		if _, err := db.Exec(string(content)); err != nil { //nolint:gosec // controlled migration files only
			log.Fatalf("Failed to apply migration %d: %v", i, err)
		}
	}

	// Load production data
	fmt.Println("\nLoading production data...")
	data, err := ioutil.ReadFile("scripts/mirror_data/production_servers.json")
	if err != nil {
		log.Fatal("Failed to read production data:", err)
	}

	var prodData struct {
		Servers []json.RawMessage `json:"servers"`
	}
	if err := json.Unmarshal(data, &prodData); err != nil {
		log.Fatal("Failed to parse production data:", err)
	}

	fmt.Printf("Loading %d servers...\n", len(prodData.Servers))

	// Prepare insert statement
	stmt, err := db.Prepare("INSERT INTO servers (version_id, value) VALUES ($1, $2)")
	if err != nil {
		log.Fatal("Failed to prepare statement:", err)
	}
	defer stmt.Close()

	// Insert each server
	for i, server := range prodData.Servers {
		// Generate a unique version_id
		versionID := uuid.New().String()

		// The server data is already JSON, just insert it
		if _, err := stmt.Exec(versionID, server); err != nil {
			log.Printf("Failed to insert server %d: %v", i, err)
			continue
		}
	}

	fmt.Println("Data loaded successfully!")

	// Verify the data
	var count int
	db.QueryRow("SELECT COUNT(*) FROM servers").Scan(&count)
	fmt.Printf("\nTotal servers in database: %d\n", count)

	// Check for NULL status values in the JSON
	fmt.Println("\nAnalyzing status field in JSON data...")

	rows, err := db.Query(`
		SELECT
			COUNT(*) as total,
			COUNT(CASE WHEN value->>'status' IS NULL THEN 1 END) as null_status,
			COUNT(CASE WHEN value->>'status' = '' THEN 1 END) as empty_status,
			COUNT(CASE WHEN value->>'status' = 'null' THEN 1 END) as string_null_status,
			COUNT(CASE WHEN value->>'status' = 'active' THEN 1 END) as active_status,
			COUNT(CASE WHEN value->>'status' = 'deprecated' THEN 1 END) as deprecated_status,
			COUNT(CASE WHEN value->>'status' = 'deleted' THEN 1 END) as deleted_status
		FROM servers
	`)
	if err != nil {
		log.Fatal("Failed to analyze data:", err)
	}
	defer rows.Close()

	if rows.Next() {
		var total, nullStatus, emptyStatus, stringNullStatus, activeStatus, deprecatedStatus, deletedStatus int
		rows.Scan(&total, &nullStatus, &emptyStatus, &stringNullStatus, &activeStatus, &deprecatedStatus, &deletedStatus)

		fmt.Printf("  Total servers: %d\n", total)
		fmt.Printf("  NULL status: %d\n", nullStatus)
		fmt.Printf("  Empty status: %d\n", emptyStatus)
		fmt.Printf("  'null' string status: %d\n", stringNullStatus)
		fmt.Printf("  'active' status: %d\n", activeStatus)
		fmt.Printf("  'deprecated' status: %d\n", deprecatedStatus)
		fmt.Printf("  'deleted' status: %d\n", deletedStatus)
		fmt.Printf("  Other/Invalid: %d\n", total-nullStatus-emptyStatus-stringNullStatus-activeStatus-deprecatedStatus-deletedStatus)
	}

	// Print sample servers with no status
	fmt.Println("\nSample servers with NULL status:")
	rows, err = db.Query(`
		SELECT value->>'name', value->>'version'
		FROM servers
		WHERE value->>'status' IS NULL
		LIMIT 5
	`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var name, version string
			rows.Scan(&name, &version)
			fmt.Printf("  - %s@%s\n", name, version)
		}
	}

	fmt.Printf("\nDatabase is ready for testing migration %03d!\n", maxMigration+1)
}
