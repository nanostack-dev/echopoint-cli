# Probes

`echopoint probe` (alias `probes`) manages monitors of type `probe`. A probe
selects a set of flows or a tag query, then runs them with an environment, runner,
cadence and health policy. It uses the ordinary monitor scheduler,
flow execution path and durable history. The server snapshots each occurrence's
sources; creating a probe does not require publishing flows or selecting version
IDs. All commands support the normal `--profile`, `--org` and authentication
inputs. Nested results default to JSON; `-o yaml` uses the same field names.

## Select flows and inspect the forecast

Create flows through the ordinary `flow create -f flow.json` command. Give them
stable tags through `flow tag`; inspect available IDs with `flow list`.

```bash
echopoint --org "$ORG_ID" probe estimate \
  --flow-id "$FIRST_FLOW_ID" --flow-id "$SECOND_FLOW_ID" \
  --environment production --interval 60 -o json

echopoint --org "$ORG_ID" probe estimate \
  --tag production --tag checkout --match-mode all \
  --environment production --interval 60 -o json
```

Use exactly one selector: repeatable `--flow-id` inputs or repeatable `--tag`
inputs. `--match-mode any` is the default; `all` requires every selected tag.
Tags resolve at each occurrence. The estimate is read-only and includes matching
flow IDs, executions and static HTTP requests per occurrence/day/30 days, forecast
executions from now until the calendar month ends, and notes about uncertain
request counts. The month-end forecast is expected usage, not the execution
allowance left on the license. Request totals are omitted when branches, modules,
loops, polling, streams, webhook waits or unknown node types prevent a safe bound
(`requests_exact: false`); that request volume is Variable, and no total is guessed.
Static request counts assume each request node runs once; early failure can reduce
actual calls. An estimate is not a promise of throughput. Each selected flow is a
separate execution. Two flows requested every
60 seconds forecast 2,880 executions per day and 86,400 over 30 days, before
capacity, pauses and execution limits.

```bash
echopoint --org "$ORG_ID" probe create --name "Checkout availability" \
  --tag production --tag checkout --match-mode all \
  --environment production --interval 60 --runner cloud -o json > created.json
PROBE_ID=$(jq -r .id created.json)
echopoint --org "$ORG_ID" probe view "$PROBE_ID" -o json
echopoint --org "$ORG_ID" probe list --limit 20 --offset 0 -o json
```

Flag creation defaults to enabled, Cloud, 60-second cadence, 30-second execution
timeout, two confirming/recovery occurrences, and freshness of at least 180
seconds or interval plus timeout. Add `--paused` to configure without scheduling.
Each probe reports one health result from all selected flows and their executed
assertions. Use separate probes with dedicated flows for separate outcomes.
`--runner self_hosted` selects a customer-operated runner.
The effective cadence remains subject to execution budgets and capacity.

## Complete JSON configuration

[Example input](../internal/commands/testdata/probe.json) is a complete
`CreateProbeRequest`. Replace its flow IDs and environment before sending it:

```bash
echopoint probe validate -f probe.json
echopoint --org "$ORG_ID" probe create -f probe.json --environment production -o json
```

`-f -` reads stdin. JSON uses `config.flow_ids` **or** `config.tags` with optional
`tag_match_mode`; both nonempty selectors are refused. Create, update and
validation accept selector flags to replace the file's selector, while preserving
its health policy. An explicit `--environment` overrides the file, including
an empty value allowing each flow's saved target environment (or organization
base variables when no flow default is saved). Other convenience creation
flags cannot be mixed with `--file`; edit those fields in the complete file.
Organization selection stays in `--org`, never in JSON. Probe/run IDs are
server-assigned KSUIDs; flow and monitor IDs are UUIDs. `probe view` includes the
backing `schedule_id`, and runs include their `schedule_run_id` and execution
references.

The execution path resolves the latest published source when available and the
current flow definition otherwise, and records immutable execution snapshots.
Environment values resolve at launch through the normal encrypted-input path.
Offline validation checks schema and selector shape; the server validates tenant
ownership, environments and execution limits. Probes use normal flow execution:
assertions run at each reached node, and a failed assertion can prevent later
assertions or dependent nodes from running. There is no separate assertion
selection or dependency policy in probe configuration.

## Edit, pause, resume and delete

Read the current revision and complete config before replacing it:

```bash
echopoint --org "$ORG_ID" probe view "$PROBE_ID" -o json \
  | jq '{expected_revision: .revision, config}' > update.json
# Edit the complete config; selector flags may replace its flow/tag selection.
echopoint probe validate --update -f update.json
echopoint --org "$ORG_ID" probe update "$PROBE_ID" -f update.json --tag production -o json

echopoint --org "$ORG_ID" probe pause "$PROBE_ID" --expected-revision 2 -o json
echopoint --org "$ORG_ID" probe resume "$PROBE_ID" --expected-revision 3 -o json
echopoint --org "$ORG_ID" probe delete "$PROBE_ID" --expected-revision 4 --yes -o json
```

Use each action's returned revision for the next action. A 409 conflict requires
reloading and reconciling; the CLI does not retry. Editing, pause and resume reset
revision-compatible measurements. Creating, editing and resuming recurring
execution requires both `flows:update` and `flows:execute`; diagnostics require
`flows:execute`, and reads require `flows:read`. Deletion asks on a terminal and
requires `--yes` noninteractively. Pause, resume and update do not ask.

## Diagnostics and health

```bash
echopoint --org "$ORG_ID" probe run "$PROBE_ID" --expected-revision 4 -o json > run.json
RUN_ID=$(jq -r .id run.json)
echopoint --org "$ORG_ID" probe execution view "$PROBE_ID" "$RUN_ID" -o json
echopoint --org "$ORG_ID" probe history "$PROBE_ID" --limit 20 -o json
```

`run` queues a real private diagnostic and returns without waiting. It refuses a
stale revision and shares the monitor's overlap gate. Diagnostics never confirm,
clear or extend public health. History retains origin, revision, flow/execution
references and sanitized assertion evidence. Scheduled occurrences alone
establish health. A conclusive assertion failure is Outage. When no assertion
failure is recorded, incomplete, blocked, unavailable, expired or incompatible
evidence remains Unknown; successful normal flow completion is Operational.
Ordinary skipped branches do not make a completed flow Unknown. A paused probe
is Not monitored. The saved probe's `health` and each run's `observation` are single
objects. Confirmation, recovery and
freshness remain probe policies rather than ordinary monitor success/failure.
The status evaluator refreshes the public projection every 30 seconds. A probe
pause or policy change can therefore take an evaluation cycle to appear as
Unknown publicly; anonymous reads do not execute probes or evaluate their
private configuration.

## Connect to a status page

Read the shared draft and retain its slug and complete configuration:

```bash
echopoint --org "$ORG_ID" status-page view -o json \
  | jq '{expected_draft_version: .draft_version, slug, config, binding, probe_bindings} \
        | with_entries(select(.value != null))' > page.json
```

Add a mapping to the complete `SaveStatusPageRequest`, using a visible service ID
and the probe ID:

```json
{"probe_bindings":[{"service_id":"api","probe_id":"<probe-id>"}]}
```

Up to 32 mappings are supported. Preserve an existing legacy `binding` when
retaining it. Publication approves source revisions server-side; client mappings
do not accept revision overrides.

Optional public wording belongs to the status-page service. Include it in the
complete service configuration, alongside its existing ID, name and design fields:

```json
{"messages":{"operational":"Checkout is available","outage":"Checkout is unavailable","unknown":"We are checking checkout"}}
```

Each message is plain text of at most 240 characters. Empty or omitted state
messages use the existing public wording. Publication freezes these messages;
private draft edits do not change what visitors see. A message does not change
the measured state, and assertion errors are never copied into public text.

```bash
echopoint status-page validate -f page.json
echopoint --org "$ORG_ID" status-page save -f page.json -o json > saved-page.json
echopoint --org "$ORG_ID" status-page publish \
  --expected-draft-version "$(jq -r .draft_version saved-page.json)" \
  --expected-intent-version "$(jq -r .intent_version saved-page.json)" -o json
echopoint status-page public "$(jq -r .slug saved-page.json)" -o json
```

Verify saved mappings with `status-page view` and the anonymous projection with
`status-page public`. Public results contain curated health without probe/run
IDs, raw exchanges, assertion operands or credentials.

## MCP and release

The embedded contract exposes `list_probes`, `create_probe`, `get_probe`,
`update_probe`, `delete_probe`, `pause_probe`, `resume_probe`, `run_probe`,
`list_probe_runs`, `get_probe_run` and `estimate_probe`.
Arguments use API names (`probeId`, `runId`, `flow_ids`, `tags`, `tag_match_mode`,
`environment_key`, `expected_revision`); create/update retain nested `config`.
Existing `save_status_page` accepts direct `probe_bindings` and service `messages`; publication and withdrawal
retain their exclusion rules.

The CLI feature must merge and release alongside the API/UI milestone. Deployment
of the API alone does not add commands to existing CLI releases. A `feat:` commit
cuts a minor release on main; verify the released version and setup workflow
before calling rollout complete.

## Real API verification

Against an owned loopback API with a disposable organization without a status
page, supply `ECHOPOINT_PROBE_E2E_API_URL`, `ECHOPOINT_PROBE_E2E_API_KEY` and
`ECHOPOINT_PROBE_E2E_ORG_ID` securely, plus an explicit public HTTPS
`ECHOPOINT_PROBE_E2E_TARGET_URL` reachable by Cloud workers:

```bash
go test -tags integration ./internal/commands \
  -run '^TestProbeAPIConfigurationWorkflow$' -count=1 -v -timeout 6m
```

The built CLI creates two tagged flows and an environment, estimates an explicit
set and tag query, creates a paused probe without publishing/version inputs,
checks its shared monitor linkage/type, executes both flows in a private
diagnostic, updates to tags, resumes scheduling, publishes a status-page mapping
and waits for two distinct scheduled measurements and Operational public health.
It verifies direct bindings, frozen custom service messages, default wording
when messages are omitted, draft isolation and Unknown when the probe is paused.
Cleanup withdraws publication and removes the probe, flows and environment through
supported commands. A private draft remains because there is no page-delete API.
Cloud targets cannot use loopback/private networks. The integration build tag
prevents accidental live mutation during ordinary unit/race tests.
