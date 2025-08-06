# Registry API

There are a variety of use cases where an MCP Server Registry API is useful to the MCP ecosystem. At least the following:
- Implementation of a centralized, publicly available catalog of all publicly accessible MCP server implementations
- Implementation of a private catalog of MCP server implementations exclusively accessible by a specific group of people (e.g. a single enterprise)

These scenarios would benefit from a standard "MCP server registry API" specification that they could potentially compose, as well as share resources (like SDK implementations).

The centralized, publicly available catalog ("Official MCP Registry") needs additional constraints that need not apply to the broader "MCP server registry API"

References:
- [openapi.yaml](./openapi.yaml) - A reusable API specification (MCP Server Registry API) that anyone building any sort of "MCP server registry" should consider adopting / aligning with.
- [examples.md](./api_examples.md) - Example manifestations of the OpenAPI specification
- [official-registry-openapi.ayml](./official-registry-openapi.yaml) - The specification backing the Official MCP Registry; a derivative of the MCP Server Registry API specification.