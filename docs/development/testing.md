# Testing

```sh
go test -race ./...
golangci-lint run
./action-tests/run_tests.sh
```

The Go workflow runs `dagger check` ([dagger.toml](../../dagger.toml)): lint, race-enabled tests (Dagger's official Go modules) and the cross-platform build (`cli/cross-build`). Run `dagger check` locally to run exactly that, after installing the pinned CLI with `./scripts/ci/install-dagger.sh ~/.local/bin`. `TestFindAssetMatchesPlatform` fails on Linux arm64 hosts: its fixture has no linux/arm64 asset. The workflow no longer uploads `coverage.out` or the PR binaries; release.yml still builds the shipped binaries. Action shell tests need Bats and are a separate local verification; the Go workflow does not run them. The [Action sandbox workflow](../../.github/workflows/action-sandbox.yml) is an explicit live Action exercise, not unit coverage.

New command leaves need examples, help-group registration and convention tests. Process tests use `runCLI` in [testmain_test.go](../../internal/commands/testmain_test.go), which builds the binary once. Flow-run and Action contract changes must pass [golden fixtures](../../internal/commands/flow_run_golden_test.go) and Action tests. API/MCP changes must validate exposure and an end-to-end supported CLI configuration flow against the matching API; report credentials/target limitations separately from local checks.

Product administration is browser-panel-only. For these routes, verify CLI/MCP
exclusion instead of adding a configuration workflow: the administration process
tests check unavailable commands and tools, zero outbound requests for rejected
calls, and a successful ordinary flow call. Catalogue tests also cover incorrect
safe annotations and the exact `/administrations` path boundary.

The [CI workflow](../../.github/workflows/go.yml) uses path filters, so documentation-only PRs may have no Go checks. Check local Markdown links, command provenance and `git diff --check` for documentation changes.

Pull-request checks run for every target branch, including intermediate `gh stack`
branches. This validates each stack entry before it is ready for review; publishing
and deployment remain restricted by their existing workflow conditions.
