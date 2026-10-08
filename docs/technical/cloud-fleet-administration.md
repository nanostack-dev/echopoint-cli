# Cloud fleet administration

`echopoint admin cloud-fleet` reads and updates the Cloud execution capacity shared by **all organizations in one deployment**. Use an Echopoint product administrator session, the same authorization required by the administration UI. Organization administrators and organization API keys cannot perform these operations.

## Select the deployment and sign in

A CLI profile selects the API deployment. It is distinct from an organization's named variable environment. Create a dev profile if needed, then sign in as a product administrator:

```sh
echopoint profile add dev --api-url https://apidev.echopoint.dev --frontend-url https://dev.echopoint.dev
echopoint auth login --admin --profile dev
```

The implicit `default` profile points to production. Fleet updates require an explicit `--profile`, even when a profile is already selected in configuration or `ECHOPOINT_PROFILE`.

Fleet requests send the session's Bearer token and omit the organization header. `--org` does not narrow their scope. If `ECHOPOINT_API_KEY` is set, unset it for these commands; an explicitly selected API key takes precedence over a session and is rejected. A fresh session login makes the session preferred over a stored API key.

Ordinary CLI browser login uses the `cli` JWT template, which may omit the product administrator claim. `--admin` requests the existing default template through the browser exchange and retains the token's actual expiry. It does not grant an administrator role. Sign in again with `--admin` when that session expires. For noninteractive use, supply an existing privileged session using `ECHOPOINT_TOKEN` or `--token` on the fleet command; `auth login --token` does not save it.

## Read, update and verify

```sh
# Read the actual enforced limits and the current settings revision.
echopoint admin cloud-fleet view --profile dev -o json

# Use the revision from that response. Both limits must be supplied.
echopoint admin cloud-fleet update --profile dev \
  --expected-revision 0 --daily-launch-limit 200 --global-cap 10 -o json

# Verify the persisted settings and the incremented revision.
echopoint admin cloud-fleet view --profile dev -o json
```

`view` also accepts `get` and `show`. Both commands support JSON and YAML through `-o`; the default table includes the selected profile and API URL. API responses retain the contract's JSON field names in structured output.

- `daily_launch_limit` is the maximum accepted Cloud launches in the last 24 hours. Queued launches already consume a slot. Zero pauses new launches.
- `global_cap` limits claimed Cloud jobs across organizations and backend replicas. It must be at least one. Accepted jobs wait when there are no claim slots.
- `launches_last_24h`, `launches_remaining`, `claimed_jobs` and `queued_jobs` report the current fleet snapshot.
- `window_start` and `generated_at` identify the snapshot's rolling window and fetch time. `next_launch_available_at` is nullable; it reports when a launch slot becomes available when applicable.
- `revision`, `settings_source` and nullable `settings_updated_at` distinguish deployment defaults from a saved administrator override.

Revision zero means there is no saved override and deployment defaults apply. The first update creates revision one. Each successful update increments the revision. A stale `--expected-revision` returns HTTP 409 with `CLOUD_FLEET_SETTINGS_CONFLICT`; the CLI exits unsuccessfully and does not retry or overwrite a concurrent administrator's change. Read again before choosing a new update.

Updates apply to the shared fleet. They do not reset launch reservations, refund deleted executions, change an organization's licensed quota, or alter Self-hosted and Ephemeral execution limits. Lowering a launch limit below current usage blocks new launches until enough reservations expire. Existing queued and claimed work is retained.

## Pause and resume

```sh
echopoint admin cloud-fleet view --profile default -o json
echopoint admin cloud-fleet update --profile default \
  --expected-revision 4 --daily-launch-limit 0 --global-cap 10 -o json

# Resume using the revision returned by the pause operation.
echopoint admin cloud-fleet update --profile default \
  --expected-revision 5 --daily-launch-limit 1000 --global-cap 10 -o json
```

The revisions are examples; use the values returned by the selected deployment. These settings constrain work, not the account's dollar bill. Cloudflare billing alerts and controller/container safeguards remain separate controls.

## MCP exposure

The API contract deliberately excludes product administration routes from MCP with `x-ai-danger`. Synchronizing the schema adds generated client support without exposing `get_cloud_fleet` or `update_cloud_fleet` as agent tools. Use the explicit CLI commands for this operator workflow.

## Verification against a matching API

Build this checkout with `go build -o /tmp/echopoint-fleet-cli ./cmd/echopoint`. Point a disposable profile at an API built from the companion contract, sign in with an administrator session, and run the read/update/read workflow above. Verify that the revision and both limits persist, a stale revision fails, and a non-admin session receives a forbidden response. Use the API's local test deployment for updates; stub-server command tests verify request shape and error handling but do not establish database persistence or server authorization.
