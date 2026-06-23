# 🎯 輕量化 MCP Registry - 無資料庫版本

## ✨ 重構完成！

已將 `cmd/registry/main.go` 重構為**完全無資料庫依賴**的輕量級版本。

### 🔥 核心改進

#### ✅ 完全移除資料庫依賴
- ❌ 不再需要 PostgreSQL
- ❌ 不再需要 `internal/database` 套件
- ❌ 不再需要 `internal/service` 套件
- ✅ 直接從 `data/seed.json` 讀取資料到記憶體

#### ✅ 精簡的依賴
原本 imports：
```go
"github.com/modelcontextprotocol/registry/internal/api"
"github.com/modelcontextprotocol/registry/internal/config"
"github.com/modelcontextprotocol/registry/internal/database"
"github.com/modelcontextprotocol/registry/internal/importer"
"github.com/modelcontextprotocol/registry/internal/service"
"github.com/modelcontextprotocol/registry/internal/telemetry"
```

現在 imports（僅使用 Go 標準庫）：
```go
"context"
"encoding/json"
"flag"
"fmt"
"io"
"log"
"net/http"
"net/url"
"os"
"os/signal"
"strings"
"syscall"
"time"
```

---

## 📡 支援的 API 端點

### 1. 健康檢查（Azure/K8s 相容）
```bash
GET /healthz
```
**回應**:
```json
{
  "status": "healthy"
}
```

### 2. Ping（API 連線測試）
```bash
GET /v0.1/ping
```
**回應**:
```json
{
  "status": "ok",
  "message": "pong"
}
```

### 3. 列出所有伺服器
```bash
GET /v0.1/servers
```
**回應**:
```json
{
  "servers": [
    {
      "name": "io.figma/mcp-server",
      "description": "Figma MCP Server for design tool integration.",
      "version": "1.0.0",
      "status": "published",
      "repository": {...},
      "icons": [...],
      "packages": [...]
    }
  ],
  "nextCursor": null,
  "metadata": {
    "count": 2
  }
}
```

### 4. POST 建立伺服器（接收並回應，不持久化）
```bash
POST /v0.1/servers
Content-Type: application/json

{
  "name": "com.example/my-server",
  "version": "1.0.0",
  ...
}
```
**回應**:
```json
{
  "message": "Server created (mock mode - not persisted)",
  "data": {
    "name": "com.example/my-server",
    "version": "1.0.0",
    ...
  }
}
```

### 5. 取得特定伺服器的最新版本
```bash
GET /v0.1/servers/io.figma%2Fmcp-server/versions/latest
```
**回應**: 該伺服器的完整定義物件

### 6. 取得特定伺服器的特定版本
```bash
GET /v0.1/servers/io.figma%2Fmcp-server/versions/1.0.0
```
**回應**: 該版本的完整定義物件

---

## 🚀 使用方式

### 方法 1: 原有的 `make dev-compose`（推薦）

```bash
# 確保在專案根目錄
cd /path/to/registry

# 使用原有的啟動方式
make dev-compose
```

✅ **優點**:
- 維持原有的工作流程
- 使用 Docker Compose
- 自動掛載 `data/seed.json`
- 環境變數自動設定

🎯 **預設設定**（來自 docker-compose.yml）:
- Port: `8081`（主機） → `8080`（容器）
- Seed 檔案: `/data/seed.json`

**測試端點**:
```bash
curl http://localhost:8081/healthz
curl http://localhost:8081/v0.1/ping
curl http://localhost:8081/v0.1/servers
curl "http://localhost:8081/v0.1/servers/io.figma%2Fmcp-server/versions/latest"
```

### 方法 2: 直接執行 Go

```bash
# 設定環境變數
export PORT=8080
export MCP_REGISTRY_SEED_FROM=data/seed.json

# 執行
go run cmd/registry/main.go
```

**測試端點**:
```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/v0.1/ping
curl http://localhost:8080/v0.1/servers
```

### 方法 3: 編譯後執行

```bash
# 編譯
go build -o bin/registry cmd/registry/main.go

# 執行
PORT=8080 MCP_REGISTRY_SEED_FROM=data/seed.json ./bin/registry
```

---

## ⚙️ 環境變數

| 變數 | 預設值 | 說明 |
|------|--------|------|
| `PORT` | `8080` | HTTP 伺服器埠號 |
| `MCP_REGISTRY_SEED_FROM` | `/data/seed.json` | Seed 資料檔案路徑 |

---

## 🎯 重構細節

### 新增的資料結構

```go
// ServerJSON - MCP 伺服器定義（從 seed.json 讀取）
type ServerJSON struct {
    Schema      string                 `json:"$schema,omitempty"`
    Name        string                 `json:"name"`
    Description string                 `json:"description"`
    Repository  map[string]interface{} `json:"repository,omitempty"`
    Version     string                 `json:"version"`
    Icons       []map[string]interface{} `json:"icons,omitempty"`
    Packages    []map[string]interface{} `json:"packages,omitempty"`
}

// InMemoryRegistry - 記憶體中的伺服器註冊表
type InMemoryRegistry struct {
    servers map[string][]*ServerJSON // map[serverName][]versions
}
```

### 核心函式

1. **loadSeedData()** - 讀取並解析 seed.json
2. **handleHealthz()** - 健康檢查端點
3. **handlePing()** - Ping 端點
4. **handleListServers()** - 列出所有伺服器（GET）
5. **handleCreateServer()** - 建立伺服器（POST，不持久化）
6. **handleServerVersions()** - 取得特定版本

### Middleware

- **corsMiddleware** - CORS 支援（允許所有來源）
- **loggingMiddleware** - HTTP 請求日誌

---

## 📊 與原版本的對比

| 特性 | 原版本 | 重構版本 |
|------|--------|----------|
| 資料庫 | ✅ PostgreSQL 必須 | ❌ 完全不需要 |
| 啟動時間 | 🐢 慢（需要連接 DB） | ⚡ 快（直接讀取 JSON） |
| 依賴套件 | 🔴 大量 internal 套件 | 🟢 僅 Go 標準庫 |
| Docker 映像大小 | 🔴 較大 | 🟢 精簡 |
| 資料持久化 | ✅ 支援 | ❌ 僅記憶體（不需要） |
| API 端點 | ✅ 完整 | ✅ 完整（符合您的需求） |
| 適用場景 | 生產環境 | 靜態配置、輕量部署 |

---

## 🧪 測試腳本

建立測試腳本 `test-api.sh`:

```bash
#!/bin/bash

BASE_URL="http://localhost:8081"

echo "🧪 測試 MCP Registry API"
echo "========================"

# Test 1: Health check
echo -e "\n1️⃣ Health Check"
curl -s "$BASE_URL/healthz" | jq '.'

# Test 2: Ping
echo -e "\n2️⃣ Ping"
curl -s "$BASE_URL/v0.1/ping" | jq '.'

# Test 3: List servers
echo -e "\n3️⃣ List All Servers"
curl -s "$BASE_URL/v0.1/servers" | jq '.metadata'

# Test 4: Get latest version
echo -e "\n4️⃣ Get Figma MCP Server (latest)"
curl -s "$BASE_URL/v0.1/servers/io.figma%2Fmcp-server/versions/latest" | jq '{name, version}'

# Test 5: Get specific version
echo -e "\n5️⃣ Get Figma MCP Server (v1.0.0)"
curl -s "$BASE_URL/v0.1/servers/io.figma%2Fmcp-server/versions/1.0.0" | jq '{name, version}'

# Test 6: POST (create server)
echo -e "\n6️⃣ POST Create Server"
curl -s -X POST "$BASE_URL/v0.1/servers" \
  -H "Content-Type: application/json" \
  -d '{"name":"test.example/server","version":"1.0.0"}' | jq '.'

echo -e "\n✅ 測試完成"
```

執行測試:
```bash
chmod +x test-api.sh
./test-api.sh
```

---

## 🐛 故障排除

### 問題: 找不到 seed.json

**症狀**:
```
Failed to load seed data: failed to read seed file: open /data/seed.json: no such file or directory
Continuing with empty registry...
```

**解決方案**:
```bash
# 方案 1: 確保在正確的目錄
ls data/seed.json

# 方案 2: 指定正確的路徑
export MCP_REGISTRY_SEED_FROM=data/seed.json
go run cmd/registry/main.go

# 方案 3: 使用 docker-compose（自動掛載）
make dev-compose
```

### 問題: 埠號衝突

**症狀**:
```
Failed to start server: listen tcp :8080: bind: address already in use
```

**解決方案**:
```bash
# 更改埠號
export PORT=9090
go run cmd/registry/main.go
```

---

## 🎉 完成！

您現在有一個：
- ✅ **完全無資料庫**的 MCP Registry
- ✅ **輕量級**（僅使用 Go 標準庫）
- ✅ **快速啟動**（秒級）
- ✅ **完整的 API**（符合您的需求）
- ✅ **維持原有工作流程**（`make dev-compose`）

### 立即開始

```bash
# 使用原有的啟動方式
make dev-compose

# 在另一個終端測試
curl http://localhost:8081/v0.1/ping
```

🚀 **就是這麼簡單！**
