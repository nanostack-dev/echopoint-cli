# Local setup

Use the Go version in [go.mod](../../go.mod), currently 1.26.5. Ordinary builds use pinned remote modules and the local embedded API contract; a sibling workspace checkout is optional. Python 3 is needed only for [contract refresh](../technical/integration-contracts.md), and Bats is needed for Action shell tests.

```sh
go mod download
go build -o /tmp/echopoint ./cmd/echopoint
/tmp/echopoint --help
/tmp/echopoint version
```

API-backed commands require a reachable target and credentials for that target. Use [authentication](../auth.md) and [environment management](../environment-management.md); check the selected profile and organization before a write. Unit tests use their own test fixtures and do not require live credentials.

For consumer installation, [README](../../README.md) documents platform installers, source builds and self-update. [Publication](../runbooks/deployment.md) distinguishes released binaries from the independent Action tag.

## Agent harness setup

Codex, OpenCode and Grok Build discover the local `AGENTS.md` natively. Claude Code uses the project-local [.claude/settings.json](../../.claude/settings.json) SessionStart hook to read that guide from the Git checkout root, including sessions started in a nested directory. Approve project trust on first use and reload the session after adding or updating hooks. Personal overrides stay in ignored `.claude/settings.local.json`; required project guidance does not depend on the shared workspace or globally installed skills.
