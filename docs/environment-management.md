# CLI Environment Management

Bring environment-variable management (currently UI-only) into the CLI.

## Scopes

Backend already exposes all three. No backend change.

| Scope | Backend resource | Selected when |
| --- | --- | --- |
| **Org base** | `variables` on `/organization/environment` | every execution |
| **Named environment** (dev/stg/prd) | `environments` overlay map on `/organization/environment` | flow launch via `environment_key` |
| **Flow** | `variables` on `/flows/{id}/environments` | every execution of that flow |

Resolution precedence at launch (highest wins): **flow vars > org named overlay > org base**.

## Command surface

`org env` is the new group (org base + named overlays). `flows env` stays for per-flow and gains `unset` (import/export remain org-only for now).

```
echopoint org env get [-e <name>]              # base vars; -e shows a named overlay
echopoint org env set --var K=V [-e <name>]     # merge (read-modify-write); -e auto-creates overlay
echopoint org env unset KEY [KEY...] [-e <name>]
echopoint org env import --file <env.json|.env> [-e <name>]
echopoint org env delete [--yes]                # delete whole org environment
echopoint org env environments list             # list overlay names
echopoint org env environments delete <name>    # drop one overlay

echopoint flows env get|set|unset|delete <flow-id>   # existing + new unset
```

`-o json|yaml` works on every read, mirroring existing commands.

### Why read-modify-write

The API has no per-key endpoint — only whole-object POST (upsert) / PUT (replace) / DELETE.
`set`/`unset` therefore: GET current env → convert `map[string]EnvironmentVariable` to `map[string]string` (take `.Value`) → mutate → POST `createOrUpdate` with the merged object. `delete` (no key) calls DELETE.

## CLI changes (all in `echopoint-cli`)

1. **`internal/api/openapi.yaml`** is the **full prod contract**, sourced from `https://api.echopoint.dev/openapi.yaml` (replacing the old hand-curated 28-path subset). Authoritative source of truth for codegen. Refresh by re-fetching and re-running the stripper below.
   - **Strip codegen-breaking annotations**: remove every `x-go-type` and `x-go-type-import` — they map schemas to backend/framework Go packages (`echopoint/internal/...`, `nanostack-framework/pkg/...`, `echopoint-runner/pkg/...`) that the CLI's module cannot import. Stripping leaves plain enums/base types. Keep `x-go-type-skip-optional-pointer` and `x-enum-varnames` (no imports, improve generation).
   - The full spec includes all path parameters (org header, pagination, filters), so generated methods gain a `*XxxParams` arg. The client request editor already injects `X-Organization-Id`, so call sites pass `nil` for params.
2. **Regen client**: `go generate ./internal/api`.
3. **`internal/commands/org_env.go`** (new): `newOrgCmd` → `org env ...`, mirroring `flow_env.go` style. Wire `newOrgCmd(state)` into `root.go` `AddCommand`.
4. **`internal/commands/flow_env.go`**: add `unset` for parity.

### Refreshing the contract

```sh
curl -fsSL https://api.echopoint.dev/openapi.yaml -o /tmp/prod.yaml
python3 scripts/strip_gotype.py /tmp/prod.yaml internal/api/openapi.yaml   # strips x-go-type + x-go-type-import
go generate ./internal/api
go build ./...   # fix any new call-site params
```

## Edge cases

- **No org id**: org endpoints need org context. Error early with a clear message if neither flag, `ECHOPOINT_ORGANIZATION_ID`, nor stored creds provide one.
- **No secret masking**: values are plaintext `value` server-side — no `isSecret` flag exists. `get` prints values as-is (same as `flows env`). Note for future.
- **Empty env**: GET on a never-set org env returns 200 with empty maps; `set` still works (upsert).
- **`-e` auto-create**: `set -e newname` creates the overlay; no separate "create environment" step.
