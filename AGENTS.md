# Echopoint CLI

Go CLI, stdio MCP server and composite GitHub Action. A standalone clone includes its OpenAPI contract and pinned runner/kit modules.

- Before implementation or delivery, read [agent workflow](docs/development/agent-workflow.md) and [testing](docs/development/testing.md).
- Before changing commands, flags, output or confirmation behavior, read [command conventions](docs/development/command-conventions.md) and [domain context](CONTEXT.md).
- Before API synchronization, runner upgrades, MCP or Action changes, read [integration contracts](docs/technical/integration-contracts.md) and the relevant capability docs in [the index](docs/README.md). Preserve `flows run`, its flags, stdout JSON and exit codes pinned by golden tests.
- Ship supported CLI configuration/lifecycle commands, structured output and noninteractive inputs with every corresponding Echopoint milestone. Generated client methods alone do not satisfy CLI coverage; link companion API/UI changes and report gaps before declaring completion.
- Before setup or recurring failures, read [setup](docs/development/setup.md) or [troubleshooting](docs/development/troubleshooting.md). Before merging release behavior, read [publication](docs/runbooks/deployment.md); for recovery, read [rollback](docs/runbooks/rollback.md).
- Keep required procedures local and update owning docs in the same PR as changed behavior or verified lessons.
