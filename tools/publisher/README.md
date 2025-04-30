# MCP Registry Publisher Tool

The MCP Registry Publisher Tool is designed to publish Model Context Protocol (MCP) server details to an MCP registry. This tool currently only handles GitHub authentication via device flow and manages the publishing process.

## Building the Tool

You can build the publisher tool using the provided build script:

```bash
# Build for current platform
./build.sh

# Build for all supported platforms (optional)
./build.sh --all
```

The compiled binary will be placed in the `bin` directory.

## Usage

```bash
# Basic usage
./bin/mcp-publisher --registry-url <REGISTRY_URL> --mcp-file <PATH_TO_MCP_FILE>

# Force a new login even if a token exists
./bin/mcp-publisher --registry-url <REGISTRY_URL> --mcp-file <PATH_TO_MCP_FILE> --login
```

### Command-line Arguments

- `--registry-url`: URL of the MCP registry (required)
- `--mcp-file`: Path to the MCP configuration file (required)
- `--login`: Force a new GitHub authentication even if a token already exists (overwrites existing token file)
- `--token`: Use the provided token instead of GitHub authentication (bypasses the device flow)

## Authentication

The tool uses GitHub device flow authentication:
1. When first run (or with `--login` flag), the tool will initiate the GitHub device flow
2. You'll be provided with a URL and a code to enter
3. After successful authentication, the tool saves the token locally for future use
4. The token is sent in the HTTP Authorization header with the Bearer scheme

_NOTE_ : Authentication is made on behalf of a OAuth App which you must authorize for respective resources (e.g `org`)

## Example

1. Prepare your `mcp.json` file with your server details:

```json
{
  "name": "io.github.yourusername/your-repository",
  "description": "Your MCP server description",
  "version_detail": {
    "version": "1.0.0"
  },
  "registries": [
    {
      "name": "npm",
      "package_name": "your-npm-package",
      "license": "MIT",
      "command_arguments": {
        "sub_commands": [
          {
            "name": "start",
            "description": "Start the server",
            "named_arguments": null
          }
        ],
        "positional_arguments": null,
        "environment_variables": [
          {
            "name": "PORT",
            "description": "Port to run the server on",
            "required": false
          }
        ],
        "named_arguments": null
      }
    }
  ],
  "remotes": [
    {
      "transporttype": "http",
      "url": "http://yourdomain.com/api"
    }
  ]
}
```

2. Run the publisher tool:

```bash
./bin/mcp-publisher --registry-url "https://mcp-registry.example.com" --mcp-file "./mcp.json"
```

3. Follow the authentication instructions in the terminal if prompted.

4. Upon successful publication, you'll see a confirmation message.

## Important Notes

- The authentication token is saved in a file named `.mcpregistry_token` in the current directory
- The tool requires an active internet connection to authenticate with GitHub and communicate with the registry
- Make sure the repository and package mentioned in your `mcp.json` file exist and are accessible
