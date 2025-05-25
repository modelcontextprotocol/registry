# MCP Registry

[English](README.md) | 繁體中文 | [简体中文](README.zh-CN.md)

一個由社群驅動的 Model Context Protocol (MCP) 伺服器註冊服務。

## 開發狀態

本專案於公開環境下開發，目前處於早期階段。請參閱[專案概述討論](https://github.com/modelcontextprotocol/registry/discussions/11)以了解專案範圍與目標。如欲貢獻，請查閱[貢獻指南](CONTRIBUTING.md)。

## 概述

MCP Registry 服務為 MCP 伺服器條目提供集中式儲存庫。它允許發現與管理各種 MCP 實作及其相關的中繼資料、設定與能力。

## 特色

- 提供 RESTful API 以管理 MCP 註冊條目（列表、取得、新增、更新、刪除）
- 健康檢查端點，方便服務監控
- 支援多種環境設定
- 優雅的關閉處理
- 支援 MongoDB 與記憶體資料庫
- 完整的 API 文件
- 註冊條目列表支援分頁

## 快速開始

### 先決條件

- Go 1.18 或更新版本
- MongoDB
- Docker（選用，但建議於開發時使用）

## 執行

最簡單的啟動方式是使用 `docker compose`。這將設定 MCP Registry 服務、匯入種子資料並於本地 Docker 環境中執行 MongoDB。

```bash
# 建立 Docker 映像檔
docker build -t registry .

# 使用 docker compose 啟動 registry 與 MongoDB
docker compose up
```

這會啟動 MCP Registry 服務與 MongoDB，並於 8080 埠口對外開放。

## 建置

若你希望不透過 Docker 直接於本地執行服務，可使用 Go 直接建置與執行。

```bash
# 建置 registry 執行檔
go build ./cmd/registry
```

這會在目前目錄下產生 `registry` 執行檔。你需要確保本地或 Docker 中有執行 MongoDB。

預設情況下，服務會運行於 `http://localhost:8080`。

## 專案結構

```text
├── api/           # OpenApi 規格
├── cmd/           # 應用程式進入點
├── config/        # 設定檔
├── internal/      # 私有應用程式碼
│   ├── api/       # HTTP 伺服器與請求處理
│   ├── config/    # 設定管理
│   ├── model/     # 資料模型
│   └── service/   # 商業邏輯
├── pkg/           # 公用程式庫
├── scripts/       # 工具腳本
└── tools/         # 命令列工具
    └── publisher/ # 發佈 MCP 伺服器至 registry 的工具
```

## API 文件

API 以 Swagger/OpenAPI 格式記錄。你可以透過以下路徑存取互動式 Swagger UI：

```text
/v0/swagger/index.html
```

這裡提供所有端點的完整參考，包括請求/回應結構，並可直接於瀏覽器測試 API。

## API 端點

### 健康檢查

```text
GET /v0/health
```

回傳服務健康狀態：

```json
{
  "status": "ok"
}
```

### Registry 端點

#### 列出 Registry 伺服器條目

```text
GET /v0/servers
```

以分頁方式列出 MCP registry 伺服器條目。

查詢參數：

- `limit`: 回傳的最大條目數（預設：30，最大：100）
- `cursor`: 分頁游標，用於取得下一批結果

回應範例：

```json
{
  "servers": [
    {
      "id": "123e4567-e89b-12d3-a456-426614174000",
      "name": "Example MCP Server",
      "url": "https://example.com/mcp",
      "description": "An example MCP server",
      "created_at": "2025-05-17T17:34:22.912Z",
      "updated_at": "2025-05-17T17:34:22.912Z"
    }
  ],
  "metadata": {
    "next_cursor": "123e4567-e89b-12d3-a456-426614174000",
    "count": 30
  }
}
```

#### 取得伺服器詳細資訊

```
GET /v0/servers/{id}
```

檢索特定 MCP 伺服器條目的詳細資訊。

路徑參數：
- `id`: 伺服器條目的唯一識別碼

回應範例：
```json
{
  "id": "01129bff-3d65-4e3d-8e82-6f2f269f818c",
  "name": "io.github.gongrzhe/redis-mcp-server",
  "description": "A Redis MCP server (pushed to https://github.com/modelcontextprotocol/servers/tree/main/src/redis) implementation for interacting with Redis databases. This server enables LLMs to interact with Redis key-value stores through a set of standardized tools.",
  "repository": {
    "url": "https://github.com/GongRzhe/REDIS-MCP-Server",
    "source": "github",
    "id": "907849235"
  },
  "version_detail": {
    "version": "0.0.1-seed",
    "release_date": "2025-05-16T19:13:21Z",
    "is_latest": true
  },
  "packages": [
    {
      "registry_name": "docker",
      "name": "@gongrzhe/server-redis-mcp",
      "version": "1.0.0",
      "package_arguments": [
        {
          "description": "Docker image to run",
          "is_required": true,
          "format": "string",
          "value": "mcp/redis",
          "default": "mcp/redis",
          "type": "positional",
          "value_hint": "mcp/redis"
        },
        {
          "description": "Redis server connection string",
          "is_required": true,
          "format": "string",
          "value": "redis://host.docker.internal:6379",
          "default": "redis://host.docker.internal:6379",
          "type": "positional",
          "value_hint": "host.docker.internal:6379"
        }
      ]
    }
  ]
}
```

#### 發佈伺服器條目

```text
POST /v0/publish
```

將新的 MCP 伺服器條目發佈至 registry。需於 Authorization 標頭中以 Bearer token 進行驗證。

標頭：

- `Authorization`: Bearer token（例如：`Bearer your_token_here`）
- `Content-Type`: application/json

請求範例：

```json
{
    "description": "<your description here>",
    "name": "io.github.<owner>/<server-name>",
    "packages": [
        {
            "registry_name": "npm",
            "name": "@<owner>/<server-name>",
            "version": "0.2.23",
            "package_arguments": [
                {
                    "description": "Specify services and permissions.",
                    "is_required": true,
                    "format": "string",
                    "value": "-s",
                    "default": "-s",
                    "type": "positional",
                    "value_hint": "-s"
                }
            ],
            "environment_variables": [
                {
                    "description": "API Key to access the server",
                    "name": "API_KEY"
                }
            ]
        },{
            "registry_name": "docker",
            "name": "@<owner>/<server-name>-cli",
            "version": "0.123.223",
            "runtime_hint": "docker",
            "runtime_arguments": [
                {
                    "description": "Specify services and permissions.",
                    "is_required": true,
                    "format": "string",
                    "value": "--mount",
                    "default": "--mount",
                    "type": "positional",
                    "value_hint": "--mount"
                }
            ],
            "environment_variables": [
                {
                    "description": "API Key to access the server",
                    "name": "API_KEY"
                }
            ]
        }
    ],
    "repository": {
        "url": "https://github.com//<owner>/<server-name>",
        "source": "github"
    },
    "version_detail": {
        "version": "0.0.1-<publisher_version>"
    }
}
```

回應範例：

```json
{
  "message": "Server publication successful",
  "id": "1234567890abcdef12345678"
}
```

### Ping 端點

```text
GET /v0/ping
```

簡單的 ping 端點，回傳環境設定資訊：

```json
{
  "environment": "dev",
  "version": "registry-<sha>"
}
```

## 設定

服務可透過環境變數進行設定：

| 變數名稱                            | 說明                       | 預設值                      |
| ----------------------------------- | -------------------------- | --------------------------- |
| `MCP_REGISTRY_APP_VERSION`          | 應用程式版本               | `dev`                       |
| `MCP_REGISTRY_COLLECTION_NAME`      | MongoDB 集合名稱           | `servers_v2`                |
| `MCP_REGISTRY_DATABASE_NAME`        | MongoDB 資料庫名稱         | `mcp-registry`              |
| `MCP_REGISTRY_DATABASE_URL`         | MongoDB 連線字串           | `mongodb://localhost:27017` |
| `MCP_REGISTRY_GITHUB_CLIENT_ID`     | GitHub App Client ID       |                             |
| `MCP_REGISTRY_GITHUB_CLIENT_SECRET` | GitHub App Client Secret   |                             |
| `MCP_REGISTRY_LOG_LEVEL`            | 日誌等級                   | `info`                      |
| `MCP_REGISTRY_SEED_FILE_PATH`       | 匯入種子檔案路徑           | `data/seed.json`            |
| `MCP_REGISTRY_SEED_IMPORT`          | 首次啟動時匯入 `seed.json` | `true`                      |
| `MCP_REGISTRY_SERVER_ADDRESS`       | 伺服器監聽位址             | `:8080`                     |

## 測試

執行測試腳本以驗證 API 端點：

```bash
./scripts/test_endpoints.sh
```

你可以指定特定端點進行測試：

```bash
./scripts/test_endpoints.sh --endpoint health
./scripts/test_endpoints.sh --endpoint servers
```

## 授權

詳見 [LICENSE](LICENSE) 檔案。

## 貢獻

詳見 [CONTRIBUTING](CONTRIBUTING.md) 檔案。
