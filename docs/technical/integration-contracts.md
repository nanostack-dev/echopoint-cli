# Integration contracts

## API and MCP

Echopoint owns the API schema. This repository stores a stripped copy at `internal/api/openapi.yaml`; it contains the complete contract needed for ordinary standalone development. Update the owning Echopoint contract first for API changes. Obtain the approved source `cmd/http/openapi.yaml` from the companion checkout or an identified upstream commit, then run from this repository root:

```sh
python3 scripts/strip_gotype.py /path/to/approved/openapi.yaml internal/api/openapi.yaml
(cd internal/api && go generate ./...)
```

The script removes `x-go-type` and `x-go-type-import` references to server-private Go types. The generation directive and config live in [gen.go](../../internal/api/gen.go) and [oapi-codegen.yaml](../../internal/api/oapi-codegen.yaml); edit those owners and the schema, then regenerate `client.gen.go` rather than editing generated code.

MCP exposes `x-ai-tool: true` operations and excludes `x-ai-danger`. It also always excludes `/administrations` and every descendant path, regardless of annotations. Product administration, including Cloud fleet settings, belongs only to the browser administration panel: do not add CLI commands or administrator login modes for these routes. The generated client still mirrors the complete contract internally. Calls for supported tools preserve API authentication, tenant scope and permissions. Verify parameter names, exposure and a representative configuration workflow when syncing ordinary product capabilities; preserve explicit organization/environment selection where applicable.

## Embedded libraries and Action

Upgrade runner behavior with `go get github.com/nanostack-dev/echopoint-runner@vX.Y.0` and `go mod tidy`, then use `feat:` or `fix:` so the CLI release ships it. Kit updates similarly pin `github.com/nanostack-dev/echopoint-kit` and require spec regression verification.

The Action invokes `echopoint flows run <ids> -o json ...`. Its flags, stdout result JSON and exit codes `0/1/2/3/4` are compatibility contracts pinned by [flow-run goldens](../../internal/commands/flow_run_golden_test.go). `flows` remains a permanent alias of `flow`. Every CLI release repoints Action `v1` to the released commit; changing the Action's breaking input contract requires an intentional `ACTION_MAJOR_TAG` change in the release workflow.
