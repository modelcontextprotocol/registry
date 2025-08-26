# MCP Registry Roadmap

This is a high-level roadmap for the MCP Registry. It is subject to change and not exhaustive, but it outlines the general thinking of the sequencing and scope of our work in this repository.

This roadmap may occasionally drift out of date. Please review [Issues](https://github.com/modelcontextprotocol/registry/issues) (and corresponding Labels) for the most current work in progress.

## Current Status

The initial version of the MCP Registry is actively being developed. The initial focus is on delivering a REST API to which server creators can publish, and aggregator/marketplace consumers can ETL.

## Phase 1: MVP/Go-Live (Current Focus)

See the [go-live blocker issues](https://github.com/modelcontextprotocol/registry/issues?q=is%3Aissue%20state%3Aopen%20label%3A%22go-live%20blocker%22).

## Backlog (Future Work, may be moved to out of scope)

- [ ] UI implementation
- [ ] Store and surface other data besides servers (e.g. [clients](https://modelcontextprotocol.io/clients), resources)
- [ ] Download count tracking
- [ ] Internationalization (i18n)

## Out of Scope (Not Planned)

- **Source code hosting**: The registry will never host actual server code
- **Quality rankings**: No built-in server quality assessments or rankings
- **Curation**: No editorial decisions about which servers are "better"
- **Unified runtime**: Not solving how servers are executed
- **Server hosting**: The registry does not provide hosting for servers
- **Search engine**: The registry will not provide a commercial grade search engine for servers
- **Server tags or categories**: Not supported, to reduce moderation burden
- **Server rankings**: The registry will not rank servers by subjective measures of quality
