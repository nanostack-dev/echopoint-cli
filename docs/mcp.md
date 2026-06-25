# MCP Server

`echopoint mcp` runs a [Model Context Protocol](https://modelcontextprotocol.io)
server over stdio, exposing echopoint operations as tools that any MCP-compatible
AI client (Claude Desktop, Cursor, etc.) can call. The AI client owns the
conversation loop; each tool call is dispatched through the CLI's authenticated
API client, so the AI can do only what you can do.

## How tools are defined

Tools are not hand-written. They are derived from the OpenAPI contract: any
operation annotated with `x-ai-tool: true` becomes a tool, and its parameters and
JSON request body are merged into a single argument schema. Adding a tool is a
spec annotation — nothing in the CLI changes.

```yaml
# in openapi.yaml
post:
  operationId: launchFlow
  x-ai-tool: true
  x-ai-description: "Run a flow by id. Use when the user asks to run or test a flow."
```

`x-ai-danger: true` excludes an operation from the MCP surface (e.g. deletes),
even if it is otherwise annotated.

## Authentication

The server uses the same credentials as every other command:

- **Browser login** (recommended): run `echopoint auth login` once. The server
  reuses the stored session.
- **API key**: set `ECHOPOINT_API_KEY` (and `ECHOPOINT_ORGANIZATION_ID`).

The server refuses to start without one of these.

## Claude Desktop

Add to `claude_desktop_config.json`
(`~/Library/Application Support/Claude/claude_desktop_config.json` on macOS):

```json
{
  "mcpServers": {
    "echopoint": {
      "command": "echopoint",
      "args": ["mcp"]
    }
  }
}
```

This relies on a prior `echopoint auth login`. To run with an API key instead:

```json
{
  "mcpServers": {
    "echopoint": {
      "command": "echopoint",
      "args": ["mcp"],
      "env": {
        "ECHOPOINT_API_KEY": "<your key>",
        "ECHOPOINT_ORGANIZATION_ID": "<your org id>"
      }
    }
  }
}
```

Restart Claude Desktop; the echopoint tools appear in the tool picker.

## Profiles and API URL

The standard flags apply: `--profile`, `--api-url`, `--api-key`. To point a
client at a non-default environment, add them to `args` (e.g.
`"args": ["mcp", "--profile", "staging"]`).

## Tools

Most echopoint operations are exposed — flows, collections, webhooks, requests,
folders, environments, schedules, executions, generation, plus
`get_current_api_key` (the authenticating key's org + permissions).

Every operation in the contract declares its intent explicitly: `x-ai-tool`
(exposed) or `x-ai-danger` with a reason (deliberately excluded). The excluded
set:

- **API key management** — an API-key principal minting/revoking keys is privilege escalation
- **Runner protocol** — machine-to-machine job claim/complete, not an agent action
- **SSE streams** — long-lived, incompatible with request/response tools
- **Public webhook ingestion**, **admin routes**, `/me`, `/init`

The tool set tracks the contract: annotate an operation `x-ai-tool` (and resync)
to expose it.
