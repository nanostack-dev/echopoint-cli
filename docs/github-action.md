# Running flows in CI: `echopoint flows run` and the GitHub Action

Run Echopoint flows from any CI/CD system as **ephemeral runner** executions. The flow runs
on your CI worker (not on Echopoint cloud-runner capacity); Echopoint resolves the flow
definition and environment inputs and returns them on the launched execution
(`flow_snapshot` / `runner_inputs` / `referenced_flows`), and records the result you publish.
The CLI flow is: launch → run the runner from the execution → complete.

## Required API key scopes

Create an organization API key with exactly these two scopes:

| Scope | Why it is needed |
|-------|------------------|
| `flows:execute` | Launch an ephemeral execution **and** receive its runnable data: the immutable flow definition (`flow_snapshot`), referenced flows, and the resolved execution inputs/env (`runner_inputs`) for the created execution. |
| `runner:complete` | Publish the runner result for that execution via `POST /runner/ephemeral/executions/{executionId}/complete`. |

`runner:claim` is **not** required for the ephemeral CI path — the action proactively launches a
known flow rather than claiming arbitrary queued work.

### Secret boundary (read this)

For `runner_type = ephemeral`, `flows:execute` grants the right to receive the **resolved
inputs/env** (`runner_inputs`) on the execution it creates — including any secrets referenced by the
selected environment. Those values are delivered to the CI worker so the flow can run locally. They
are:

- present only on the launched execution on the worker,
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
| `api-key` | yes | — | API key with `flows:execute` + `runner:complete`. |
| `organization-id` | yes | — | Echopoint organization ID. |
| `flow-id` | one of | — | A single flow ID. Mutually exclusive with `flow-ids`. |
| `flow-ids` | one of | — | Comma- or newline-separated flow IDs. Mutually exclusive with `flow-id`. |
| `environment` | no | — | Environment key for resolved inputs/env. |
| `version-id` | no | current | Immutable flow version to run. Applies to every requested flow. |
| `cli-version` | no | `latest` | `echopoint` CLI release to download. |
| `runner-version` | no | = `cli-version` | `echopoint-runner` release to download. |
| `runner-binary` | no | — | Path to a pre-installed runner; skips the runner download. |
| `poll-timeout` | no | `300` | Max seconds to wait for all flows. |
| `parallel` | no | `1` | Number of flows to run concurrently (must be ≥ 1). |

Exactly one of `flow-id` or `flow-ids` is required; `parallel >= 1` is validated **before** any
launch.

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
    runner-version: v1.2.0
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
  --runner-binary "$(which echopoint-runner)" \
  --parallel 1 \
  --poll-timeout 5m \
  -o json
```

Flags: `--environment`, `--version-id`, `--runner-binary`, `--idempotency-key`,
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

- **Matching key + same scope, execution not terminal** → the original (still runnable) execution
  is returned and the flow runs once.
- **Matching key + same scope, execution already terminal** → the existing terminal execution is
  returned; the CLI reports the existing result and does **not** re-run the flow.
- **Same key reused with different scoped parameters** (different flow, environment, version,
  trigger, or runner type) → `409 Conflict`.

For multiple flow IDs, an explicit or CI-derived key is treated as a *base* key; the CLI derives
a stable per-flow key so each launch retries safely. There is no separate active lease in v1:
idempotency prevents duplicate execution records, but a CI system that starts two workers for the
same pending execution before cancelling the first may run the flow twice — avoid concurrent
duplicate workers when side effects matter.
