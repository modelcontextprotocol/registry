# MCP Registry Importer Tool

This command-line tool imports server data from a JSON file into a MongoDB database for the Model Context Protocol Registry.

## Usage

```bash
go run main.go [options]
```

### Options

- `-uri string`: MongoDB connection URI (default "mongodb://localhost:27017")
- `-db string`: MongoDB database name (default "mcp_registry")
- `-collection string`: MongoDB collection name (default "servers")
- `-seed string`: Path to seed.json file (default: looks for data/seed.json relative to current directory)
- `-drop`: Drop collection before importing (default false)

### Examples

Import data using default settings:
```bash
go run main.go
```

Import data into a specific MongoDB instance:
```bash
go run main.go -uri mongodb://username:password@example.com:27017 -db registry
```

Import data from a specific seed file and replace all existing data:
```bash
go run main.go -seed /path/to/custom-seed.json -drop
```

## Building

To build the tool as an executable:

```bash
go build -o registry-importer
```

Then you can run it directly:

```bash
./registry-importer [options]
```
