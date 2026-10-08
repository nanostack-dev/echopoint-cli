# Capability probes

`echopoint probe` (alias `probes`) configures durable measurements of named
capabilities. A saved probe pins an immutable published flow version, its effective
environment, selected assertion indexes, runner and confirmation/recovery policy.
All commands support the normal `--profile`, `--org` and authentication inputs.
Nested results default to JSON; `-o yaml` uses the same field names.

## Configure a published source

```bash
echopoint --org "$ORG_ID" flow create -f flow.json -o json > flow-created.json
FLOW_ID=$(jq -r .id flow-created.json)
echopoint --org "$ORG_ID" flow publish "$FLOW_ID" -o json > published.json
VERSION_ID=$(jq -r .id published.json)

echopoint --org "$ORG_ID" flow version list "$FLOW_ID" -o json
echopoint --org "$ORG_ID" flow version view "$FLOW_ID" "$VERSION_ID" -o json
echopoint --org "$ORG_ID" org env environments list -o json
echopoint --org "$ORG_ID" probe source-options \
  --flow-id "$FLOW_ID" --version-id "$VERSION_ID" --environment production \
  -o json > source.json
```

Use an environment that exists in the organization. An empty environment selects
the published flow's default; `source-options` returns the effective environment
that will be saved. Its checks identify assertions by `node_id` and zero-based
`assertion_index`. Select checks deliberately; a later flow publication does not
move the probe's pin.

[Complete example input](../internal/commands/testdata/probe.json) is a
`CreateProbeRequest`. Replace its flow/version IDs, effective environment and
checks with the discovered source values before sending it:

```bash
echopoint probe validate -f probe.json
echopoint --org "$ORG_ID" probe create -f probe.json -o json > created.json
PROBE_ID=$(jq -r .id created.json)
echopoint --org "$ORG_ID" probe list --limit 20 --offset 0 -o json
echopoint --org "$ORG_ID" probe view "$PROBE_ID" -o json
```

`-f -` reads stdin. For create, update and validation, an explicit `--environment`
overrides `config.environment_key` from the file, including an explicit empty
value. Organization selection remains a global `--org` input, never a JSON field.
Probe and run IDs are server-assigned KSUID strings; flow/version IDs are UUIDs.

`config.capabilities` contains stable IDs, labels, enabled state, selected checks
and `depends_on` capability IDs. The minimum interval is 60 seconds. Timeout,
confirmation count, recovery count and freshness are explicit configuration;
freshness must cover the interval plus execution timeout. The server validates
source ownership, published versions, current assertions, environment, unique
IDs/checks and acyclic dependencies. Offline validation checks the schema only.

## Edit, pause, resume and delete

Every edit invalidates evidence from the previous revision. Read the current
revision and complete config before replacing it:

```bash
echopoint --org "$ORG_ID" probe view "$PROBE_ID" -o json \
  | jq '{expected_revision: .revision, config}' > update.json
# Edit update.json, keeping every required config field.
echopoint probe validate --update -f update.json
echopoint --org "$ORG_ID" probe update "$PROBE_ID" -f update.json -o json

echopoint --org "$ORG_ID" probe pause "$PROBE_ID" --expected-revision 2 -o json
echopoint --org "$ORG_ID" probe resume "$PROBE_ID" --expected-revision 3 -o json
```

Use each action's returned revision for the next action. A 409 conflict is not
retried automatically: reload and reconcile the saved state. Pause stops future
scheduled measurements; capability health reflects the server's measurement
policy. Resume starts a new revision and requires fresh scheduled evidence.

```bash
echopoint --org "$ORG_ID" probe delete "$PROBE_ID" --expected-revision 4 --yes -o json
```

Deleting a whole resource asks on a terminal. Noninteractive deletion requires
`--yes`; pause, resume and update edit the resource and do not ask.

## Diagnostic runs and durable history

```bash
echopoint --org "$ORG_ID" probe run "$PROBE_ID" --expected-revision 4 -o json > run.json
RUN_ID=$(jq -r .id run.json)
echopoint --org "$ORG_ID" probe execution view "$PROBE_ID" "$RUN_ID" -o json
echopoint --org "$ORG_ID" probe execution list "$PROBE_ID" --limit 20 -o json
echopoint --org "$ORG_ID" probe history "$PROBE_ID" --limit 20 -o json
```

`run` queues a private diagnostic and returns without waiting. It uses the saved
pin and refuses a stale revision. It cannot select a scheduled origin and never
confirms, clears or extends public health. History includes scheduled and
diagnostic origins, revision, terminal status, sanitized step/assertion evidence
and observations. Follow the accepted run ID until terminal; the API prevents
overlapping occurrences of one probe.

Scheduled runs execute through the existing Cloud or Self-hosted flow runners.
Health comes from matching terminal evidence at the saved revision. Missing,
blocked, skipped, unavailable, expired or incompatible evidence remains Unknown;
an unmet dependency cannot establish a downstream outage. Disabled capabilities
are Not monitored. Confirmed target failures require the configured consecutive
occurrences; recovery has its own configured count.

## Map capabilities to a status page

Read the shared draft and retain its immutable slug and complete configuration:

```bash
echopoint --org "$ORG_ID" status-page view -o json \
  | jq '{expected_draft_version: .draft_version, slug, config, binding, probe_bindings} \
        | with_entries(select(.value != null))' > page.json
```

Add `probe_bindings` to the `SaveStatusPageRequest`, using an enabled, visible
service ID and an existing capability of the selected probe:

```json
{
  "probe_bindings": [
    {"service_id": "api", "probe_id": "<probe-id>", "capability_id": "api"}
  ]
}
```

This fragment must be included in the complete page request. Up to 32 mappings
are supported. Preserve the legacy `binding` when retaining a monitor source;
the server validates incompatible and duplicate assignments. Multiple capability
sources for a service are evaluated conservatively. Publication pins source
revisions server-side; do not add a client-supplied revision to a mapping.

```bash
echopoint status-page validate -f page.json
echopoint --org "$ORG_ID" status-page save -f page.json -o json > saved-page.json
echopoint --org "$ORG_ID" status-page publish \
  --expected-draft-version "$(jq -r .draft_version saved-page.json)" \
  --expected-intent-version "$(jq -r .intent_version saved-page.json)" -o json
echopoint status-page public "$(jq -r .slug saved-page.json)" -o json
```

Verify the stored mappings with `status-page view` and the resulting anonymous
projection with `status-page public`. Public results contain curated service
health, not probe/run IDs, assertion operands, raw exchanges or credentials.

## MCP and rollout

The embedded contract exposes `list_probes`, `create_probe`, `get_probe`,
`update_probe`, `delete_probe`, `pause_probe`, `resume_probe`, `run_probe`,
`list_probe_runs`, `get_probe_run` and `get_probe_source_options`. Tool arguments
use the exact API names: `probeId`, `runId`, `environment_key` and
`expected_revision`. Create/update retain the nested `config` object. Existing
`save_status_page` accepts `probe_bindings`; publication, withdrawal and public
reads retain the existing exclusion rules.

The companion CLI feature must be merged and released alongside the API/UI
milestone. Existing published CLI releases do not acquire these commands merely
because the API was deployed. A `feat:` behavior commit cuts a minor release on
main; validate the released version and complete setup workflow before declaring
the rollout complete.

## Verify the complete API workflow locally

Against an owned loopback API, provide a disposable organization with no existing
status page and a scoped key through the environment (never print the key):

```bash
go test -tags integration ./internal/commands \
  -run '^TestProbeAPIConfigurationWorkflow$' -count=1 -v
```

The test requires `ECHOPOINT_PROBE_E2E_API_URL`, `ECHOPOINT_PROBE_E2E_API_KEY` and
`ECHOPOINT_PROBE_E2E_ORG_ID`, plus `ECHOPOINT_PROBE_E2E_TARGET_URL` for an explicit
public HTTPS target reachable by Cloud jobs (for example, `https://example.com`).
Cloud jobs refuse loopback and private address ranges. It runs the built CLI against real APIs, creates a
named environment and flow, publishes it, verifies durable probe/source history,
executes a private diagnostic, resumes real scheduling, publishes a capability
mapping and waits for two distinct scheduled measurements and anonymous
Operational health. It then pauses/removes the probe, flow and environment and
withdraws publication through supported CLI commands. A private draft remains in
the disposable organization because the API has no page-delete operation. The
integration build tag prevents accidental live API changes during the ordinary
unit/race suite.
