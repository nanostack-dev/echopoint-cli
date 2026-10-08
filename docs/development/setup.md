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
