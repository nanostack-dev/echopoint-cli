# Echopoint CLI

Go CLI, stdio MCP server and composite GitHub Action. A standalone clone includes its OpenAPI contract and pinned runner/kit modules.

- Before implementation or delivery, read [agent workflow](docs/development/agent-workflow.md) and [testing](docs/development/testing.md).
- Before changing commands, flags, output or confirmation behavior, read [command conventions](docs/development/command-conventions.md) and [domain context](CONTEXT.md).
- Before API synchronization, runner upgrades, MCP or Action changes, read [integration contracts](docs/technical/integration-contracts.md) and the relevant capability docs in [the index](docs/README.md). Preserve `flows run`, its flags, stdout JSON and exit codes pinned by golden tests.
- Prove every client change end to end in the default configuration too (no `--workspace` or other optional selection), with the client regenerated from echopoint main's contract ([postmortem](docs/postmortems/2026-10-22-blank-workspace-header.md)).
- Ship supported CLI configuration/lifecycle commands, structured output and noninteractive inputs with every corresponding Echopoint milestone. Generated client methods alone do not satisfy CLI coverage; link companion API/UI changes and report gaps before declaring completion.
- Product administration uses Echopoint's browser Admin Panel. Keep `/administrations` operations outside CLI commands and privileged CLI login; MCP excludes that path and its descendants regardless of tool annotations. The complete internal API mirror may retain them.
- Before setup or recurring failures, read [setup](docs/development/setup.md) or [troubleshooting](docs/development/troubleshooting.md). Before merging release behavior, read [publication](docs/runbooks/deployment.md); for recovery, read [rollback](docs/runbooks/rollback.md).
- Keep required procedures local and update owning docs in the same PR as changed behavior or verified lessons.

## Flow files

Write a flow as `*.flow.yaml`. Nodes are a mapping keyed by the node id. `next` and `on_failure` name the nodes they run, so the file has no edges array. `module: anchor/login` is another file under `--root`.

`echopoint flow create -f` and `echopoint flow update -f` compile that file, run the linter, and send the JSON the API stores. `echopoint flow compile -f <file> --check` lints; it reads the API only for a flow that uses an integration, to check the operation outputs other nodes read against the workspace catalog. Pass `--json` only when the file is already a CreateFlowRequest or an UpdateFlowRequest.

The grammar, checks, and module rules are in [docs/flows.md](docs/flows.md). Do not hand-write the JSON graph when a flow file can say it.
