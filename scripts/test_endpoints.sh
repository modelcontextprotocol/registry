#!/bin/bash

set -e 

echo "=================================================="
echo "MCP Registry Endpoint Test Script"
echo "=================================================="
echo "This script expects the MCP Registry server to be running locally."
echo "Please ensure the server is started using one of the following methods:"
echo "  • Docker Compose: docker compose up"
echo "  • Direct execution: go run ./cmd/registry"
echo "  • Built binary: ./build/registry"
echo "=================================================="
echo ""

# Default values
HOST="http://localhost:8080"
ENDPOINT="all"
LIMIT=""

# Display usage information
function show_usage {
  echo "Usage: $0 [options]"
  echo "Options:"
  echo "  -h, --host      Base URL of the MCP Registry service (default: http://localhost:8080)"
  echo "  -e, --endpoint  Endpoint to test: health, servers, ping, all (default: all)"
  echo "  -l, --limit     Test servers with specified limit parameter"
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

# Parse command line arguments
while [[ "$#" -gt 0 ]]; do
  case $1 in
    -h|--host) HOST="$2"; shift ;;
    -e|--endpoint) ENDPOINT="$2"; shift ;;
    -l|--limit) LIMIT="$2"; shift ;;
    --help) show_usage ;;
    *) echo "Unknown parameter: $1"; show_usage ;;
  esac
  shift
done

# Validate endpoint
if [[ "$ENDPOINT" != "health" && "$ENDPOINT" != "servers" && "$ENDPOINT" != "ping" && "$ENDPOINT" != "all" ]]; then
  echo "Invalid endpoint: $ENDPOINT. Must be 'health', 'servers', 'ping', or 'all'."
  exit 1
fi

# Test health endpoint
test_health() {
  echo "Testing health endpoint: $HOST/v0/health"
  
  # Get response and status code
  http_response=$(curl -s "$HOST/v0/health")
  status_code=$(curl -s -o /dev/null -w "%{http_code}" "$HOST/v0/health")
  
  echo "Status Code: $status_code"
  
  if [[ $status_code == 2* ]]; then
    # Parse JSON response with jq
    echo "Response:"
    echo "$http_response" | jq '.'
    echo "Health check successful"
    echo "-------------------------------------"
    return 0
  else
    echo "Response:"
    echo "$http_response" | jq '.' 2>/dev/null || echo "$http_response"
    echo "Health check failed"
    echo "-------------------------------------"
    return 1
  fi
}

# Test servers endpoint
test_servers() {
  echo "Testing servers endpoint: $HOST/v0/servers"
  
  # Get response and status code
  http_response=$(curl -s "$HOST/v0/servers")
  status_code=$(curl -s -o /dev/null -w "%{http_code}" "$HOST/v0/servers")
  
  echo "Status Code: $status_code"
  
  if [[ $status_code == 2* ]]; then
    # Parse and display JSON with jq
    echo "Response Summary:"
    echo "$http_response" | jq '.servers | length' | xargs echo "Total registries:"
    
    # Display a prettier formatted summary
    echo "Server Names:"
    echo "$http_response" | jq -r '.servers[].server.name'
    
    # Show the metadata with next cursor if available
    echo -e "\nPagination Metadata:"
    echo "$http_response" | jq '.metadata'
    
    # Show more detailed output with all fields
    echo -e "\nServer Details:"
    echo "$http_response" | jq '.'

    echo "Servers request successful"
    echo "-------------------------------------"
    return 0
  else
    echo "Response:"
    echo "$http_response" | jq '.' 2>/dev/null || echo "$http_response"
    echo "Servers request failed"
    echo "-------------------------------------"
    return 1
  fi
}

# Test servers endpoint with limit
test_servers_with_limit() {
  limit=$1
  echo "Testing servers endpoint with limit: $HOST/v0/servers?limit=$limit"
  
  # Get response and status code
  http_response=$(curl -s "$HOST/v0/servers?limit=$limit")
  status_code=$(curl -s -o /dev/null -w "%{http_code}" "$HOST/v0/servers?limit=$limit")
  
  echo "Status Code: $status_code"
  
  if [[ $status_code == 2* ]]; then
    # Verify the response contains the right number of items (or not more than the limit)
    item_count=$(echo "$http_response" | jq '.servers | length')
    echo "Response has $item_count items (requested limit: $limit)"

    # Verify we're not exceeding the limit
    if [[ $item_count -gt $limit ]]; then
      echo "ERROR: Response contains more items ($item_count) than the requested limit ($limit)"
      return 1
    fi

    # Parse and display JSON with jq
    echo "Response Summary:"

    # Display a prettier formatted summary
    echo "Server Names:"
    echo "$http_response" | jq -r '.servers[].server.name'
    
    # Show the metadata with next cursor if available
    echo -e "\nPagination Metadata:"
    echo "$http_response" | jq '.metadata'
    
    # Show more detailed output with all fields
    echo -e "\nServer Details:"
    echo "$http_response" | jq '.'

    echo "Servers request with limit successful"
    echo "-------------------------------------"
    return 0
  else
    echo "Response:"
    echo "$http_response" | jq '.' 2>/dev/null || echo "$http_response"
    echo "Servers request with limit failed"
    echo "-------------------------------------"
    return 1
  fi
}

# Test ping endpoint
test_ping() {
  echo "Testing ping endpoint: $HOST/v0/ping"
  
  # Get response and status code
  http_response=$(curl -s "$HOST/v0/ping")
  status_code=$(curl -s -o /dev/null -w "%{http_code}" "$HOST/v0/ping")
  
  echo "Status Code: $status_code"
  
  if [[ $status_code == 2* ]]; then
    # Parse JSON response with jq
    echo "Response:"
    echo "$http_response" | jq '.'
    echo "Ping successful"
    echo "-------------------------------------"
    return 0
  else
    echo "Response:"
    echo "$http_response" | jq '.' 2>/dev/null || echo "$http_response"
    echo "Ping failed"
    echo "-------------------------------------"
    return 1
  fi
}

# Run tests based on selected endpoint
success=0
if [[ "$ENDPOINT" == "health" || "$ENDPOINT" == "all" ]]; then
  test_health
  success=$((success + $?))
fi

if [[ "$ENDPOINT" == "servers" || "$ENDPOINT" == "all" ]]; then
  if [ -n "$LIMIT" ]; then
    test_servers_with_limit "$LIMIT"
  else
    test_servers
  fi
  success=$((success + $?))
fi

if [[ "$ENDPOINT" == "ping" || "$ENDPOINT" == "all" ]]; then
  test_ping
  success=$((success + $?))
fi

# Return overall success/failure
if [[ $success -eq 0 ]]; then
  echo "All tests passed successfully!"
  exit 0
else
  echo "Some tests failed!"
  exit 1
fi