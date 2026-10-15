# Monitors

`echopoint monitor` (alias `monitors`) manages monitors. A monitor runs the flows it
selects, by tag or by explicit flow IDs, on a cadence, in one environment, on Cloud
or a Self-hosted runner. A monitor can carry a **health policy**: it then also
measures health from its scheduled runs, and a status page can publish it. The design
is Echopoint ADR-0020 (a probe is a monitor with a health policy);
the API behaviour is in Echopoint's `docs/technical/monitors.md`. All commands support
`--profile`, `--org`, the normal authentication inputs and `-o table|json|yaml`.

> **Breaking change (API 11.0.0).** The `probe` commands and the `/probes` API are
> removed, and `/flow-schedules` became `/monitors`. A probe is now a monitor with a
> `health_policy`; its ID is the monitor's ID. There is no `probe` alias, and the
> `--expected-revision` flag is gone: the last write wins. This CLI needs an API that
> serves 11.0.0, and an older CLI does not work against it.

## Cadence

| Monitor | Cadence | Floor |
| --- | --- | --- |
| Without a health policy | `cron_expression` | the license floor, never below 15 minutes |
| With a health policy | `interval_seconds` 60 to 86400 | 60 seconds |

`health_policy` is `{confirmation_runs 1..10, recovery_runs 1..10, freshness_seconds 60..604800}`,
and `freshness_seconds` must cover `interval_seconds` plus `timeout_seconds`.
`revision` bumps on every configuration, enable or pause change.

## Commands

```bash
echopoint monitor list --enabled --has-health-policy -o json
echopoint monitor view <monitor-id>
echopoint monitor create -f monitor.yaml
echopoint monitor update <monitor-id> -f monitor.yaml
echopoint monitor pause <monitor-id>
echopoint monitor resume <monitor-id>
echopoint monitor delete <monitor-id>          # asks; -y/--yes skips the question
echopoint monitor run <monitor-id>             # diagnostic run, health-policy monitors only
echopoint monitor runs <monitor-id> --limit 20
echopoint monitor run-view <monitor-id> <run-id>
echopoint monitor estimate -f estimate.yaml
echopoint monitor preview-tags --tag checkout --tag smoke --match-mode all
```

`list` takes `--limit` (1 to 100, default 20), `--offset`, `--enabled` (`--enabled=false`
lists the paused ones) and `--has-health-policy` (`=false` lists the others). A monitor
or run argument is a UUID; anything else fails before any request.
`delete` removes a whole resource, so it asks `Delete monitor <id>? [y/N]` on a
terminal and refuses without one unless `-y/--yes` is given. `pause`, `resume` and
`update` do not ask.

## Create and update

`create -f` and `update <monitor-id> -f` take a JSON or YAML file (`-f -` reads stdin).
The file is checked against `CreateMonitorRequest` or `UpdateMonitorRequest` before
any request: an unknown or read-only field, or a wrong type, fails instead of being
dropped.

```yaml
# monitor.yaml: a health monitor over a tag, every 5 minutes
name: Checkout health
tags: [checkout]
tag_match_mode: any
runner_type: cloud
environment_key: production
interval_seconds: 300
timeout_seconds: 30
health_policy:
  confirmation_runs: 2
  recovery_runs: 2
  freshness_seconds: 900
```

```yaml
# a nightly monitor over explicit flows, without a health policy
name: Nightly regression
flow_ids: [550e8400-e29b-41d4-a716-446655440001]
runner_type: self_hosted
cron_expression: "0 3 * * *"
```

Choose either `flow_ids` or `tags`. `update` replaces the whole mutable configuration,
and a request without `health_policy` removes the policy, so edit the current one:

```bash
echopoint monitor view <monitor-id> -o yaml > monitor.yaml
# keep only request fields: drop id, organization_id, revision, health, next_run_at,
# timezone, created_by, created_at and updated_at, and the empty flow_ids or tags; then edit
echopoint monitor update <monitor-id> -f monitor.yaml
```

Complete examples: [json](../internal/commands/testdata/monitor.json) and
[yaml](../internal/commands/testdata/monitor.yaml).

## Estimate and the Cloud plan

`monitor estimate -f` forecasts executions per occurrence, day and 30 days before you
save, with static request counts when they can be bounded. For Cloud it adds the
organization's daily Cloud execution limit and what its other enabled Cloud monitors
already plan (`monitor_id` in the request leaves the monitor being edited out).
Saving a monitor is never refused for the Cloud plan; runs past the limit are refused
when they launch. The Cloud budget read (`GET /monitors/cloud-budget`) serves the web
app only: it has no CLI command and no MCP tool.

```bash
echo '{"tags":["checkout"],"runner_type":"cloud","interval_seconds":300,"health_policy":true}' \
  | echopoint monitor estimate -f - -o json
```

## Diagnostic runs and history

`monitor run` queues a diagnostic run of a monitor that has a health policy and returns
without waiting. A diagnostic run never confirms health: only scheduled runs do.

```bash
RUN_ID=$(echopoint monitor run <monitor-id> -o json | jq -r .id)
echopoint monitor run-view <monitor-id> "$RUN_ID"
echopoint monitor runs <monitor-id> -o json
```

A run records its `origin` (`scheduled` or `diagnostic`), the revision it fired under,
its status, and, for a health monitor, an `observation` (`state`, `reason`).
`run-view` adds the executions the run launched.

## Connect to a status page

A status page maps a visible service to a monitor with a health policy through
`monitor_bindings`. Only an enabled monitor with a health policy can be bound.

```json
{"monitor_bindings":[{"service_id":"api","monitor_id":"<monitor-id>"}]}
```

See [status pages](status-pages.md). Find the candidates with
`echopoint monitor list --enabled --has-health-policy`. `status-page binding-options`
and the single-assertion `binding` are gone.

## MCP

The embedded contract exposes the monitor operations under their unchanged operation
names: `list_flow_schedules`, `create_flow_schedule`, `get_flow_schedule`,
`update_flow_schedule`, `delete_flow_schedule`, `pause_flow_schedule`,
`resume_flow_schedule`, `list_flow_schedule_runs`, `get_flow_schedule_run`,
`preview_schedule_tags`, plus `run_monitor` and `estimate_monitor`. Path arguments use
`monitorId` and `runId`. `get_monitor_cloud_budget` and every probe tool do not exist.
See [MCP](mcp.md).

## Release

The change is a breaking one for the API, so the CLI ships it as `feat!:`. Release it
only after production serves API 11.0.0; the old CLI against the new API and the new
CLI against the old API both break, and there is no alias.
