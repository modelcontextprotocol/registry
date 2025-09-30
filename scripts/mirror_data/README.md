# Mirror Production Data

> **Note:** These tools were created by Claude Code as a simple way to kick the tires on data migrations.
> They are not intended for production use.

Tools to fetch and load production registry data for local testing and migration debugging.

## Overview

These scripts help you:
1. Fetch all server data from the production registry API
2. Load it into a local PostgreSQL database
3. Test migrations against real production data

## Prerequisites

- Go 1.24.x
- PostgreSQL (via Docker or local installation)
- Required Go packages:
  ```bash
  go get github.com/google/uuid github.com/lib/pq
  ```

## Usage

### 1. Fetch Production Data

```bash
# From the project root directory
go run scripts/mirror_data/fetch_production_data.go
```

This will:
- Fetch all servers from https://registry.modelcontextprotocol.io/v0/servers
- Handle pagination automatically
- Save data to `scripts/mirror_data/production_servers.json`
- Be respectful to the API with rate limiting

Output: `production_servers.json` containing all server records

### 2. Set Up Test Database

Start a PostgreSQL container for testing:

```bash
docker run -d --name test-postgres \
  -p 5433:5432 \
  -e POSTGRES_PASSWORD=testpass \
  -e POSTGRES_DB=registry_test \
  postgres:16
```

Or reset an existing database:

```bash
docker exec test-postgres psql -U postgres postgres -c "DROP DATABASE IF EXISTS registry_test;"
docker exec test-postgres psql -U postgres postgres -c "CREATE DATABASE registry_test;"
```

### 3. Load Production Data

```bash
# From the project root directory
go run scripts/mirror_data/load_production_data.go
```

This will:
1. Connect to PostgreSQL at `localhost:5433`
2. Apply migrations up to a configurable point (default: migration 007)
3. Load all production server data
4. Analyze the data and report statistics
5. Show sample servers with NULL status values

To test a different migration, edit `maxMigration` in the script (line 24)

### 4. Test Migrations

After loading the data, you can test migrations against real production data.

#### Testing a Single Migration

```bash
# Test migration 008
cat internal/database/migrations/008_separate_official_metadata.sql | \
  docker exec -i test-postgres psql -U postgres -d registry_test
```

#### Debugging Failed Migrations

1. **Check the error output:**
```bash
# Run migration and capture all output
cat internal/database/migrations/008_separate_official_metadata.sql | \
  docker exec -i test-postgres psql -U postgres -d registry_test 2>&1 | \
  grep -E "(ERROR|NOTICE|WARNING)"
```

2. **Inspect data before migration:**
```bash
# Check for NULL values that might cause issues
docker exec test-postgres psql -U postgres -d registry_test -c \
  "SELECT COUNT(*) FROM servers WHERE value->>'status' IS NULL;"

# Find sample problematic records
docker exec test-postgres psql -U postgres -d registry_test -c \
  "SELECT value->>'name', value->>'version' FROM servers \
   WHERE value->>'status' IS NULL LIMIT 5;"
```

3. **Test partial migrations:**
```bash
# Extract and run only part of a migration to isolate issues
# For example, run only up to the NOT NULL constraint:
docker exec test-postgres psql -U postgres -d registry_test <<EOF
BEGIN;
-- Paste migration SQL up to the problematic line
-- Check intermediate state
SELECT COUNT(*) FROM servers WHERE status IS NULL;
ROLLBACK;  -- or COMMIT if testing further
EOF
```

#### Testing Migration Rollback

```bash
# Start a transaction to test and rollback
docker exec test-postgres psql -U postgres -d registry_test <<EOF
BEGIN;
\i /dev/stdin < internal/database/migrations/008_separate_official_metadata.sql
-- Inspect the results
\d servers
SELECT COUNT(*) FROM servers;
ROLLBACK;  -- Undo all changes
EOF
```

#### Iterative Testing Workflow

1. **Reset database to clean state:**
```bash
docker exec test-postgres psql -U postgres postgres -c "DROP DATABASE IF EXISTS registry_test;"
docker exec test-postgres psql -U postgres postgres -c "CREATE DATABASE registry_test;"
go run scripts/mirror_data/load_production_data.go
```

2. **Test your migration changes:**
```bash
cat internal/database/migrations/008_separate_official_metadata.sql | \
  docker exec -i test-postgres psql -U postgres -d registry_test
```

3. **Verify the results:**
```bash
# Check final table structure
docker exec test-postgres psql -U postgres -d registry_test -c "\d servers"

# Verify data integrity
docker exec test-postgres psql -U postgres -d registry_test -c \
  "SELECT COUNT(*), COUNT(DISTINCT server_name) FROM servers;"
```

#### Common Migration Issues to Check

- **NULL values:** Check columns before adding NOT NULL constraints
- **Unique constraints:** Verify no duplicates exist before adding
- **Check constraints:** Validate all existing data matches the constraint
- **Data type changes:** Ensure all values can be cast to new type
- **Foreign keys:** Verify referential integrity before adding

### 5. Clean Up

```bash
docker stop test-postgres
docker rm test-postgres
```

## Configuration

The `load_production_data.go` script connects to:
- Host: `localhost`
- Port: `5433`
- Database: `registry_test`
- User: `postgres`
- Password: `testpass`

Modify line 17 in the script if you need different connection parameters.

## Example Output

When running `load_production_data.go`:

```
Running migrations 001-007...
  Applying migration 1: 001_initial_schema.sql
  Applying migration 2: 002_add_server_extensions.sql
  ...

Loading production data...
Loading 779 servers...
Data loaded successfully!

Total servers in database: 779

Analyzing status field in JSON data...
  Total servers: 779
  NULL status: 126
  Empty status: 0
  'null' string status: 0
  'active' status: 652
  'deprecated' status: 0
  'deleted' status: 0
  Other/Invalid: 1

Sample servers with NULL status:
  - com.biodnd/agent-ip@0.1.2
  - io.github.jkakar/cookwith-mcp@1.0.0
  ...

Database is ready for testing migration 008!
```

## Troubleshooting

### Port already in use
If port 5433 is already in use, either:
- Stop the existing service using that port
- Use a different port in both the Docker command and the Go script

### Connection refused
Ensure the PostgreSQL container is running and healthy:
```bash
docker ps | grep test-postgres
docker logs test-postgres
```

### Migration failures
The script stops at migration 007 intentionally. To test migration 008, run it manually after the data is loaded.