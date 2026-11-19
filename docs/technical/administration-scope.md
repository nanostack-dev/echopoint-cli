# Product administration scope

Product administration belongs only to Echopoint's browser administration panel.
This includes viewing and changing Cloud fleet limits shared across organizations
in a deployment. The CLI provides ordinary product configuration and execution
workflows; it has no administration command tree or administrator login mode.

The CLI has no command for the exact `/administrations` path or any path starting
with `/administrations/`. Other path segments such as `/administrations-report` are
unaffected.
`echopoint api` refuses those paths before it sends
anything ([raw API calls](../api.md)).

The complete owning API contract remains in `internal/api/openapi.yaml`, with its
generated client methods retained as an internal mirror. Mirroring a route does
not expose a CLI command. Keep generated files synchronized through
the [integration contract procedure](integration-contracts.md).

Regression tests in
[administration_exclusion_test.go](../../internal/commands/administration_exclusion_test.go)
verify that the commands are unavailable, that rejected calls send no HTTP request,
and that an ordinary flow listing still works. The remote MCP server, served by the
Echopoint API, owns its own administration exclusion ([MCP](../mcp.md)).
