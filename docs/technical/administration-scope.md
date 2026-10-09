# Product administration scope

Product administration belongs only to Echopoint's browser administration panel.
This includes viewing and changing Cloud fleet limits shared across organizations
in a deployment. The CLI provides ordinary product configuration and execution
workflows; it has no administration command tree or administrator login mode.

MCP always excludes the exact `/administrations` path and all paths starting with
`/administrations/`. This rule applies before tool annotations, including when a
route is accidentally marked `x-ai-tool: true` without `x-ai-danger`. Other path
segments such as `/administrations-report` are unaffected. Former administration
tool names return an unknown-tool error without sending an API request.

The complete owning API contract remains in `internal/api/openapi.yaml`, with its
generated client methods retained as an internal mirror. Mirroring a route does
not expose a CLI command or MCP tool. Keep generated files synchronized through
the [integration contract procedure](integration-contracts.md).

Regression tests exercise a permissively annotated administration contract and
the real CLI stdio MCP process. They verify administration exclusion, rejection
without outbound HTTP requests, and continued access to ordinary flow tools.
