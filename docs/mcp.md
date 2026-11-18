# MCP

The CLI no longer ships an MCP server. `echopoint mcp` (stdio) was removed in
[ADR-0001](adr/0001-remove-stdio-mcp-server.md). The Echopoint API serves one
remote MCP server (Streamable HTTP, Clerk OAuth) instead.

For now only the dev endpoint exists, and only dev Echopoint accounts can sign in
to it. The production endpoint follows.

```bash
claude mcp add --transport http echopoint https://apidev.echopoint.dev/mcp
```

Every CLI command (`flow`, `monitor`, `echopoint api`, ...) is unchanged.
