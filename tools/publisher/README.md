# MCP Registry Publisher Tool

The MCP Registry Publisher Tool is designed to publish Model Context Protocol (MCP) server details to an MCP registry. This tool uses GitHub OAuth device flow authentication to securely manage the publishing process.

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
./bin/mcp-publisher -registry-url <REGISTRY_URL> -mcp-file <PATH_TO_MCP_FILE>

# Force a new login even if a token exists
./bin/mcp-publisher -registry-url <REGISTRY_URL> -mcp-file <PATH_TO_MCP_FILE> -login
```

### Command-line Arguments

- `-registry-url`: URL of the MCP registry (required)
- `-mcp-file`: Path to the MCP configuration file (required)
- `-login`: Force a new GitHub authentication even if a token already exists (overwrites existing token file)
- `-auth-method`: Authentication method to use (default: github-oauth)

## Authentication

The tool has been simplified to use **GitHub OAuth device flow authentication exclusively**. Previous versions supported multiple authentication methods, but this version focuses solely on GitHub OAuth for better security and user experience.

1. **Automatic Setup**: The tool automatically retrieves the GitHub Client ID from the registry's health endpoint
2. **First Run Authentication**: When first run (or with the `--login` flag), the tool initiates the GitHub device flow
3. **User Authorization**: You'll be provided with a URL and a verification code to enter on GitHub
4. **Token Storage**: After successful authentication, the tool saves the access token locally in `.mcpregistry_token` for future use
5. **Secure Communication**: The token is sent in the HTTP Authorization header with the Bearer scheme for all registry API calls

**Note**: Authentication is performed via GitHub OAuth App, which you must authorize for the respective resources (e.g., organization access if publishing organization repositories).

## Example

1. Prepare your `mcp.json` file with your server details:

```json
{
  "name": "io.github.yourusername/your-repository",
  "description": "Your MCP server description",
  "version_detail": {
    "version": "1.0.0"
  },
  "packages": [
    {
      "registry_name": "npm",
      "name": "your-npm-package",
      "version": "1.0.0",
      "package_arguments": [
        {
          "description": "Specify services and permissions",
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
          "name": "API_KEY",
          "description": "API Key to access the server"
        }
      ]
    }
  ],
  "repository": {
    "url": "https://github.com/yourusername/your-repository",
    "source": "github"
  }
}
```

2. Run the publisher tool:

```bash
./bin/mcp-publisher --registry-url "https://mcp-registry.example.com" --mcp-file "./mcp.json"
```

3. Follow the authentication instructions in the terminal if prompted.

4. Upon successful publication, you'll see a confirmation message.

## Important Notes

- **GitHub Authentication Only**: The tool exclusively uses GitHub OAuth device flow for authentication
- **Automatic Client ID**: The GitHub Client ID is automatically retrieved from the registry's health endpoint
- **Token Storage**: The authentication token is saved in `.mcpregistry_token` in the current directory
- **Internet Required**: Active internet connection needed for GitHub authentication and registry communication
- **Repository Access**: Ensure the repository and package mentioned in your `mcp.json` file exist and are accessible
- **OAuth Permissions**: You may need to grant the OAuth app access to your GitHub organizations if publishing org repositories
