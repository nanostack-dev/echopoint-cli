# MCP Server

`echopoint mcp` runs a [Model Context Protocol](https://modelcontextprotocol.io)
server over stdio, exposing echopoint operations as tools that any MCP-compatible
AI client (Claude Desktop, Cursor, etc.) can call. The AI client owns the
conversation loop; each tool call is dispatched through the CLI's authenticated
API client, so the AI can do only what you can do.

## How tools are defined

Tools are derived from eligible operations in the OpenAPI contract. An operation
annotated with `x-ai-tool: true` becomes a tool unless it is marked `x-ai-danger`
or its path is `/administrations` or a descendant. Its parameters and JSON request
body are merged into a single argument schema. Eligible tools need only a spec
annotation and contract resync.

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
folders, environments, monitors, executions, generation, execution analytics
(`get_execution_analytics_summary`, `list_flow_analytics`,
`list_endpoint_analytics`), plus `get_current_api_key` (the authenticating key's
org + permissions).

`create_flow` and `update_flow` take an optional `role` (`standalone` or `module`; an update that omits it keeps the current role). `create_flow_schedule`, `update_flow_schedule` and `preview_schedule_tags` take an optional `ignore_modules` boolean: monitors skip Module flows unless it is false.

Every operation in the contract declares its intent explicitly: `x-ai-tool`
(exposed) or `x-ai-danger` with a reason (deliberately excluded). The excluded
set:

- **API key management** — an API-key principal minting/revoking keys is privilege escalation
- **Runner protocol** — machine-to-machine job claim/complete, not an agent action
- **SSE streams** — long-lived, incompatible with request/response tools
- **Public webhook ingestion**, **admin routes**, `/me`, `/init`

Product administration, including Cloud fleet settings, belongs only to the
browser administration panel. MCP always excludes `/administrations` and
`/administrations/*`, even if an operation is accidentally annotated as safe.
`get_cloud_fleet` and `update_cloud_fleet` are not tools and there are no matching
CLI administration commands. The complete API schema and generated client are
retained internally; they do not grant tool exposure. See
[product administration scope](technical/administration-scope.md).

The ordinary tool set tracks the contract: annotate an eligible operation
`x-ai-tool` (and resync) to expose it. Administration routes remain excluded.

## Monitors

The contract exposes the monitor operations under their unchanged operation names:
`list_flow_schedules`, `create_flow_schedule`, `get_flow_schedule`, `update_flow_schedule`,
`delete_flow_schedule`, `pause_flow_schedule`, `resume_flow_schedule`,
`list_flow_schedule_runs`, `get_flow_schedule_run` and `preview_schedule_tags`, plus
`run_monitor` and `estimate_monitor`. Their paths are `/monitors/...`; path arguments
use `monitorId` and `runId`. Create/update take the monitor fields at the top level:
exactly one of `flow_ids` or `tags` (with `tag_match_mode`), a `cron_expression`, or
`interval_seconds` with a `health_policy`. There is no `expected_revision`: the last
write wins.

`run_monitor` is a diagnostic run, only for a monitor with a health policy, and never
confirms health; scheduled runs do. `estimate_monitor` forecasts executions before a
save. `get_monitor_cloud_budget` serves the web app only and is not a tool, and the
probe tools no longer exist. [`docs/monitors.md`](monitors.md) describes the workflow.
Every call retains the selected organization and authenticated API permissions.

## Status pages

`get_status_page` and `save_status_page` manage the private draft. Publication and
withdrawal remain excluded by the API contract; use the explicit
[`status-page` CLI commands](status-pages.md) for those actions and anonymous
public verification.

`save_status_page` also accepts `monitor_bindings` using `service_id` and
`monitor_id`; only an enabled monitor with a health policy can be bound. Find one with
`list_flow_schedules` (`has_health_policy`). Optional `config.services[].messages`
supplies plain public text for `operational`, `outage` and `unknown` states. The server
freezes messages and monitor revisions when publishing; raw diagnostic evidence is not
part of the anonymous projection. `get_status_page_binding_options` no longer exists.
