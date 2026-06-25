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

## Tools (v1)

| Tool | Operation | Notes |
|------|-----------|-------|
| `get_me` | getMe | Current user/org context |
| `list_flows` | listFlows | |
| `get_flow` | getFlow | |
| `search_flows` | searchFlows | |
| `launch_flow` | launchFlow | Runs a flow asynchronously |
| `list_collections` | listCollections | |
| `get_collection` | getCollection | |
| `list_webhooks` | listWebhooks | |
| `get_current_api_key` | getCurrentAPIKey | Identity (org + permissions) of the authenticating API key |

The set grows by annotating more operations in the contract.
