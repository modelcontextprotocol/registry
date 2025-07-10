# server.json file

There are a variety of use cases where a _static representation of an MCP server_ is necessary:
- Discoverability on a centralized registry (i.e. our official Registry work)
- Discoverability on a decentralized `.well-known` endpoint
- As a response to an initialization call, so the client knows information about the MCP server to which it is connecting
- As an input into crafting a [DXT file](https://www.anthropic.com/engineering/desktop-extensions)
- Packaged in with the source code of an MCP server, so as to have a structured way to identify a server given just its source code

All of these scenarios (and more) would benefit from an agreed-upon, standardized format that makes it easy to port around and maintain a consistent experience for consumers and developers working with the data. At the end of the day, it's all the same data (or a subset of it). MCP server maintainers should have to manage one copy of this file, and all these use cases can serve that file (or a programmatic derivative/subset of it).

Please note: this is different from the file commonly referred to as `mcp.json`, which is _an MCP client's configuration file for **running** a specific set of MCP servers_. See [this issue](https://github.com/modelcontextprotocol/modelcontextprotocol/issues/292).

References:
- [schema.json](./schema.json) - The official JSON schema specification for this representation
- [examples.md](./examples.md) - Example manifestations of the JSON schema
- [registry-schema.json](./registry-schema.json) - A more constrained version of `schema.json` that the official registry supports
