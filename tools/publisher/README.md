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

The tool supports two main commands:

### Publishing a server

```bash
# Basic usage
./bin/mcp-publisher publish -registry-url <REGISTRY_URL> -mcp-file <PATH_TO_MCP_FILE>

# Force a new login even if a token exists
./bin/mcp-publisher publish -registry-url <REGISTRY_URL> -mcp-file <PATH_TO_MCP_FILE> -login
```

### Creating a server.json file

```bash
# Create a new server.json file
./bin/mcp-publisher create --name "io.github.owner/repo" --description "My server" --repo-url "https://github.com/owner/repo"
```

### Command-line Arguments

- `-registry-url`: URL of the MCP registry (required)
- `-mcp-file`: Path to the MCP configuration file (required)
- `-login`: Force a new GitHub authentication even if a token already exists (overwrites existing token file)
- `-auth-method`: Authentication method to use (default: github-oauth)

## Creating a server.json file

The tool provides a `create` command to help generate a properly formatted `server.json` file. This command takes various flags to specify the server details and generates a complete server.json file that you can then modify as needed.

### Usage

```bash
./bin/mcp-publisher create [flags]
```

### Create Command Flags

#### Required Flags
- `--name`, `-n`: Server name (e.g., io.github.owner/repo-name)
- `--description`, `-d`: Server description
- `--repo-url`: Repository URL

#### Optional Flags
- `--version`, `-v`: Server version (default: "1.0.0")
- `--repo-source`: Repository source (default: "github")
- `--output`, `-o`: Output file path (default: "server.json")
- `--execute`, `-e`: Command to execute the server (generates runtime arguments)
- `--registry`: Package registry name (default: "npm")
- `--package-name`: Package name (defaults to server name)
- `--package-version`: Package version (defaults to server version)
- `--runtime-hint`: Runtime hint (e.g., "docker")
- `--env-var`: Environment variable in format NAME:DESCRIPTION (can be repeated)
- `--package-arg`: Package argument in format VALUE:DESCRIPTION (can be repeated)

### Create Examples

#### Basic NPX Server

```bash
./bin/mcp-publisher create \
  --name "io.github.example/my-server" \
  --description "My MCP server" \
  --repo-url "https://github.com/example/my-server" \
  --execute "npx @example/my-server --verbose" \
  --env-var "API_KEY:Your API key for the service"
```

#### Docker Server

```bash
./bin/mcp-publisher create \
  --name "io.github.example/docker-server" \
  --description "Docker-based MCP server" \
  --repo-url "https://github.com/example/docker-server" \
  --runtime-hint "docker" \
  --execute "docker run --mount type=bind,src=/data,dst=/app/data example/server" \
  --env-var "CONFIG_PATH:Path to configuration file"
```

#### Server with Package Arguments

```bash
./bin/mcp-publisher create \
  --name "io.github.example/full-server" \
  --description "Complete server example" \
  --repo-url "https://github.com/example/full-server" \
  --execute "npx @example/server" \
  --package-arg "-s:Specify services and permissions" \
  --package-arg "--config:Configuration file path" \
  --env-var "API_KEY:Service API key" \
  --env-var "DEBUG:Enable debug mode"
```

The `create` command will generate a `server.json` file with:
- Proper structure and formatting
- Runtime arguments parsed from the `--execute` command
- Environment variables with descriptions
- Package arguments for user configuration
- All necessary metadata

After creation, you may need to manually edit the file to:
- Adjust argument descriptions and requirements
- Set environment variable optionality (`is_required`, `is_secret`)
- Add remote server configurations
- Fine-tune runtime and package arguments

## Authentication

The tool has been simplified to use **GitHub OAuth device flow authentication exclusively**. Previous versions supported multiple authentication methods, but this version focuses solely on GitHub OAuth for better security and user experience.

1. **Automatic Setup**: The tool automatically retrieves the GitHub Client ID from the registry's health endpoint
2. **First Run Authentication**: When first run (or with the `--login` flag), the tool initiates the GitHub device flow
3. **User Authorization**: You'll be provided with a URL and a verification code to enter on GitHub
4. **Token Storage**: After successful authentication, the tool saves the access token locally in `.mcpregistry_token` for future use
5. **Secure Communication**: The token is sent in the HTTP Authorization header with the Bearer scheme for all registry API calls

**Note**: Authentication is performed via GitHub OAuth App, which you must authorize for the respective resources (e.g., organization access if publishing organization repositories).

## Publishing Example

To publish an existing server.json file to the registry:

1. Prepare your `server.json` file with your server details:

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
./bin/mcp-publisher publish --registry-url "https://mcp-registry.example.com" --mcp-file "./server.json"
```

3. Follow the authentication instructions in the terminal if prompted.

4. Upon successful publication, you'll see a confirmation message.

## Important Notes

- **GitHub Authentication Only**: The tool exclusively uses GitHub OAuth device flow for authentication
- **Automatic Client ID**: The GitHub Client ID is automatically retrieved from the registry's health endpoint
- **Token Storage**: The authentication token is saved in `.mcpregistry_token` in the current directory
- **Internet Required**: Active internet connection needed for GitHub authentication and registry communication
- **Repository Access**: Ensure the repository and package mentioned in your `server.json` file exist and are accessible
- **OAuth Permissions**: You may need to grant the OAuth app access to your GitHub organizations if publishing org repositories
