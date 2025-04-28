#!/bin/bash

# Build script for MCP Registry Publisher Tool

# Set variables
OUTPUT_DIR="./bin"
BINARY_NAME="mcp-publisher"

# Create output directory if it doesn't exist
mkdir -p $OUTPUT_DIR

# Print build information
echo "Building MCP Registry Publisher Tool..."

# Build for current platform
echo "Building for $(go env GOOS)/$(go env GOARCH)..."
go build -o "$OUTPUT_DIR/$BINARY_NAME" .

# Make the binary executable
chmod +x "$OUTPUT_DIR/$BINARY_NAME"

echo "Build complete: $OUTPUT_DIR/$BINARY_NAME"

# Optional: Build for multiple platforms
if [ "$1" == "--all" ]; then
    echo "Building for all supported platforms..."
    
    # Linux AMD64
    GOOS=linux GOARCH=amd64 go build -o "$OUTPUT_DIR/${BINARY_NAME}-linux-amd64" .
    
    # MacOS AMD64
    GOOS=darwin GOARCH=amd64 go build -o "$OUTPUT_DIR/${BINARY_NAME}-darwin-amd64" .
    
    # MacOS ARM64 (Apple Silicon)
    GOOS=darwin GOARCH=arm64 go build -o "$OUTPUT_DIR/${BINARY_NAME}-darwin-arm64" .
    
    # Windows AMD64
    GOOS=windows GOARCH=amd64 go build -o "$OUTPUT_DIR/${BINARY_NAME}-windows-amd64.exe" .
    
    echo "Multi-platform build complete in $OUTPUT_DIR/"
fi
