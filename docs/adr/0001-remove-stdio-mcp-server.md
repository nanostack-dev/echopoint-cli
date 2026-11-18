# 0001. Remove the stdio MCP server

Status: accepted, 2026-11-18

## Context

`echopoint mcp` ran a Model Context Protocol server over stdio. It derived its tools from `x-ai-tool` operations in the embedded OpenAPI contract, plus a hand-written `build_flow` tool. Every user had to install the CLI and configure a local process. Echopoint is moving to one remote MCP server inside the Echopoint API (Streamable HTTP, Clerk OAuth), decided in [echopoint ADR-0031](https://github.com/nanostack-dev/echopoint/pull/876). Echopoint has no external users yet, so no client depends on the stdio server.

## Decision

Remove the stdio MCP server and the `echopoint mcp` command now. All other commands stay. The MCP surface lives in the API's remote endpoint (dev: `https://apidev.echopoint.dev/mcp`; production endpoint to follow), added with `claude mcp add --transport http echopoint <url>`.

The last CLI commit that contained the server is `8e43e0a` (`feat(api): call any EchoPoint endpoint with the CLI's credentials (#107)`). Use it as the source for the API port: `internal/mcp/` (catalog, `build_flow` and its schema, integration catalog checks) and `internal/commands/mcp.go`.

The integration-catalog check that `flow compile` and `flow create/update -f` use was not MCP-only. It moved to `internal/flowfile`, which has no MCP SDK import.

## Alternatives

- Keep both servers until the remote one ships. Rejected: two surfaces to keep in step, with no users to protect.
- Keep the stdio server as a thin proxy to the remote endpoint. Rejected: the remote endpoint serves clients directly.

## Consequences

- Breaking change: `echopoint mcp` no longer exists (`feat!`).
- The dependencies `modelcontextprotocol/go-sdk` and `google/jsonschema-go` leave `go.mod`.
- `auth.SessionSource` and `client.WithTokenSource` served only the long-lived stdio server and have no production caller now.
- The embedded OpenAPI contract stays: the monitor and status-page commands read it.
- `x-ai-tool` and `x-ai-danger` annotations are owned by the API's remote MCP server.
