# MCP Registry

[English](README.md) | 繁體中文 | [简体中文](README.zh-CN.md)

一個由社群驅動的 Model Context Protocol (MCP) 伺服器註冊服務。

## 開發狀態

本專案正在公開開發中，目前處於早期開發階段。請參閱[專案概述討論](https://github.com/modelcontextprotocol/registry/discussions/11)以了解專案範圍與目標。如欲貢獻，請參考[貢獻指南](CONTRIBUTING.md)。

## 概述

MCP Registry 服務提供 MCP 伺服器條目的集中式儲存庫。它允許發現與管理各種 MCP 實作及其相關的中繼資料、設定與功能。

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

最簡單的啟動方式是使用 `docker compose`。這會設定 MCP Registry 服務、匯入種子資料並於本地 Docker 環境中執行 MongoDB。

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
      "id": "1",
      "name": "Example MCP Server",
      "description": "An example MCP server implementation",
      "url": "https://example.com/mcp",
      "repository": {
        "url": "https://github.com/example/mcp-server",
        "stars": 120
      },
      "version": "1.0.0"
    }
  ],
  "metadata": {
    "next_cursor": "cursor-value-for-next-page"
  }
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
  "server_detail": {
    "name": "io.github.username/repository",
    "description": "Your MCP server description",
    "version_detail": {
      "version": "1.0.0"
    },
    "registries": [
      {
        "name": "npm",
        "package_name": "your-package-name",
        "license": "MIT"
      }
    ],
    "remotes": [
      {
        "transport_type": "http",
        "url": "https://your-api-endpoint.com"
      }
    ]
  },
  "repo_ref": "username/repository"
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
