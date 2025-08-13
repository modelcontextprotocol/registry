# Figma Tools

A Model Context Protocol (MCP) server for Figma integration that provides seamless access to Figma design files, components, and assets through AI assistants.

## Features

- 🔍 **File Access**: Browse and search Figma files
- 🖼️ **Image Export**: Extract images from Figma designs
- 📋 **Component Management**: Access and manage Figma components
- 💬 **Comments**: Read and post comments on Figma designs
- 🔧 **Design Tokens**: Extract design tokens and styles
- 🌐 **Cross-platform**: Works on macOS, Linux, and Windows

## Quick Start

### Installation

```bash
pip install figma-mcp-tools
```

### Configuration

1. Get your Figma access token from [Figma Settings](https://www.figma.com/settings)
2. Add the server to your MCP configuration:

```json
{
  "mcpServers": {
    "figma-tools": {
      "command": "figma-mcp-tools",
      "env": {
        "FIGMA_ACCESS_TOKEN": "your_token_here"
      }
    }
  }
}
```

### Usage

Once configured, you can use commands like:
- "Show me the latest designs in my Figma project"
- "Extract all images from this Figma file"
- "What components are available in this design system?"
- "Add a comment to this design"

## Requirements

- Python 3.10+
- Figma access token
- MCP-compatible client (like Cursor, Claude Desktop, etc.)

## Documentation

- [GitHub Repository](https://github.com/DRX-1877/figma-mcp-server)
- [PyPI Package](https://pypi.org/project/figma-mcp-tools/)

## License

MIT License - see [LICENSE](https://github.com/DRX-1877/figma-mcp-server/blob/main/LICENSE) for details.
