#!/bin/bash

set -e 

echo "=================================================="
echo "MCP Registry Publish Endpoint Test Script"
echo "=================================================="
echo "This script expects the MCP Registry server to be running locally."
echo "Please ensure the server is started using one of the following methods:"
echo "  • Docker Compose: docker compose up"
echo "  • Direct execution: go run ./cmd/registry"
echo "  • Built binary: ./build/registry"
echo ""
echo "REQUIRED: Set the BEARER_TOKEN environment variable with a valid GitHub token"
echo "Example: export BEARER_TOKEN=your_github_token_here"
echo "=================================================="
echo ""

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
  echo ""
  echo "Environment Variables:"
  echo "  BEARER_TOKEN    Required: GitHub token for authentication"
  exit 1
}

# Check if bearer token is set
if [[ -z "$BEARER_TOKEN" ]]; then
  echo "Error: BEARER_TOKEN environment variable is not set."
  echo "Please set your GitHub token as an environment variable:"
  echo "  export BEARER_TOKEN=your_github_token_here"
  exit 1
fi

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
echo "Checking if the MCP Registry API is running at $HOST..."
health_check=$(curl -s -o /dev/null -w "%{http_code}" "$HOST/v0/health" 2>/dev/null)
if [[ "$health_check" != "200" ]]; then
  echo "Error: MCP Registry API is not running at $HOST (health check returned $health_check)"
  echo "Please start the server using one of the methods mentioned above and try again."
  exit 1
else
  echo "✓ MCP Registry API is running at $HOST"
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

# Create sample server detail payload based on current model structure
cat > "$PAYLOAD_FILE" << EOF
{
  "name": "io.github.example/test-mcp-server",
  "description": "A test server for MCP Registry validation - published at $(date)",
  "repository": {
    "url": "https://github.com/example/test-mcp-server",
    "source": "github",
    "id": "example/test-mcp-server"
  },
  "version": "1.0.$(date +%s)",
  "packages": [
    {
      "registryType": "npm",
      "registryBaseUrl": "https://registry.npmjs.org",
      "identifier": "test-mcp-server",
      "version": "1.0.$(date +%s)",
      "runtimeHint": "node",
      "transport": {
        "type": "stdio"
      },
      "runtimeArguments": [
        {
          "type": "positional",
          "name": "config",
          "description": "Configuration file path",
          "format": "filepath",
          "isRequired": false,
          "default": "./config.json"
        }
      ],
      "environmentVariables": [
        {
          "name": "PORT",
          "description": "Port to run the server on",
          "format": "number",
          "isRequired": false,
          "default": "3000"
        },
        {
          "name": "API_KEY",
          "description": "API key for external service",
          "format": "string",
          "isRequired": true,
          "isSecret": true
        }
      ]
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
echo "Using Bearer Token: ${BEARER_TOKEN:0:10}..." # Show only first 10 chars for security

# Get response and status code in a single request
response_file=$(mktemp)
headers_file=$(mktemp)

# Execute curl with response body to file and headers+status to another file
curl -s -X POST \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${BEARER_TOKEN}" \
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
  
  # Check for server added message and extract server name
  message=$(echo "$http_response" | jq -r '.message // empty' 2>/dev/null)
  server_name=$(echo "$http_response" | jq -r '.server.name // .name // empty' 2>/dev/null)
  
  # Validate the response contains success indicators
  success_indicators=0
  
  if [[ ! -z "$message" && "$message" != "null" ]]; then
    echo "✓ Success message received: $message"
    if [[ "$message" == *"server"* && ("$message" == *"added"* || "$message" == *"published"* || "$message" == *"created"*) ]]; then
      ((success_indicators++))
      echo "✓ Message indicates server was successfully added"
    fi
  fi
  
  if [[ ! -z "$server_name" && "$server_name" != "null" && "$server_name" != "empty" ]]; then
    echo "✓ Server name received: $server_name"
    # Validate server name format (should contain a namespace like 'domain/name')
    if [[ "$server_name" == *"/"* ]]; then
      ((success_indicators++))
      echo "✓ Server name appears to be a valid namespaced format"
    else
      echo "⚠ Server name format may not contain namespace: $server_name"
      ((success_indicators++)) # Still count as success if we got a name
    fi
  fi
  
  if [[ $success_indicators -ge 2 ]]; then
    echo ""
    echo "🎉 PUBLISH TEST PASSED!"
    echo "   ✓ Server successfully published with name: $server_name"
    echo "   ✓ Success message: $message"
  else
    echo ""
    echo "❌ PUBLISH TEST FAILED!"
    echo "   Expected: Success message about server being added AND a server name"
    echo "   Received: message='$message', name='$server_name'"
    exit 1
  fi
  
else
  echo ""
  echo "❌ PUBLISH TEST FAILED!"
  echo "   Expected: 2xx status code"
  echo "   Received: $status_code"
  echo "   Response:"
  echo "$http_response" | jq '.' 2>/dev/null || echo "$http_response"
  exit 1
fi

echo "-------------------------------------"

# Clean up temp file
rm "$PAYLOAD_FILE"
