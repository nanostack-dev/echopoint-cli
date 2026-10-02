# Echopoint CLI Agent Guide

Go `echopoint` CLI (`cmd/echopoint`) for the Echopoint API, plus the composite GitHub Action `action.yml` at the repo root (`uses: nanostack-dev/echopoint-cli@v1`).

## Surfaces

- `echopoint mcp` is a stdio MCP server (`internal/commands/mcp.go`, `internal/mcp`); every operation with `x-ai-tool: true` in the spec becomes a tool, `x-ai-danger` excludes one. See `docs/mcp.md`.
- The Action downloads the CLI release (`cli-version` input, default `latest`) and runs flows through it.

## OpenAPI Sync

- `internal/api/openapi.yaml` is echopoint's contract with `x-go-type` and `x-go-type-import` stripped; it also feeds the MCP catalog via `internal/api/spec.go`.
- Refresh: `python3 scripts/strip_gotype.py ../echopoint/cmd/http/openapi.yaml internal/api/openapi.yaml && (cd internal/api && go generate ./...)`.
- Never hand-edit `internal/api/client.gen.go`.

## Runner

- The runner is compiled in as a library (imports `pkg/jobrunner` and `pkg/spi`); its version is the `echopoint-runner` line in `go.mod`.
- Bump: `go get github.com/nanostack-dev/echopoint-runner@vX.Y.0 && go mod tidy`, then commit as `feat:` or `fix:` so it ships.

## Releases

- `.github/workflows/release.yml` runs on every push to `main` and reads commits since the last `vX.Y.Z` tag: `type!:`/`BREAKING CHANGE` -> major, `feat` -> minor, `fix`/`perf` -> patch.
- `docs:`, `chore:`, `ci:`, `refactor:`, `test:` alone cut nothing; a runner bump committed as `chore:` ships nothing.
- The Action's `v1` tag is repointed at the released commit on every release; the `ACTION_MAJOR_TAG` env in `release.yml` changes by hand only for a breaking `action.yml` change.
- Consumers on `@v1` get `action.yml` changes at the next release; the CLI itself resolves to `latest`.

## Commands

- Test: `go test -race ./...`. Lint: `golangci-lint run`.
- Action tests: `./action-tests/run_tests.sh` (needs `bats`; not run in CI).

## Gotchas

- `flows list --limit` is not clamped; the API caps `limit` at 100 (`maximum: 100` in the spec).
- MCP tool arguments use the spec's parameter names (`delete_flow` takes `id`, not `flowId`); a missing path parameter returns an error naming it.
