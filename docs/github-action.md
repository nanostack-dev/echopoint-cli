# Running flows in CI: `echopoint flows run` and the GitHub Action

Run Echopoint flows from any CI/CD system as **ephemeral runner** executions. The flow runs
on your CI worker. Echopoint creates a one-shot Job for the execution and delivers its resolved
inputs only when the CLI claims that Job. The CLI reports progress, renews the Job lease, and
completes it using the one-Job token. The flow is: launch → claim → run and report.

## Required API key scopes

Create an organization API key with this scope:

| Scope | Why it is needed |
|-------|------------------|
| `flows:execute` | Launch an ephemeral execution and claim its one-shot Job. The claim returns the runnable flow, resolved inputs, and a token scoped to that Job. |

The CLI uses the Job token for events, heartbeats, and completion. It does not need
`runner:claim` or `runner:complete` on the API key.

### Secret boundary (read this)

For `runner_type = ephemeral`, `flows:execute` grants the right to claim the Job and receive its
resolved inputs, including secrets referenced by the selected environment. Those values are
delivered once to the CI worker so the flow can run locally. They are:

- present only in the one-time claim response on the worker,
- never logged by the CLI, runner, or action (the API key is masked via `::add-mask::`),
- never written to `$GITHUB_OUTPUT` or the step summary.

Use a dedicated, least-privilege organization API key for CI and store it as a repository secret.

## GitHub Action

```yaml
- uses: nanostack-dev/echopoint-cli@v1
  with:
    api-key: ${{ secrets.ECHOPOINT_API_KEY }}
    organization-id: ${{ secrets.ECHOPOINT_ORG_ID }}
    flow-id: flow_abc123
```

### Inputs

| Input | Required | Default | Description |
|-------|----------|---------|-------------|
| `api-key` | yes | — | API key with `flows:execute`. |
| `organization-id` | yes | — | Echopoint organization ID. |
| `flow-id` | one of | — | A single flow ID. Mutually exclusive with `flow-ids` / `tags`. |
| `flow-ids` | one of | — | Comma- or newline-separated flow IDs. Mutually exclusive with `flow-id` / `tags`. |
| `tags` | one of | — | Comma- or newline-separated flow tags; flows are selected via flow search. Mutually exclusive with `flow-id` / `flow-ids`. Also requires `flows:read`. |
| `match-mode` | no | `any` | Tag match mode when using `tags`: `any` or `all`. |
| `api-url` | no | CLI default | Echopoint API base URL, e.g. `https://apidev.echopoint.dev`. |
| `environment` | no | — | Environment key for resolved inputs/env. |
| `version-id` | no | current | Immutable flow version to run. Applies to every requested flow. |
| `cli-version` | no | `latest` | `echopoint` CLI release to download. |
| `poll-timeout` | no | `300` | Max seconds to wait for all flows. |
| `parallel` | no | `1` | Number of flows to run concurrently (must be ≥ 1). |

Exactly one of `flow-id`, `flow-ids`, or `tags` is required; `parallel >= 1` is validated
**before** any launch. Selecting by `tags` additionally requires the API key to have `flows:read`
(the flows are resolved through flow search).

### Outputs

| Output | Description |
|--------|-------------|
| `execution-id` | Execution ID for a single-flow run. |
| `execution-ids` | Comma-separated execution IDs for a multi-flow run. |
| `status` | Aggregate status: `completed`, `failed`, or `error`. |
| `success` | `true` only if all flows completed successfully. |
| `results-json` | Raw JSON output from `echopoint flows run`. |

The action captures the CLI exit code, parses JSON outputs, writes `$GITHUB_OUTPUT` and
`$GITHUB_STEP_SUMMARY`, and then exits with the **original CLI exit code**.

### Examples

Default single flow:

```yaml
- uses: nanostack-dev/echopoint-cli@v1
  with:
    api-key: ${{ secrets.ECHOPOINT_API_KEY }}
    organization-id: ${{ secrets.ECHOPOINT_ORG_ID }}
    flow-id: flow_abc123
```

Multiple flows with bounded parallelism:

```yaml
- uses: nanostack-dev/echopoint-cli@v1
  with:
    api-key: ${{ secrets.ECHOPOINT_API_KEY }}
    organization-id: ${{ secrets.ECHOPOINT_ORG_ID }}
    flow-ids: |
      flow_abc123
      flow_def456
      flow_ghi789
    parallel: '2'
```

Run a tagged suite against a specific environment (e.g. a post-deploy smoke gate):

```yaml
- uses: nanostack-dev/echopoint-cli@v1
  with:
    api-key: ${{ secrets.ECHOPOINT_API_KEY }}
    organization-id: ${{ secrets.ECHOPOINT_ORG_ID }}
    api-url: https://apidev.echopoint.dev
    tags: anchor
    match-mode: any
    parallel: '3'
```

Pin an environment and an immutable version:

```yaml
- uses: nanostack-dev/echopoint-cli@v1
  with:
    api-key: ${{ secrets.ECHOPOINT_API_KEY }}
    organization-id: ${{ secrets.ECHOPOINT_ORG_ID }}
    flow-id: flow_abc123
    environment: staging
    version-id: ver_2024_06_01
```

Pin specific binary versions and consume outputs:

```yaml
- id: smoke
  uses: nanostack-dev/echopoint-cli@v1
  with:
    api-key: ${{ secrets.ECHOPOINT_API_KEY }}
    organization-id: ${{ secrets.ECHOPOINT_ORG_ID }}
    flow-id: flow_abc123
    cli-version: v1.2.0
- run: echo "status=${{ steps.smoke.outputs.status }} success=${{ steps.smoke.outputs.success }}"
```

## Using the CLI directly (GitHub Actions or any CI)

The action is a thin wrapper around `echopoint flows run`. On any CI system:

```bash
export ECHOPOINT_API_KEY=...          # or --api-key
export ECHOPOINT_ORGANIZATION_ID=...  # or --organization-id

echopoint flows run flow_abc123 \
  --environment staging \
  --version-id ver_2024_06_01 \
  --parallel 1 \
  --poll-timeout 5m \
  -o json
```

Flags: `--environment`, `--version-id`, `--idempotency-key`,
`--poll-timeout` (default `30m`), `--parallel` (default `1`), `-o json`. Auth comes from
`--api-key` / `ECHOPOINT_API_KEY` and `--organization-id` / `ECHOPOINT_ORGANIZATION_ID`
(API-key auth takes precedence over any Bearer token). When `GITHUB_ACTIONS=true` the CLI
auto-derives `trigger_type=git` provenance (repository, workflow, job, run ID, run attempt, SHA,
ref, actor) and a stable idempotency key.

### Exit codes

| Code | Meaning |
|------|---------|
| `0` | All flows succeeded. |
| `1` | A flow ran and failed. |
| `2` | Cancelled. |
| `3` | API / runner / contract error. |
| `4` | Timeout. |

For multi-flow runs the CLI continues scheduling the remaining flows after a normal flow
failure and exits with the **highest-severity** code across all results.

### JSON output

With `-o json`, stdout is exactly one JSON object (human-readable progress goes to stderr).

Single flow:

```json
{
  "execution_id": "exec_...",
  "flow_id": "flow_...",
  "status": "completed",
  "success": true,
  "exit_code": 0,
  "duration_ms": 5000,
  "error_message": null,
  "nodes": [
    { "node_id": "n1", "display_name": "Login", "node_type": "request", "status": "completed", "duration_ms": 120, "error_message": null }
  ]
}
```

Multiple flows:

```json
{
  "status": "failed",
  "success": false,
  "exit_code": 1,
  "duration_ms": 8200,
  "results": [ { "execution_id": "...", "flow_id": "...", "status": "completed", "success": true, "exit_code": 0, "duration_ms": 5000, "error_message": null, "nodes": [] } ]
}
```

## Idempotency and retries

Ephemeral launch is idempotent so CI retries do not duplicate side-effecting runs. Pass
`--idempotency-key` (or let GitHub Actions derive one). The server scopes idempotency by
organization, flow, environment key, version ID, runner type, trigger type, and the key digest:

- **Matching key + same scope, execution not terminal** → the original execution is returned.
  The CLI claims its Job once; a second claim is refused, so the flow does not run twice.
- **Matching key + same scope, execution already terminal** → the existing terminal execution is
  returned; the CLI reports the existing result and does **not** re-run the flow.
- **Same key reused with different scoped parameters** (different flow, environment, version,
  trigger, or runner type) → `409 Conflict`.

For multiple flow IDs, an explicit or CI-derived key is treated as a *base* key; the CLI derives
a stable per-flow key so each launch retries safely. If a claim response is lost, the CLI exits
without running the flow. Inspect that execution before retrying: the Job may already be claimed.
