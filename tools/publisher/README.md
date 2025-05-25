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

### Required Environment Variable

Before using the GitHub authentication, you need to set the following environment variable:

```bash
export MCP_REGISTRY_GITHUB_CLIENT_ID="your_github_client_id"
```

This environment variable is required for the GitHub device flow authentication. If not set, the authentication will fail.

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
# First, set the required environment variable
export MCP_REGISTRY_GITHUB_CLIENT_ID="your_github_client_id"

# Then run the publisher tool
./bin/mcp-publisher --registry-url "https://mcp-registry.example.com" --mcp-file "./mcp.json"
```

3. Follow the authentication instructions in the terminal if prompted.

4. Upon successful publication, you'll see a confirmation message.

## Important Notes

- The `MCP_REGISTRY_GITHUB_CLIENT_ID` environment variable must be set for GitHub authentication
- The authentication token is saved in a file named `.mcpregistry_token` in the current directory
- The tool requires an active internet connection to authenticate with GitHub and communicate with the registry
- Make sure the repository and package mentioned in your `mcp.json` file exist and are accessible

"""
Copyright (c) 2025 DevRev, Inc.
SPDX-License-Identifier: MIT

This module implements the MCP server for DevRev integration.
"""

import asyncio
import os
import requests

from mcp.server.models import InitializationOptions
import mcp.types as types
from mcp.server import NotificationOptions, Server
from pydantic import AnyUrl
import mcp.server.stdio
from .utils import make_devrev_request
from .utils import make_internal_devrev_request

server = Server("devrev_mcp")

@server.list_tools()
async def handle_list_tools() -> list[types.Tool]:
    """
    List available tools.
    Each tool specifies its arguments using JSON Schema validation.
    """

    return [
        types.Tool(
            name="get_current_user",
            description="Fetch the current DevRev user’s ID and name. Use this id to set the `owned_by` field when creating or updating an issue or ticket.",
            inputSchema={"type": "object", "properties": {}},
        ),

        types.Tool(
            name="infer_code_changes",
            description="Get git diff for a given repo path. Can fetch diff from main or just latest commit depending on the mode.",
            inputSchema={
                "type": "object",
                "properties": {
                    "repo_path": {
                        "type": "string",
                        "description": "Absolute path to the local git repository"
                    },
                    "mode": {
                        "type": "string",
                        "description": "Mode of operation: 'progress_update' for diff from last commit, 'object_summary' for diff from main",
                        "enum": ["progress_update", "object_summary"]
                    }
                },
                "required": ["repo_path", "mode"]
            },
        ),

        types.Tool(
            name="find_relevant_part",
            description="Search for a DevRev Part based on repo_name. Use this to determine the `applies_to_part` when creating or updating a work object.",
            inputSchema={
                "type": "object",
                "properties": {
                    "repo_name": {"type": "string", "description": "The name of the repository to search for."},
                },
                "required": ["repo_name"],
            },
        ),

        types.Tool(
            name="search",
            description="Search DevRev for work objects, articles, parts, or users using a text query. Use this to look up existing records.",
            inputSchema={
                "type": "object",
                "properties": {
                    "query": {"type": "string"},
                    "namespace": {
                        "type": "string",
                        "enum": ["article", "issue", "ticket", "part", "dev_user"],
                        "description": "The namespace to search in. Use this to specify the type of object you want to search for."
                    },
                },
                "required": ["query", "namespace"],
            },
        ),

        types.Tool(
            name="get_object",
            description="Fetch complete details for a DevRev issue or ticket using its unique ID. Use this when you need full context about a specific work object.",
            inputSchema={
                "type": "object",
                "properties": {"id": {"type": "string", "description": "The ID of the issue or ticket to get the details for."}},
                "required": ["id"],
            },
        ),

        types.Tool(
            name="create_object",
            description="Create a new DevRev issue or ticket. Whenever the user says 'create issue/ticket' without any title or body specified, call infer_code_changes tool (mode: 'object_summary') to get the title and body from code changes. Set the other fields carefully based on their description, you may need to call other tools for that.",
            inputSchema={
                "type": "object",
                "properties": {
                    "type": {"type": "string", "enum": ["issue", "ticket"]},
                    "title": {"type": "string", "description": "read the tool description to understand how to set this field."},
                    "body": {"type": "string", "description": "read the tool description to understand how to set this field."},
                    "applies_to_part": {"type": "string", "description": "The part ID to associate the issue or ticket with. When the part ID is not provided in query, the tool will use the find_relevant_part tool to generate a part ID."},
                    "owned_by": {"type": "array", "items": {"type": "string"}, "description": "The list of user IDs to associate the issue or ticket with. When the owner is not provided in query, the tool will use the get_current_user tool to generate a list of user IDs."},
                    "sprint": {"type": "string", "description": "The sprint ID to associate the issue or ticket with. When the sprint is not provided in query, the tool will use the get_sprints tool to generate a sprint ID. Keep this field empty string if the type is ticket or if you don't find a sprint."},
                },
                "required": ["type", "title", "body", "applies_to_part", "owned_by", "sprint"],
            },
        ),

        types.Tool(
            name="update_object",
            description="Update an existing issue or ticket. Whenever the user says 'update issue/ticket' without any title or body specified, call infer_code_changes tool (mode: 'object_summary') to get the title and body from code changes.",
            inputSchema={
                "type": "object",
                "properties": {
                    "type": {"type": "string", "enum": ["issue", "ticket"], "description": "This field is mandatory for API call, this is not settable by the user."},
                    "id": {"type": "string", "description": "This field is mandatory for API call, this is not settable by the user."},
                    "title": {"type": "string", "description": "read the tool description to understand how to set this field."},
                    "body": {"type": "string", "description": "read the tool description to understand how to set this field."},
                    "applies_to_part": {"type": "string", "description": "The part ID to associate the issue or ticket with."},
                    "owned_by": {"type": "array", "items": {"type": "string"}, "description": "The list of user IDs to associate the issue or ticket with."},
                    "stage": {"type": "string", "description": "The stage ID to associate the issue or ticket with. use valid_stage_transitions tool to see if the transition is valid."},
                    "sprint": {"type": "string", "description": "The sprint ID to associate the issue or ticket with."},
                },
                "required": ["id", "type"],
            },
        ),
        types.Tool(
            name="add_timeline_entry",
            description="This tool is purely used to add timeline entry of code-changes from the latest commit and NOT about changes in the object (issue or ticket). MUST call infer_code_changes tool (mode: 'progress_update') before this tool to get the latest changes in code and update the timeline entry accordingly. Recommended flow: call infer_code_changes tool, then call this tool to update the timeline entry.",
            inputSchema={
                "type": "object",
                "properties": {
                    "id": {"type": "string"},
                    "timeline_entry": {"type": "string", "description": "The timeline entry to add regrading the progress of work (purely about code changes) on the issue or ticket. Use infer_code_changes tool to get the latest changes in code and update the timeline entry accordingly."},
                },
                "required": ["id", "timeline_entry"],
            },
        ),
        types.Tool(
            name="valid_stage_transitions",
            description="List all valid stage transitions for the current stage of a work object. Use this before changing an issue or ticket’s stage.",
            inputSchema={
                "type": "object",
                "properties": {"id": {"type": "string", "description": "The ID of the issue or ticket to get the valid stage transitions for."}},
                "required": ["id"],
            },
        ),

        types.Tool(
            name="get_sprints",
            description="Retrieve active or planned sprints for a given DevRev part. Use this to associate a work object with an active sprint.",
            inputSchema={
                "type": "object",
                "properties": {
                    "ancestor_part_id": {"type": "string", "description": "The ID of the part to get the sprints for."},
                    "state": {
                        "type": "string",
                        "enum": ["active", "planned"],
                        "description": "The state of the sprints to get. When the state is not provided in query, the tool will get the active sprints."
                    },
                },
                "required": ["ancestor_part_id"],
            },
        )
    ]

@server.call_tool()
async def handle_call_tool(
    name: str, arguments: dict | None
) -> list[types.TextContent | types.ImageContent | types.EmbeddedResource]:
    """
    Handle tool execution requests.
    Tools can modify server state and notify clients of changes.
    """
    if name == "search":
        if not arguments:
            raise ValueError("Missing arguments")

        query = arguments.get("query")
        if not query:
            raise ValueError("Missing query parameter")
        
        namespace = arguments.get("namespace")
        if not namespace:
            raise ValueError("Missing namespace parameter")

        response = make_devrev_request(
            "search.hybrid",
            {"query": query, "namespace": namespace}
        )
        if response.status_code != 200:
            error_text = response.text
            return [
                types.TextContent(
                    type="text",
                    text=f"Search failed with status {response.status_code}: {error_text}"
                )
            ]
        
        search_results = response.json()
        return [
            types.TextContent(
                type="text",
                text=f"Search results for '{query}':\n{search_results}"
            )
        ]

    elif name == "infer_code_changes":
        import subprocess
        repo_path = arguments["repo_path"]
        mode = arguments.get("mode", "object_summary")

        try:
            if mode == "progress_update":
                # Check if HEAD has a parent commit
                try:
                    subprocess.check_output(["git", "rev-parse", "HEAD^"], cwd=repo_path, stderr=subprocess.STDOUT)
                    head_has_parent = True
                except subprocess.CalledProcessError:
                    head_has_parent = False

                if not head_has_parent:
                    return [ types.TextContent(type="text", text="No progress changes to add to timeline entry (only one commit exists).") ]

                diff_cmd = ["git", "diff", "HEAD^", "HEAD"]

            elif mode == "object_summary":
                diff_cmd = ["git", "diff", "main...HEAD"]

            else:
                return [ types.TextContent(type="text", text=f"Invalid mode: {mode}. Supported: 'progress_update', 'object_summary'") ]

            diff_output = subprocess.check_output(
                diff_cmd,
                cwd=repo_path,
                stderr=subprocess.STDOUT
            ).decode("utf-8")

        except subprocess.CalledProcessError as e:
            return [ types.TextContent(type="text", text=f"git diff failed: {e.output.decode()}") ]

        if not diff_output.strip():
            if mode == "progress_update":
                return [ types.TextContent(type="text", text="No progress changes to add to timeline entry.") ]
            else:
                try:
                    branch_name = subprocess.check_output(
                        ["git", "rev-parse", "--abbrev-ref", "HEAD"],
                        cwd=repo_path
                    ).decode("utf-8").strip()
                except subprocess.CalledProcessError:
                    branch_name = "unknown-branch"

                return [ types.TextContent(type="text", text=f"No changes between main and HEAD. Use '{branch_name}' as the title and an empty string as the body.") ]

        return [ types.TextContent(type="text", text=diff_output) ]
    elif name == "find_relevant_part":
        if not arguments:
            raise ValueError("Missing arguments")

        repo_name = arguments.get("repo_name")

        if not repo_name:
            raise ValueError("repo_name is required.")
        
        query_text = f"{repo_name}"

        response = make_devrev_request(
            "search.hybrid",
            {
                "query": query_text,
                "namespace": "part"
            }
        )

        if response.status_code != 200:
            error_text = response.text
            return [
                types.TextContent(
                    type="text",
                    text=f"Search for relevant parts failed with status {response.status_code}: {error_text}"
                )
            ]

        results = response.json()

        return [
            types.TextContent(
                type="text",
                text=f"Relevant parts:\n{results}"
            )
        ]
    elif name == "get_current_user":
        response = make_devrev_request(
            "dev-users.self",
            {}
        )

        if response.status_code != 200:
            error_text = response.text
            return [
                types.TextContent(
                    type="text",
                    text=f"Get current user failed with status {response.status_code}: {error_text}"
                )
            ]

        user_info = response.json()
        user_id = user_info.get("dev_user", {}).get("id")
        user_name = user_info.get("dev_user", {}).get("display_name")
        return [
            types.TextContent(
                type="text",
                text=f"Current DevRev user ID: {user_id}, Name: {user_name}"
            )
        ]
    elif name == "get_object":
        if not arguments:
            raise ValueError("Missing arguments")

        id = arguments.get("id")
        if not id:
            raise ValueError("Missing id parameter")

        response = make_devrev_request(
            "works.get",
            {"id": id}
        )
        if response.status_code != 200:
            error_text = response.text
            return [
                types.TextContent(
                    type="text",
                    text=f"Get object failed with status {response.status_code}: {error_text}"
                )
            ]
        
        object_info = response.json()
        return [
            types.TextContent(
                type="text",
                text=f"Object information for '{id}':\n{object_info}"
            )
        ]
    elif name == "create_object":
        if not arguments:
            raise ValueError("Missing arguments")

        object_type = arguments.get("type")
        if not object_type:
            raise ValueError("Missing type parameter")
    
        applies_to_part = arguments.get("applies_to_part")
        if not applies_to_part:
            raise ValueError("Missing applies_to_part parameter")

        title = arguments.get("title")
        body = arguments.get("body")
        if not title:
            raise ValueError("Missing title parameter")

        owned_by = arguments.get("owned_by")
        if not owned_by:
            raise ValueError("Missing owned_by parameter")

        create_payload = {
            "type": object_type,
            "title": title,
            "body": body,
            "applies_to_part": applies_to_part,
            "owned_by": owned_by
        }
        sprint = arguments.get("sprint")
        if object_type == "issue" and sprint:
            create_payload["sprint"] = sprint

        response = make_devrev_request(
            "works.create",
            create_payload
        )
        if response.status_code != 201:
            error_text = response.text
            return [
                types.TextContent(
                    type="text",
                    text=f"Create object failed with status {response.status_code}: {error_text}"
                )
            ]

        return [
            types.TextContent(
                type="text",
                text=f"Object created successfully: {response.json()}"
            )
        ]
    elif name == "update_object":
        # Update mandatory fields
        if not arguments:
            raise ValueError("Missing arguments")

        id = arguments.get("id")
        if not id:
            raise ValueError("Missing id parameter")
        
        object_type = arguments.get("type")
        if not object_type:
            raise ValueError("Missing type parameter")
        
        title = arguments.get("title")
        body = arguments.get("body")
        stage = arguments.get("stage")
        sprint = arguments.get("sprint")
        owned_by = arguments.get("owned_by")
        applies_to_part = arguments.get("applies_to_part")

        # Build request payload with only the fields that have values
        update_payload = {"id": id, "type": object_type}
        if title:
            update_payload["title"] = title
        if body:
            update_payload["body"] = body
        if stage:
            update_payload["stage"] = {"stage": stage}
        if sprint:
            update_payload["sprint"] = sprint
        if owned_by:
            update_payload["owned_by"] = owned_by
        if applies_to_part:
            update_payload["applies_to_part"] = applies_to_part        
        # Make devrev request to update the object
        response = make_devrev_request(
            "works.update",
            update_payload
        )

        # Check if the request was successful
        if response.status_code != 200:
            error_text = response.text
            return [
                types.TextContent(
                    type="text",
                    text=f"Update object failed with status {response.status_code}: {error_text}"
                )
            ]
        
        return [
            types.TextContent(
                type="text",
                text=f"Object updated successfully: {id}"
            )
        ]
    elif name == "add_timeline_entry":
        if not arguments:
            raise ValueError("Missing arguments")

        id = arguments.get("id")
        if not id:
            raise ValueError("Missing id parameter")
                
        timeline_entry = arguments.get("timeline_entry")
        if not timeline_entry:
            raise ValueError("Missing timeline_entry parameter")
        
        timeline_response = make_devrev_request(
            "timeline-entries.create",
            {
                "type": "timeline_comment",
                "object": id,
                "body": timeline_entry
            }
        )
        if timeline_response.status_code != 201:
            error_text = timeline_response.text
            return [
                types.TextContent(
                    type="text",
                    text=f"Create timeline entry failed with status {timeline_response.status_code}: {error_text}"
                )
            ]
        
        return [
            types.TextContent(
                type="text",
                text=f"Timeline entry created successfully: {timeline_response.json()}"
            )
        ]

    elif name == "valid_stage_transitions":
        if not arguments:
            raise ValueError("Missing arguments")

        id = arguments.get("id")
        if not id:
            raise ValueError("Missing id parameter")
        
        response = make_devrev_request(
            "works.get",
            {"id": id}
        )

        if response.status_code != 200:
            error_text = response.text
            return [
                types.TextContent(
                    type="text",
                    text=f"Get object for Get stage transitions failed with status {response.status_code}: {error_text}"
                )
            ]
        
        current_stage_id = response.json().get("work", {}).get("stage", {}).get("stage", {}).get("id", {})

        stock_schema_frag_id = response.json().get("work", {}).get("stock_schema_fragment", {})
        custom_schema_frag_id = response.json().get("work", {}).get("custom_schema_fragments", [])
        leaf_type = response.json().get("work", {}).get("type", {})

        schema_response = make_internal_devrev_request(
            "schemas.aggregated.get",
            {
             "custom_schema_fragment_ids": custom_schema_frag_id,
             "leaf_type": leaf_type,
             "stock_schema_fragment_id": stock_schema_frag_id
            }
        )

        if schema_response.status_code != 200:
            error_text = schema_response.text
            return [
                types.TextContent(
                    type="text",
                    text=f"Get object schema for Get stage transitions failed with status {schema_response.status_code}: {error_text}"
                )
            ]

        stage_diagram_id = schema_response.json().get("schema", {}).get("stage_diagram_id", {}).get("id", {})
        stage_transitions_response = make_internal_devrev_request(
            "stage-diagrams.get",
            {"id": stage_diagram_id}
        )

        if stage_transitions_response.status_code != 200:
            error_text = stage_transitions_response.text
            return [
                types.TextContent(
                    type="text",
                    text=f"Get stage diagram for Get stage transitions failed with status {stage_transitions_response.status_code}: {error_text}"
                )
            ]

        # Need to iterate over the stages and find the one which matches current stage id and return its transitions
        stages = stage_transitions_response.json().get("stage_diagram", {}).get("stages", [])

        for stage in stages:
            if stage.get("stage", {}).get("id") == current_stage_id:
                transitions = stage.get("transitions", [])
                return [
                    types.TextContent(
                        type="text",
                        text=f"Valid Transitions for '{id}' from current stage:\n{transitions}"
                    )
                ]

        return [
            types.TextContent(
                type="text",
                text=f"No valid transitions found for '{id}' from current stage"
            ),
        ]
    elif name == "get_sprints":
        if not arguments:
            raise ValueError("Missing arguments")

        ancestor_part_id = arguments.get("ancestor_part_id")
        if not ancestor_part_id:
            raise ValueError("Missing ancestor_part_id parameter")

        state = arguments.get("state")
        if not state:
            state = "active"
        
        response = make_internal_devrev_request(
            "vistas.groups.list",
            {
                "ancestor_part": [ancestor_part_id],
                "group_object_type": ["work"],
                "state": [state]
            }
        )
        
        if response.status_code != 200:
            error_text = response.text
            return [
                types.TextContent(
                    type="text",
                    text=f"Get sprints failed with status {response.status_code}: {error_text}"
                )
            ]
        
        sprints = response.json().get("vista_group", [])
        return [
            types.TextContent(
                type="text",
                text=f"'{state}' Sprints for '{ancestor_part_id}':\n{sprints}. Note sprints are only available for issues, not for tickets. While creating a ticket, keep this field empty."
            )
        ]
    else:
        raise ValueError(f"Unknown tool: {name}")

async def main():
    # Run the server using stdin/stdout streams
    async with mcp.server.stdio.stdio_server() as (read_stream, write_stream):
        await server.run(
            read_stream,
            write_stream,
            InitializationOptions(
                server_name="devrev_mcp",
                server_version="0.1.1",
                capabilities=server.get_capabilities(
                    notification_options=NotificationOptions(),
                    experimental_capabilities={},
                ),
            ),
        )
