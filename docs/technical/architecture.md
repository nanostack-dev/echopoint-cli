# Implemented architecture

`cmd/echopoint` initializes the Cobra command tree in `internal/commands`. Commands resolve profile/configuration and credentials, then use `internal/client` and the generated OpenAPI client in `internal/api`. The embedded [OpenAPI contract](../../internal/api/openapi.yaml) also supplies request schemas to the monitor and status-page commands. `internal/flowfile` checks the integration nodes of a flow file against the workspace catalog for `flow compile` and `flow create/update -f`. The CLI has no MCP server: the Echopoint API serves it ([MCP](../mcp.md), [ADR-0001](../adr/0001-remove-stdio-mcp-server.md)).

Local `flow run` embeds `echopoint-runner/pkg/jobrunner` and `pkg/spi`. Spec operations reuse `echopoint-kit/apispec`. Their exact versions are pinned in [go.mod](../../go.mod), so development builds need no sibling checkout. Browser login and profile credentials are handled by `internal/auth`; output formatting is in `internal/output`.

The root [action.yml](../../action.yml) downloads a released CLI and invokes its stable flow-run contract. Release automation, Action compatibility and API regeneration are described in [integration contracts](integration-contracts.md). User-facing procedures stay in the capability docs linked by [the index](../README.md).
