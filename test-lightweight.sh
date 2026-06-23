#!/bin/bash

# 測試 MCP Registry API 端點

BASE_URL="${BASE_URL:-http://localhost:8081}"

GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${BLUE}🧪 測試 MCP Registry API${NC}"
echo -e "${BLUE}========================${NC}"
echo -e "Base URL: ${YELLOW}${BASE_URL}${NC}"
echo ""

# Test 1: Health Check
echo -e "${BLUE}1️⃣ Health Check${NC}"
echo "GET ${BASE_URL}/healthz"
response=$(curl -s "${BASE_URL}/healthz")
if echo "$response" | jq -e '.status == "healthy"' > /dev/null 2>&1; then
    echo -e "${GREEN}✓ 成功${NC}"
    echo "$response" | jq '.'
else
    echo -e "${YELLOW}⚠ 回應:${NC} $response"
fi
echo ""

# Test 2: Ping
echo -e "${BLUE}2️⃣ Ping${NC}"
echo "GET ${BASE_URL}/v0.1/ping"
response=$(curl -s "${BASE_URL}/v0.1/ping")
if echo "$response" | jq -e '.status == "ok"' > /dev/null 2>&1; then
    echo -e "${GREEN}✓ 成功${NC}"
    echo "$response" | jq '.'
else
    echo -e "${YELLOW}⚠ 回應:${NC} $response"
fi
echo ""

# Test 3: List All Servers
echo -e "${BLUE}3️⃣ List All Servers${NC}"
echo "GET ${BASE_URL}/v0.1/servers"
response=$(curl -s "${BASE_URL}/v0.1/servers")
if echo "$response" | jq -e '.servers' > /dev/null 2>&1; then
    echo -e "${GREEN}✓ 成功${NC}"
    count=$(echo "$response" | jq '.metadata.count')
    echo "找到 ${count} 個伺服器"
    echo "$response" | jq '{servers: [.servers[0] | {name, version, description}], metadata}'
else
    echo -e "${YELLOW}⚠ 回應:${NC} $response"
fi
echo ""

# Test 4: Get Figma MCP Server (latest)
echo -e "${BLUE}4️⃣ Get Figma MCP Server (latest)${NC}"
echo "GET ${BASE_URL}/v0.1/servers/io.figma%2Fmcp-server/versions/latest"
response=$(curl -s "${BASE_URL}/v0.1/servers/io.figma%2Fmcp-server/versions/latest")
if echo "$response" | jq -e '.name' > /dev/null 2>&1; then
    echo -e "${GREEN}✓ 成功${NC}"
    echo "$response" | jq '{name, version, description, status}'
else
    echo -e "${YELLOW}⚠ 回應:${NC} $response"
fi
echo ""

# Test 5: Get Figma MCP Server (v1.0.0)
echo -e "${BLUE}5️⃣ Get Figma MCP Server (v1.0.0)${NC}"
echo "GET ${BASE_URL}/v0.1/servers/io.figma%2Fmcp-server/versions/1.0.0"
response=$(curl -s "${BASE_URL}/v0.1/servers/io.figma%2Fmcp-server/versions/1.0.0")
if echo "$response" | jq -e '.name' > /dev/null 2>&1; then
    echo -e "${GREEN}✓ 成功${NC}"
    echo "$response" | jq '{name, version, description, status}'
else
    echo -e "${YELLOW}⚠ 回應:${NC} $response"
fi
echo ""

# Test 6: Get Airtable MCP Server (latest)
echo -e "${BLUE}6️⃣ Get Airtable MCP Server (latest)${NC}"
echo "GET ${BASE_URL}/v0.1/servers/io.github.domdomegg%2Fairtable-mcp-server/versions/latest"
response=$(curl -s "${BASE_URL}/v0.1/servers/io.github.domdomegg%2Fairtable-mcp-server/versions/latest")
if echo "$response" | jq -e '.name' > /dev/null 2>&1; then
    echo -e "${GREEN}✓ 成功${NC}"
    echo "$response" | jq '{name, version, description}'
else
    echo -e "${YELLOW}⚠ 回應:${NC} $response"
fi
echo ""

# Test 7: POST Create Server
echo -e "${BLUE}7️⃣ POST Create Server${NC}"
echo "POST ${BASE_URL}/v0.1/servers"
response=$(curl -s -X POST "${BASE_URL}/v0.1/servers" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "com.example/test-server",
    "description": "Test MCP Server",
    "version": "1.0.0",
    "repository": {
      "url": "https://github.com/example/test-server",
      "source": "github"
    }
  }')
if echo "$response" | jq -e '.message' > /dev/null 2>&1; then
    echo -e "${GREEN}✓ 成功${NC}"
    echo "$response" | jq '.'
else
    echo -e "${YELLOW}⚠ 回應:${NC} $response"
fi
echo ""

# Test 8: Not Found (404)
echo -e "${BLUE}8️⃣ Get Non-existent Server (應返回 404)${NC}"
echo "GET ${BASE_URL}/v0.1/servers/nonexistent.server/versions/latest"
http_code=$(curl -s -o /dev/null -w "%{http_code}" "${BASE_URL}/v0.1/servers/nonexistent.server/versions/latest")
if [ "$http_code" = "404" ]; then
    echo -e "${GREEN}✓ 成功 - 正確返回 404${NC}"
else
    echo -e "${YELLOW}⚠ HTTP Code: ${http_code} (期望 404)${NC}"
fi
echo ""

echo -e "${BLUE}========================${NC}"
echo -e "${GREEN}✅ 測試完成${NC}"
