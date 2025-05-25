
# MCP 注册表

[English](README.md) | [中文](README.zh-CN.md)

一个社区驱动的模型上下文协议（MCP）服务器注册服务。

## 开发状态

本项目正在公开构建中，目前处于早期开发阶段。请查看[概述讨论](https://github.com/modelcontextprotocol/registry/discussions/11)了解项目范围和目标。如果您希望贡献，请查看[贡献指南](CONTRIBUTING.md)。

## 概述

MCP注册表服务提供了一个集中式的MCP服务器条目存储库。它允许发现和管理各种MCP实现及其相关元数据、配置和功能。

## 功能

- 用于管理MCP注册表条目的RESTful API（列出、获取、创建、更新、删除）
- 服务监控的健康检查端点
- 支持各种环境配置
- 优雅关闭处理
- MongoDB和内存数据库支持
- 全面的API文档
- 列出注册表条目的分页支持

## 入门指南

### 先决条件

- Go 1.18或更高版本
- MongoDB
- Docker（可选，但推荐用于开发）

## 运行

启动注册表最简单的方法是使用`docker compose`。这将设置MCP注册表服务，导入种子数据并在本地Docker环境中运行MongoDB。

```bash
# 构建Docker镜像
docker build -t registry .

# 使用docker compose运行注册表和MongoDB
docker compose up
```

这将使用Docker启动MCP注册表服务和MongoDB，并在8080端口上暴露服务。

## 构建

如果您更喜欢在没有Docker的情况下在本地运行服务，可以直接使用Go进行构建和运行。

```bash
# 构建注册表可执行文件
go build ./cmd/registry
```
这将在当前目录中创建`registry`二进制文件。您需要在本地或通过Docker运行MongoDB。

默认情况下，服务将在`http://localhost:8080`上运行。

## 项目结构

```
├── api/           # OpenApi规范
├── cmd/           # 应用程序入口点
├── config/        # 配置文件
├── internal/      # 私有应用程序代码
│   ├── api/       # HTTP服务器和请求处理程序
│   ├── config/    # 配置管理
│   ├── model/     # 数据模型
│   └── service/   # 业务逻辑
├── pkg/           # 公共库
├── scripts/       # 实用脚本
└── tools/         # 命令行工具
    └── publisher/ # 用于向注册表发布MCP服务器的工具
```

## API文档

API使用Swagger/OpenAPI记录。您可以通过以下地址访问交互式Swagger UI：

```
/v0/swagger/index.html
```

这提供了所有端点的完整参考，包括请求/响应架构，并允许您直接从浏览器测试API。

## API端点

### 健康检查

```
GET /v0/health
```

返回服务的健康状态：
```json
{
  "status": "ok"
}
```

### 注册表端点

#### 列出注册表服务器条目

```
GET /v0/servers
```

列出MCP注册表服务器条目，支持分页。

查询参数：
- `limit`：要返回的条目最大数量（默认：30，最大：100）
- `cursor`：用于检索下一组结果的分页游标

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

检索有关特定MCP服务器条目的详细信息。

路径参数：
- `id`：服务器条目的唯一标识符

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

```
POST /v0/publish
```

将新的MCP服务器条目发布到注册表。需要通过Authorization头中的Bearer令牌进行身份验证。

头部：
- `Authorization`：用于身份验证的Bearer令牌（例如，`Bearer your_token_here`）
- `Content-Type`：application/json

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

### Ping端点

```
GET /v0/ping
```

简单的ping端点，返回环境配置信息：
```json
{
  "environment": "dev",
  "version": "registry-<sha>"
}
```

## 配置

可以使用环境变量配置服务：

| 变量 | 描述 | 默认值 |
|----------|-------------|---------|
| `MCP_REGISTRY_APP_VERSION`           | 应用程序版本 | `dev` |
| `MCP_REGISTRY_COLLECTION_NAME`       | MongoDB集合名称 | `servers_v2` |
| `MCP_REGISTRY_DATABASE_NAME`         | MongoDB数据库名称 | `mcp-registry` |
| `MCP_REGISTRY_DATABASE_URL`          | MongoDB连接字符串 | `mongodb://localhost:27017` |
| `MCP_REGISTRY_GITHUB_CLIENT_ID`      | GitHub应用客户端ID |  |
| `MCP_REGISTRY_GITHUB_CLIENT_SECRET`  | GitHub应用客户端密钥 |  |
| `MCP_REGISTRY_LOG_LEVEL`             | 日志级别 | `info` |
| `MCP_REGISTRY_SEED_FILE_PATH`        | 导入种子文件的路径 | `data/seed.json` |
| `MCP_REGISTRY_SEED_IMPORT`           | 首次运行时导入`seed.json` | `true` |
| `MCP_REGISTRY_SERVER_ADDRESS`        | 服务器监听地址 | `:8080` |


## 测试

运行测试脚本以验证API端点：

```bash
./scripts/test_endpoints.sh
```

您可以指定要测试的特定端点：

```bash
./scripts/test_endpoints.sh --endpoint health
./scripts/test_endpoints.sh --endpoint servers
```

## 许可证

有关详细信息，请参阅[LICENSE](LICENSE)文件。

## 贡献

有关详细信息，请参阅[CONTRIBUTING](CONTRIBUTING.md)文件。
