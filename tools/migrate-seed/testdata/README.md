# Migration Test Data

This directory contains golden files for testing the seed migration CLI tool.

## Files

- **`legacy-input.json`** - Sample input file in the legacy seed format (flat structure with registry metadata mixed in)
- **`expected-output.json`** - Expected output after migration to extension wrapper format

## Usage

The golden file test (`TestMigrationCLI_GoldenFile`) runs the migration tool on `legacy-input.json` and compares the output against `expected-output.json` to ensure the migration produces the correct result.

## Updating Golden Files

If you make changes to the migration logic, you may need to update the expected output:

1. Review the changes to ensure they're correct
2. Regenerate the expected output:
   ```bash
   cd cmd/migrate-seed
   ../../bin/migrate-seed testdata/legacy-input.json testdata/expected-output.json
   ```
3. Verify the new output is correct by reviewing the diff
4. Run the tests to ensure they pass

## Format Details

### Legacy Input Format
- Flat JSON array of server objects
- Registry metadata fields (`id`, `is_latest`, `release_date`) mixed directly in server objects
- Version detail contains registry metadata

### Expected Output Format  
- Extension wrapper format
- Each server wrapped in `"server"` field
- Registry metadata in `"x-io.modelcontextprotocol.registry"` extension
- No `"x-publisher"` extensions for seed data