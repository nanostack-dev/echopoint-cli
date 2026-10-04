# Public status pages

The `status-pages` command manages the organization's shared draft, monitored
service binding and publication. It supports `--profile`, `--organization-id`
and the normal session/API-key authentication. Nested results default to JSON;
`--output yaml` retains the same API field names.

## Create and publish

```bash
# Discover the shared draft. A 404 means the organization has no page yet.
echopoint --profile prod status-pages get

# Discover the exact published version, environment and assertion choices.
echopoint --profile prod status-pages binding-options \
  --schedule-id <monitor-uuid> --flow-id <flow-uuid>

# Validate a complete SaveStatusPageRequest without credentials or an API call.
echopoint status-pages validate page.json

# Save the private draft; '-' reads JSON from stdin.
echopoint --profile prod status-pages save page.json > saved-page.json

# Use the draft_version and intent_version returned by save/get.
echopoint --profile prod status-pages publish \
  --expected-draft-version 1 --expected-intent-version 0

# Verify exactly what an anonymous visitor can read.
echopoint --profile prod status-pages public "$(jq -r .slug saved-page.json)"
```

[Complete example input](../internal/commands/testdata/status-page.json) includes
all design fields for Orbit, Ledger and Signal. For a new page,
`expected_draft_version` is `0`. For edits, read the current draft and construct
`{expected_draft_version, slug, config, binding}` from its current versions and
configuration. Keep any existing binding unless intentionally removing it. The
server appends a random 12-character suffix on creation: an input such as `example` becomes `example-a1b2c3d4e5f6`. The prefix accepts 3–48 characters and may be shared by other organizations. The returned full slug (up to 61 characters) is immutable: use it for later saves and public reads; never reconstruct it from the company name. Saving does not change the published page.

For an existing page, a complete editable request can be constructed without dropping the assigned address:

```bash
echopoint --profile prod status-pages get | jq '{expected_draft_version: .draft_version, slug, config, binding} | with_entries(select(.value != null))' > page.json
```

Existing addresses remain unchanged. This change adds suffixes only when creating new pages; it never renames a saved page.

For measured status, add a `binding` using the discovered values:

```json
{
  "service_id": "api",
  "schedule_id": "00000000-0000-0000-0000-000000000001",
  "flow_id": "00000000-0000-0000-0000-000000000002",
  "version_id": "00000000-0000-0000-0000-000000000003",
  "environment_key": "production",
  "check": {"node_id": "health", "assertion_index": 0}
}
```

The monitor must be enabled and select the published flow. Use the environment
returned by `binding-options`, including an empty string if that is its value.
Milestone 1 supports one monitored service/assertion. Monitors and published flow
versions are discoverable through the CLI's MCP tools (`list_flow_schedules`,
`list_flows`, `publish_flow`, `create_flow_schedule`).

Offline validation checks the OpenAPI schema. The server additionally validates
service/region uniqueness, visible services, logo policy, address immutability, binding
ownership and current monitor selection. Version conflicts are returned without
retrying: read the draft again and reconcile changes before another write.

## Public reads and withdrawal

Public reads create an anonymous client and send no bearer token, API key or
organization header, even when credentials exist in the selected profile.
Production pages are served at `https://status.echopoint.dev/<slug>`; development
uses `https://statusdev.echopoint.dev/<slug>`. The equivalent application route is
`/status/<slug>`. Custom API hosts do not imply a particular frontend address.

```bash
echopoint --profile prod status-pages unpublish --expected-intent-version 1
```

Use the latest intent version from `get`. The public endpoint returns 404 after
withdrawal, while the private draft remains editable. No page deletion endpoint
exists in milestone 1.

Health needs two consecutive scheduled measurements. Manual launches do not
establish public health. The minimum monitor interval is 15 minutes. Missing,
stale or inconclusive evidence stays Unknown; publishing never fabricates uptime.

The public HTML and JSON responses request exclusion from search engines with `X-Robots-Tag: noindex, nofollow`. They remain anonymously accessible. Customer CNAME domains require verified hostname routing and TLS; this milestone does not provide custom-domain commands.

## MCP exposure

The refreshed contract exposes `get_status_page`, `save_status_page` and
`get_status_page_binding_options`. The existing contract excludes publication,
withdrawal and visitor reads from the MCP catalog. Use the explicit CLI commands
for these actions; this change does not widen the MCP exposure policy.

The command family accompanies Echopoint API PR #475 and frontend PR #476.
It first ships in the CLI release cut after this feature is merged; earlier
releases, including v1.11.1, do not contain it.

## Permanent organization address

`get`, `save`, `publish` and `unpublish` return `public_url`, the authoritative
link for the selected environment. Anchor organizations also return
`organization_key`, a fixed 40-character lowercase hex encoding of the
organization's KSUID bytes. Renaming or republishing never changes this key.

```bash
echopoint --profile prod --organization-id "$ORG_ID" status-pages get > saved-page.json
jq -r .public_url saved-page.json
echopoint --profile prod status-pages public \
  --organization-key "$(jq -r .organization_key saved-page.json)"
```

The public command sends no stored credentials or organization headers. The new
organization-key endpoint remains excluded from MCP, like the legacy public
read. Existing slug reads and update payloads are unchanged; keep the complete
returned `slug` when saving. No DNS or Wrangler change is required per customer.
