# Moderation Guidelines

Guidelines for server publishers on the Official MCP Registry.

## TL;DR

We're quite permissive! We only remove illegal content, malware, spam and completely broken servers.

We don't make guarantees about our moderation, and subregistries should take our data "as is", assuming minimal to no moderation.

## Scope

These guidelines apply to the **Official MCP Registry** at `registry.modelcontextprotocol.io`. 

Subregistries may have their own moderation policies. If you have questions about content on a specific subregistry, please contact them directly.

## Disclaimer

We have limited active moderation capabilities, and this is a community supported projects. We largely rely on upstream package registries (like NPM, PyPi, and Docker) or downstream subregistries (like the GitHub MCP Registry) to do more in-depth moderation.

This means there may be content in the registry that should be removed under these guidelines, which we haven't yet removed. You should treat registry data accordingly.

## What We Remove

We'll remove servers that contain:

- Illegal content, which includes obscene content, copyright violations, and hacking tools
- Malware, regardless of intentions
- Spam, especially mass-created servers that disrupt the registry. Examples:
    - The same server being submitted multiple times under different names.
    - The server doesn't do anything but provide a fixed response with some marketing copy.
    - The server description is stuffed with marketing copy, and its implementation is unrelated to its name or description.
- Non-functioning servers

## What We Don't Remove

Generally, we believe in keeping the registry open and pushing moderation to subregistries. We therefore **won't** remove servers that are:

- Low quality or buggy servers
- Servers with security vulnerabilities
- Do the same thing as other servers
- Provide or contain adult content

## How Removal Works

When we remove a server:

- It's set to "deleted" status but remains accessible via the API
- This allows subregistries to remove it from their indexes
- In extreme cases, we may overwrite or erase details of a server, e.g. where the metadata itself is unlawful

## Appeals

Think we made a mistake? Open an issue on our [GitHub repository](https://github.com/modelcontextprotocol/registry) with:
- The ID and name of your server
- Why you believe it doesn't meet the criteria for removal above

## Changes to this policy

We're still learning how to best run the MCP registry! As such, we might end up changing this policy in the future.
