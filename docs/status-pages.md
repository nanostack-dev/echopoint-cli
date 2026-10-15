# Public status pages

The `status-page` command (alias `status-pages`) manages the organization's shared
draft, monitored service binding and publication. It supports `--profile`, `--org`
and the normal session/API-key authentication. Nested results default to JSON;
`--output yaml` retains the same API field names.

## Create and publish

```bash
# Discover the shared draft. A 404 means the organization has no page yet.
echopoint --profile prod status-page view

# Find the monitors a service can be bound to: enabled, with a health policy.
echopoint --profile prod monitor list --enabled --has-health-policy

# Validate a complete SaveStatusPageRequest without credentials or an API call.
echopoint status-page validate -f page.json

# Save the private draft; '-f -' reads JSON from stdin.
echopoint --profile prod status-page save -f page.json > saved-page.json

# Use the draft_version and intent_version returned by save/view.
echopoint --profile prod status-page publish \
  --expected-draft-version 1 --expected-intent-version 0

# Verify exactly what an anonymous visitor can read.
echopoint --profile prod status-page public "$(jq -r .slug saved-page.json)"
```

[Complete example input](../internal/commands/testdata/status-page.json) includes
all design fields for Orbit, Ledger and Signal. For a new page,
`expected_draft_version` is `0`. For edits, read the current draft and construct
`{expected_draft_version, slug, config, monitor_bindings}` from its current versions and
configuration. Keep any existing `monitor_bindings` unless intentionally removing them. The
server appends a random 12-character suffix on creation: an input such as `example` becomes `example-a1b2c3d4e5f6`. The prefix accepts 3–48 characters and may be shared by other organizations. The returned full slug (up to 61 characters) is immutable: use it for later saves and public reads; never reconstruct it from the company name. Saving does not change the published page.

For an existing page, a complete editable request can be constructed without dropping the assigned address:

```bash
echopoint --profile prod status-page view | jq '{expected_draft_version: .draft_version, slug, config, monitor_bindings} | with_entries(select(.value != null))' > page.json
```

Existing addresses remain unchanged. This change adds suffixes only when creating new pages; it never renames a saved page.

For measured status, bind a visible service to a monitor with a health policy:

```json
{"monitor_bindings":[{"service_id":"api","monitor_id":"00000000-0000-0000-0000-000000000001"}]}
```

Only an enabled monitor with a health policy can be bound. See
[monitors](monitors.md) to create one. Publication freezes the monitor's `revision`:
a later change to the monitor makes that service Unknown until the page is published
again. The probe bindings (`probe_bindings`), the single-assertion `binding` and
`status-page binding-options` were removed with the Probe API (API 11.0.0); a
monitor with a health policy replaces them. A page that still carries the old fields
fails offline validation, and the server shows its services as not monitored until they
are rebound.

Offline validation checks the OpenAPI schema. The server additionally validates
service/region uniqueness, visible services, logo policy, address immutability, monitor
bindings and current monitor selection. Version conflicts are returned without
retrying: read the draft again and reconcile changes before another write.

## Public reads and withdrawal

Public reads create an anonymous client and send no bearer token, API key or
organization header, even when credentials exist in the selected profile.
Production pages are served at `https://status.echopoint.dev/<slug>`; development
uses `https://statusdev.echopoint.dev/<slug>`. The equivalent application route is
`/status/<slug>`. Custom API hosts do not imply a particular frontend address.

```bash
echopoint --profile prod status-page unpublish --expected-intent-version 1
```

Unpublishing withdraws the whole page, so it asks first (deleting a whole resource asks; editing part of one does not): on a terminal `Unpublish the status page? [y/N]` (anything but
`y` cancels with exit code 2); without a terminal it refuses unless `--yes` is given.

Use the latest intent version from `view`. The public endpoint returns 404 after
withdrawal, while the private draft remains editable. No page deletion endpoint
exists in milestone 1.

A monitor's health follows its health policy: `confirmation_runs` and `recovery_runs`
consecutive scheduled runs, and `freshness_seconds` of validity. Diagnostic runs
(`monitor run`) and manual launches do not establish public health. A health monitor
runs every 60 to 86400 seconds. Optional `config.services[].messages` contains
`operational`, `outage` and `unknown` text, each at most 240 characters. Empty or absent
messages retain the normal public wording. Messages belong to the service and are frozen
with the published page; editing a draft does not change the public page. Custom text
does not alter measured health. Missing, stale or inconclusive evidence stays Unknown;
publishing never fabricates uptime.

The public HTML and JSON responses request exclusion from search engines with `X-Robots-Tag: noindex, nofollow`. They remain anonymously accessible. Customer CNAME domains require verified hostname routing and TLS; this milestone does not provide custom-domain commands.

## MCP exposure

The refreshed contract exposes `get_status_page` and `save_status_page`
(`get_status_page_binding_options` was removed). The existing contract excludes publication,
withdrawal and visitor reads from the MCP catalog. Use the explicit CLI commands
for these actions; this change does not widen the MCP exposure policy.

The command family accompanies Echopoint API PR #475 and frontend PR #476.
It first ships in the CLI release cut after this feature is merged; earlier
releases, including v1.11.1, do not contain it.

## Permanent organization address

`view`, `save`, `publish` and `unpublish` return `public_url`, the authoritative
link for the selected environment. Anchor organizations also return
`organization_key`, a fixed 40-character lowercase hex encoding of the
organization's KSUID bytes. Renaming or republishing never changes this key.

```bash
echopoint --profile prod --org "$ORG_ID" status-page view > saved-page.json
jq -r .public_url saved-page.json
echopoint --profile prod status-page public \
  --organization-key "$(jq -r .organization_key saved-page.json)"
```

The public command sends no stored credentials or organization headers. The new
organization-key endpoint remains excluded from MCP, like the legacy public
read. Existing slug reads and update payloads are unchanged; keep the complete
returned `slug` when saving. No DNS or Wrangler change is required per customer.
