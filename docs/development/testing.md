# Testing

```sh
go test -race ./...
golangci-lint run
./action-tests/run_tests.sh
```

The Go workflow runs lint, race-enabled tests and cross-platform builds. Action shell tests need Bats and are a separate local verification; the Go workflow does not run them. The [Action sandbox workflow](../../.github/workflows/action-sandbox.yml) is an explicit live Action exercise, not unit coverage.

New command leaves need examples, help-group registration and convention tests. Process tests use `runCLI` in [testmain_test.go](../../internal/commands/testmain_test.go), which builds the binary once. Flow-run and Action contract changes must pass [golden fixtures](../../internal/commands/flow_run_golden_test.go) and Action tests. API/MCP changes must validate exposure and an end-to-end supported CLI configuration flow against the matching API; report credentials/target limitations separately from local checks.

Product administration is browser-panel-only. For these routes, verify CLI/MCP
exclusion instead of adding a configuration workflow: the administration process
tests check unavailable commands and tools, zero outbound requests for rejected
calls, and a successful ordinary flow call. Catalogue tests also cover incorrect
safe annotations and the exact `/administrations` path boundary.

The [CI workflow](../../.github/workflows/go.yml) uses path filters, so documentation-only PRs may have no Go checks. Check local Markdown links, command provenance and `git diff --check` for documentation changes.
