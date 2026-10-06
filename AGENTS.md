# Echopoint CLI Agent Guide

Go `echopoint` CLI (`cmd/echopoint`) for the Echopoint API, plus the composite GitHub Action `action.yml` at the repo root (`uses: nanostack-dev/echopoint-cli@v1`).

## Surfaces

- `echopoint mcp` is a stdio MCP server (`internal/commands/mcp.go`, `internal/mcp`); every operation with `x-ai-tool: true` in the spec becomes a tool, `x-ai-danger` excludes one. See `docs/mcp.md`.
- The Action downloads the CLI release (`cli-version` input, default `latest`) and runs `echopoint flows run <ids> -o json ...` through it. `flows` is a permanent alias of `flow`; that invocation, its flags, the JSON on stdout and the exit codes 0/1/2/3/4 must not change. `internal/commands/flow_run_golden_test.go` pins them against `testdata/flow_run`.

## Milestone Coverage

- Ship CLI capabilities in the same milestone as the corresponding Echopoint API/UI changes. Agents must be able to configure and manage the whole project end to end through supported commands, without UI-only steps or direct database edits. Missing CLI coverage blocks milestone completion.
- Cover discovery/read, create/update, validation, applicable lifecycle actions such as publish/unpublish/delete, and verification of the resulting state. Support noninteractive flags or input files, structured output, explicit organization/environment selection where applicable and useful errors; preserve API authentication, tenant scope and permissions.
- Refresh the embedded OpenAPI contract and generated client, implement the commands, and update MCP exposure/catalog, help/docs and tests together. Generated client methods alone are not user-facing CLI capability; MCP tools must retain the existing exposure rules.
- Validate a reproducible end-to-end configuration workflow through the CLI and test exposed MCP operations. Link the companion API/UI PRs and include the CLI release/version in milestone rollout evidence; use a release-triggering commit for behavior changes as described below. Report any coverage gap before declaring the milestone complete.

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

- `flow list --limit` is not clamped; the API caps `limit` at 100 (`maximum: 100` in the spec).
- MCP tool arguments use the spec's parameter names (`delete_flow` takes `id`, not `flowId`); a missing path parameter returns an error naming it.
- Every `echopoint spec` command works on a spec in EchoPoint, named by its unique slug as the first argument (`<slug>`); see `docs/specs.md`. Only `create`, `push`, `pull`, `check`, `lint`, and `diff` read or write a real file, always through `-f/--file`. Never add a positional file, a `--spec` flag, or `-f` to another spec command.
- A command group (subcommands, no Run) rejects an unknown subcommand: `rejectUnknownSubcommands` in `internal/commands/command_groups.go` gives it a Run when `NewRootCmd` builds the tree, so don't write a Run for a group.

## Command Conventions

Every command follows these rules; `internal/commands` tests enforce most of them.

- Nouns are singular with a permanent plural alias: `flow` (`flows`), `collection` (`collections`), `status-page` (`status-pages`). `spec`, `org`, `profile`, `config` and `auth` have none.
- One read verb: `view`, with `get` and `show` kept as aliases. `list` is for many.
- A file is always `-f/--file` (`addFileFlag` registers the extensions for completion), and the command's `Args` is `fileFlagArgs(n)`, so a file typed as an argument fails with `pass the file with -f`. Never add a positional file.
- A flow argument is `<flow-id>` and goes through `resolveFlowID` (`resolve_flow.go`), the one place that turns it into an id. It reads an id and makes no request; anything else fails with `"x" is not a flow id; list the flows with: echopoint flow list`. Do not call `uuid.Parse` on a flow argument. `flow run` keeps its own words for a bad id (`invalid flow id "x": ...`) and the argument as typed in its result, because the Action reads them. `--flow-id` on `node add` and `status-page binding-options` goes through the resolver too.
- Vocabulary: a spec has a unique slug and a free-text title; flows and collections have an id and a free-text name, and no slug. Say "slug" for specs only.
- YAML is printed only through `output.PrintYAML`, which uses the key names of the JSON form; never marshal API types with a YAML encoder. A flag beats an environment variable, which beats the config (`configure` in `root.go`).
- **Deleting a whole resource asks; editing part of one does not.** `flow delete`, `collection delete`, `flow folder delete`, `flow env delete`, `org env delete`, `org env environments delete`, `profile delete`, `config reset` and `status-page unpublish` confirm. `env unset`, removing a node, edge, assertion or output, and the `spec` edit `remove` commands do not: they edit part of a resource, and a mistake is one command to put back. Do not add confirmations to those, and do not skip one on a command that deletes a whole resource.
- A command that deletes a whole resource calls `confirmDestructive` before it acts and `quietOnError`: `-y/--yes` skips the question, a terminal (stdin and stdout) is asked `Delete flow <id>? [y/N]` on stderr and anything but `y`/`yes` exits 2, and without a terminal it refuses with `pass --yes to delete <thing> without a prompt`. `AppState.IsTerminal` makes the terminal check injectable.
- Every leaf command has an `Example:`; `TestEveryLeafCommandHasAnExample` fails otherwise.
- Root commands sit in the help groups of `rootCommandGroups` in `command_groups.go`; a new root command needs an entry.
- Completion goes through `completeFromAPI` in `complete.go` (2s timeout, silent on error, `name\tdescription`, no file completion). A command whose `Use` starts with `<flow-id>` gets flow completion from `completeFlowPlaceholders`.
- `--org` is the visible organization flag and `--organization-id` a hidden alias (`addOrganizationFlag`). `flow run` has no `-o` of its own: it reads the global one, and an `-o` typed on it wins over `ECHOPOINT_OUTPUT_FORMAT`.
- Vocabulary in help text: Cloud (EchoPoint runs the flow), Self-hosted (a long-lived runner the customer operates), Ephemeral (a short-lived runner the caller operates; `flow run` makes the CLI one), environment (a named overlay of variables on an organization), profile (the CLI's API target, never an environment). `flow run` and `flow launch` are different commands; do not merge them.
- Tests that run the CLI as a process use `runCLI` (`testmain_test.go`), which builds `cmd/echopoint` once.
