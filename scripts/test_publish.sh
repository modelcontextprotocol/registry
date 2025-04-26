#!/bin/bash

set -e 

# Default values
HOST="http://localhost:8080"
VERBOSE=false

# Display usage information
function show_usage {
  echo "Usage: $0 [options]"
  echo "Options:"
  echo "  -h, --host      Base URL of the MCP Registry service (default: http://localhost:8080)"
  echo "  -v, --verbose   Show verbose output including full request payload"
  echo "  --help          Show this help message"
  exit 1
}

# Check if jq is installed
if ! command -v jq &> /dev/null; then
  echo "Error: jq is required but not installed."
  echo "Please install jq using your package manager, for example:"
  echo "  brew install jq (macOS)"
  echo "  apt-get install jq (Debian/Ubuntu)"
  echo "  yum install jq (CentOS/RHEL)"
  exit 1
fi

# Check if the API is running
echo "Checking if the API is running at $HOST..."
health_check=$(curl -s -o /dev/null -w "%{http_code}" "$HOST/v0/health" 2>/dev/null)
if [[ "$health_check" != "200" ]]; then
  echo "Warning: API might not be running at $HOST (health check returned $health_check)"
  echo "Do you want to continue anyway? (y/n)"
  read -r proceed
  if [[ ! "$proceed" =~ ^[Yy]$ ]]; then
    echo "Exiting. Please start the API and try again."
    exit 1
  fi
  echo "Continuing as requested..."
else
  echo "API is running at $HOST"
fi

# Parse command line arguments
while [[ "$#" -gt 0 ]]; do
  case $1 in
    -h|--host) HOST="$2"; shift ;;
    -v|--verbose) VERBOSE=true ;;
    --help) show_usage ;;
    *) echo "Unknown parameter: $1"; show_usage ;;
  esac
  shift
done

# Create a temporary file for our JSON payload
PAYLOAD_FILE=$(mktemp)

# Create sample server detail payload
cat > "$PAYLOAD_FILE" << EOF
{
  "name": "Test MCP Server",
  "description": "A test server for MCP Registry",
  "version_detail": {
    "version": "1.0.2",
    "release_date": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
    "is_latest": true
  },
  "repository": {
    "url": "https://github.com/example/test-mcp-server",
    "branch": "main"
  },
  "registry_canonical": "test-mcp-server",
  "registries": [
    {
      "name": "npm",
      "package_name": "test-mcp-server",
      "license": "MIT",
      "command_arguments": {
        "sub_commands": [
          {
            "name": "start",
            "description": "Start the server"
          }
        ],
        "environment_variables": [
          {
            "name": "PORT",
            "description": "Port to run the server on",
            "required": false
          }
        ]
      }
    }
  ],
  "remotes": [
    {
      "transport_type": "http",
      "url": "http://example.com/api"
    }
  ]
}
EOF

# Show the payload if verbose mode is enabled
if $VERBOSE; then
  echo "Request Payload:"
  cat "$PAYLOAD_FILE" | jq '.'
  echo "-------------------------------------"
fi

# Test publish endpoint
echo "Testing publish endpoint: $HOST/v0/publish"
# Get response and status code in a single request
response_file=$(mktemp)
headers_file=$(mktemp)

# Execute curl with response body to file and headers+status to another file
curl -s -X POST -H "Content-Type: application/json" \
  -d "@$PAYLOAD_FILE" \
  -D "$headers_file" \
  -o "$response_file" \
  "$HOST/v0/publish"

# Read the response body
http_response=$(<"$response_file")

# Extract the status code from the headers file
status_code=$(head -n1 "$headers_file" | awk '{print $2}')

# Clean up temp files
rm "$response_file" "$headers_file"

echo "Status Code: $status_code"

# Check for status code in 2xx range (200, 201, 202, etc)
if [[ "${status_code:0:1}" == "2" ]]; then
  # Parse JSON response with jq
  echo "Response:"
  echo "$http_response" | jq '.' 2>/dev/null || echo "$http_response"
  
  # Extract the server ID from the response
  server_id=$(echo "$http_response" | jq -r '.id')
  
  echo "Publish successful with ID: $server_id"
  
  # If we got a valid ID, verify it was actually created by calling the servers endpoint
  if [[ ! -z "$server_id" && "$server_id" != "null" ]]; then
    echo "-------------------------------------"
    echo "Verifying server was published by checking servers endpoint..."
    verify_response=$(curl -s "$HOST/v0/servers/$server_id")
    echo "Response from servers endpoint:"
    echo "$verify_response" | jq '.' 2>/dev/null || echo "$verify_response"
    echo "-------------------------------------"
    echo "Server verification response:"
    echo "$verify_response" | jq '.' 2>/dev/null || echo "$verify_response"
    echo "Server verification successful"
  else
    echo "Error: No valid server ID returned from publish response"
    echo "Response:"
    echo "$http_response" | jq '.' 2>/dev/null || echo "$http_response"
    exit 1
  fi
  
else
  echo "Response:"
  echo "$http_response" | jq '.' 2>/dev/null || echo "$http_response"
  echo "Publish failed"
fi

echo "-------------------------------------"

# Clean up temp file
rm "$PAYLOAD_FILE"
