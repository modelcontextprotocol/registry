# MCP Registry

[English](README.md) | [繁體中文](README.zh-TW.md) | 简体中文

一个由社区驱动的 Model Context Protocol (MCP) 服务器注册服务。

## 开发状态

本项目在公开环境下开发，目前处于早期阶段。请参阅[项目概述讨论](https://github.com/modelcontextprotocol/registry/discussions/11)以了解项目范围和目标。如需贡献，请查阅[贡献指南](CONTRIBUTING.md)。

## 概述

MCP Registry 服务为 MCP 服务器条目提供集中式存储库。它允许发现和管理各种 MCP 实现及其相关的元数据、配置和能力。

## 特性

- 提供 RESTful API 用于管理 MCP 注册条目（列表、获取、新增、更新、删除）
- 健康检查端点，便于服务监控
- 支持多种环境配置
- 优雅的关闭处理
- 支持 MongoDB 和内存数据库
- 完整的 API 文档
- 注册条目列表支持分页

## 快速开始

### 先决条件

- Go 1.18 或更高版本
- MongoDB
- Docker（可选，但推荐用于开发）

## 运行

最简单的启动方式是使用 `docker compose`。这将设置 MCP Registry 服务，导入种子数据并在本地 Docker 环境中运行 MongoDB。

```bash
# 构建 Docker 镜像
docker build -t registry .

# 使用 docker compose 启动 registry 和 MongoDB
docker compose up
```

这会启动 MCP Registry 服务和 MongoDB，并在 8080 端口对外开放。

## 构建

如果你希望不通过 Docker 直接在本地运行服务，可以使用 Go 直接构建和运行。

```bash
# 构建 registry 可执行文件
go build ./cmd/registry
```

这将在当前目录下生成 `registry` 可执行文件。你需要确保本地或 Docker 中已运行 MongoDB。

默认情况下，服务会运行在 `http://localhost:8080`。

## 项目结构

```text
├── api/           # OpenApi 规范
├── cmd/           # 应用程序入口
├── config/        # 配置文件
├── internal/      # 私有应用代码
│   ├── api/       # HTTP 服务器与请求处理
│   ├── config/    # 配置管理
│   ├── model/     # 数据模型
│   └── service/   # 业务逻辑
├── pkg/           # 公共库
├── scripts/       # 工具脚本
└── tools/         # 命令行工具
    └── publisher/ # 发布 MCP 服务器到 registry 的工具
```

## API 文档

API 使用 Swagger/OpenAPI 进行文档编写。你可以通过以下路径访问交互式 Swagger UI：

```text
/v0/swagger/index.html
```

这里提供所有端点的完整参考，包括请求/响应结构，并可直接在浏览器中测试 API。

## API 端点

### 健康检查

```text
GET /v0/health
```

返回服务健康状态：

```json
{
  "status": "ok"
}
```

### Registry 端点

#### 列出 Registry 服务器条目

```text
GET /v0/servers
```

以分页方式列出 MCP registry 服务器条目。

查询参数：

- `limit`: 返回的最大条目数（默认：30，最大：100）
- `cursor`: 分页游标，用于获取下一批结果

响应示例：

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

#### 获取服务器详情

```
GET /v0/servers/{id}
```

检索特定 MCP 服务器条目的详细信息。

路径参数：
- `id`: 服务器条目的唯一标识符

响应示例：
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

#### 发布服务器条目

```text
POST /v0/publish
```

将新的 MCP 服务器条目发布到 registry。需在 Authorization 头中以 Bearer token 进行认证。

请求头：

- `Authorization`: Bearer token（例如：`Bearer your_token_here`）
- `Content-Type`: application/json

请求体示例：

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

响应示例：

```json
{
  "message": "Server publication successful",
  "id": "1234567890abcdef12345678"
}
```

### Ping 端点

```text
GET /v0/ping
```

简单的 ping 端点，返回环境配置信息：

```json
{
  "environment": "dev",
  "version": "registry-<sha>"
}
```

## 配置

服务可通过环境变量进行配置：

| 变量名                              | 说明                       | 默认值                      |
| ----------------------------------- | -------------------------- | --------------------------- |
| `MCP_REGISTRY_APP_VERSION`          | 应用版本                   | `dev`                       |
| `MCP_REGISTRY_COLLECTION_NAME`      | MongoDB 集合名             | `servers_v2`                |
| `MCP_REGISTRY_DATABASE_NAME`        | MongoDB 数据库名           | `mcp-registry`              |
| `MCP_REGISTRY_DATABASE_URL`         | MongoDB 连接字符串         | `mongodb://localhost:27017` |
| `MCP_REGISTRY_GITHUB_CLIENT_ID`     | GitHub App Client ID       |                             |
| `MCP_REGISTRY_GITHUB_CLIENT_SECRET` | GitHub App Client Secret   |                             |
| `MCP_REGISTRY_LOG_LEVEL`            | 日志级别                   | `info`                      |
| `MCP_REGISTRY_SEED_FILE_PATH`       | 种子文件导入路径           | `data/seed.json`            |
| `MCP_REGISTRY_SEED_IMPORT`          | 首次启动时导入 `seed.json` | `true`                      |
| `MCP_REGISTRY_SERVER_ADDRESS`       | 服务器监听地址             | `:8080`                     |

## 测试

运行测试脚本以验证 API 端点：

```bash
./scripts/test_endpoints.sh
```

你可以指定特定端点进行测试：

```bash
./scripts/test_endpoints.sh --endpoint health
./scripts/test_endpoints.sh --endpoint servers
```

## 许可证

详见 [LICENSE](LICENSE) 文件。

## 贡献

详见 [CONTRIBUTING](CONTRIBUTING.md) 文件。
