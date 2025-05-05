# Examples

## /v0/servers

### Request

```http
GET /v0/servers?limit=5000&offset=0
```

### Response

```json
{
  "servers": [
    {
      "id": "a5e8a7f0-d4e4-4a1d-b12f-2896a23fd4f1",
      "name": "@modelcontextprotocol/servers/src/filesystem",
      "description": "Node.js server implementing Model Context Protocol (MCP) for filesystem operations.",
      "repository": {
        "url": "https://github.com/modelcontextprotocol/servers",
        "subfolder": "src/filesystem",
        "branch": "main",
        "commit": "d94b5f7ec7c6d7602c78a5e9b8a5b8c94d093eda"
      },
      "version": "0.0.3"
    }
  ],
  "next": "https://registry.modelcontextprotocol.io/servers?offset=50",
  "total_count": 1
}
```

## /v0/servers/:id

### Request

```http
GET /v0/servers/a5e8a7f0-d4e4-4a1d-b12f-2896a23fd4f1?version=0.0.3
```

### Response

```json
{
  "id": "a5e8a7f0-d4e4-4a1d-b12f-2896a23fd4f1",
  "name": "@modelcontextprotocol/servers/src/filesystem",
  "description": "Node.js server implementing Model Context Protocol (MCP) for filesystem operations.",
  "version": "0.0.3",
  "repository": {
    "url": "https://github.com/modelcontextprotocol/servers",
    "subfolder": "src/filesystem",
    "branch": "main",
    "commit": "d94b5f7ec7c6d7602c78a5e9b8a5b8c94d093eda"
  },
  "registry_canonical": "npm",
  "registries": [
    {
      "name": "npm",
      "package_name": "@modelcontextprotocol/server-filesystem",
      "license": "MIT",
      "command_arguments": {
        "subcommands": [],
        "positional_arguments": [
          {
            "position": 0,
            "name": "package",
            "description": "NPM package name",
            "default_value": "@modelcontextprotocol/server-filesystem",
            "is_required": true,
            "is_editable": false,
            "is_repeatable": false,
            "choices": []
          },
          {
            "position": 1,
            "name": "path",
            "description": "Path to access",
            "default_value": "/Users/username/Desktop",
            "is_required": true,
            "is_editable": true,
            "is_repeatable": true,
            "choices": []
          }
        ],
        "named_arguments": [
          {
            "short_flag": "-y",
            "requires_value": false,
            "is_required": false,
            "is_editable": false,
            "description": "Skip prompts and automatically answer yes",
            "choices": []
          }
        ]
      },
      "environment_variables": [
        {
          "name": "LOG_LEVEL",
          "description": "Logging level (debug, info, warn, error)",
          "required": false,
          "default_value": "info"
        }
      ]
    },
    {
      "name": "docker",
      "package_name": "mcp/filesystem",
      "license": "MIT",
      "command_arguments": {
        "subcommands": [
          {
            "name": "run",
            "description": "Run the Docker container",
            "is_required": true,
            "subcommands": [],
            "positional_arguments": [],
            "named_arguments": [
              {
                "short_flag": "-i",
                "requires_value": false,
                "is_required": true,
                "is_editable": false,
                "description": "Run in interactive mode"
              },
              {
                "long_flag": "--rm",
                "requires_value": false,
                "is_required": true,
                "is_editable": false,
                "description": "Remove container when it exits"
              },
              {
                "long_flag": "--mount",
                "requires_value": true,
                "is_required": true,
                "is_repeatable": true,
                "is_editable": true,
                "description": "Mount a volume into the container",
                "default_value": "type=bind,src=/Users/username/Desktop,dst=/projects/Desktop",
                "choices": []
              }
            ]
          }
        ],
        "positional_arguments": [
          {
            "position": 0,
            "name": "image",
            "description": "Docker image name",
            "default_value": "mcp/filesystem",
            "is_required": true,
            "is_editable": false,
            "is_repeatable": false,
            "choices": []
          },
          {
            "position": 1,
            "name": "root_path",
            "description": "Root path for filesystem access",
            "default_value": "/projects",
            "is_required": true,
            "is_editable": false,
            "is_repeatable": false,
            "choices": []
          }
        ],
        "named_arguments": []
      },
      "environment_variables": [
        {
          "name": "LOG_LEVEL",
          "description": "Logging level (debug, info, warn, error)",
          "required": false,
          "default_value": "info"
        }
      ]
    }
  ],
  "remotes": [
    {
      "transport_type": "sse",
      "url": "https://mcp-fs.example.com/sse"
    }
  ]
}
```

### Server Configuration Examples

#### Local Server with npx

API Response:
```json
{
  "id": "brave-search-12345",
  "name": "@modelcontextprotocol/server-brave-search",
  "description": "MCP server for Brave Search API integration",
  "version": "1.0.0",
  "repository": {
    "url": "https://github.com/modelcontextprotocol/servers",
    "subfolder": "src/brave-search",
    "branch": "main",
    "commit": "abc123"
  },
  "registry_canonical": "npm",
  "registries": [
    {
      "name": "npm",
      "package_name": "@modelcontextprotocol/server-brave-search",
      "license": "MIT",
      "command_arguments": {
        "subcommands": [],
        "positional_arguments": [],
        "named_arguments": [
          {
            "short_flag": "-y",
            "requires_value": false,
            "is_required": false,
            "description": "Skip prompts"
          }
        ]
      },
      "environment_variables": [
        {
          "name": "BRAVE_API_KEY",
          "description": "Brave Search API Key",
          "required": true,
          "default_value": ""
        }
      ]
    }
  ]
}
```

claude_desktop_config.json:
```json
{
  "brave-search": {
    "command": "npx",
    "args": [
      "-y",
      "@modelcontextprotocol/server-brave-search"
    ],
    "env": {
      "BRAVE_API_KEY": "YOUR_API_KEY_HERE"
    }
  }
}
```

#### Local Server with Docker

API Response:
```json
{
  "id": "filesystem-67890",
  "name": "@modelcontextprotocol/servers/src/filesystem",
  "description": "Node.js server implementing Model Context Protocol (MCP) for filesystem operations",
  "version": "0.0.3",
  "repository": {
    "url": "https://github.com/modelcontextprotocol/servers",
    "subfolder": "src/filesystem",
    "branch": "main",
    "commit": "d94b5f7ec7c6d7602c78a5e9b8a5b8c94d093eda"
  },
  "registry_canonical": "docker",
  "registries": [
    {
      "name": "docker",
      "package_name": "mcp/filesystem",
      "license": "MIT",
      "command_arguments": {
        "subcommands": [
          {
            "name": "run",
            "description": "Run the Docker container",
            "is_required": true,
            "named_arguments": [
              {
                "short_flag": "-i",
                "requires_value": false,
                "is_required": true,
                "description": "Run in interactive mode"
              },
              {
                "long_flag": "--rm",
                "requires_value": false,
                "is_required": true,
                "description": "Remove container when it exits"
              },
              {
                "long_flag": "--mount",
                "requires_value": true,
                "is_required": true,
                "is_repeatable": true,
                "description": "Mount a volume into the container"
              }
            ]
          }
        ],
        "positional_arguments": [
          {
            "position": 0,
            "name": "image",
            "description": "Docker image name",
            "default_value": "mcp/filesystem"
          },
          {
            "position": 1,
            "name": "root_path",
            "description": "Root path for filesystem access",
            "default_value": "/projects"
          }
        ]
      }
    }
  ]
}
```

claude_desktop_config.json:
```json
{
  "filesystem": {
    "command": "docker",
    "args": [
      "run",
      "-i",
      "--rm",
      "--mount", "type=bind,src=/Users/username/Desktop,dst=/projects/Desktop",
      "--mount", "type=bind,src=/path/to/other/allowed/dir,dst=/projects/other/allowed/dir,ro",
      "--mount", "type=bind,src=/path/to/file.txt,dst=/projects/path/to/file.txt",
      "mcp/filesystem",
      "/projects"
    ]
  }
}
```

#### Remote Server

API Response:
```json
{
  "id": "remote-fs-54321",
  "name": "Remote Brave Search Server",
  "description": "Cloud-hosted MCP Brave Search server",
  "version": "1.0.0",
  "repository": {
    "url": "https://github.com/example/remote-fs",
    "branch": "main",
    "commit": "xyz789"
  },
  "remotes": [
    {
      "transport_type": "sse",
      "url": "https://mcp-fs.example.com/sse"
    }
  ]
}
```