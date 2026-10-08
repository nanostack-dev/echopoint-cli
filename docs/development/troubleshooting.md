# Troubleshooting

| Symptom | Cause and repair | Verification |
| --- | --- | --- |
| Regenerated client imports private server types | The source schema was used without stripping backend Go annotations. Follow integration-contract refresh through `strip_gotype.py`. | Generated client builds and MCP contract tests pass. |
| Runner upgrade did not reach users | A `chore:`-only change does not trigger CLI publication. Follow the release workflow's `feat:`/`fix:` rule for shipped behavior. | A CLI release embeds the expected runner module version. |
| Action rejects a removed flag | Action code and downloaded CLI differ. Verify the Action ref, `cli-version`, and the `v1` release step before rerunning. | Action tests and a pinned sandbox invocation agree with the flow-run contract. |
| `flow list --limit` is rejected | The API caps this limit at 100 and the CLI does not clamp it. Supply a valid limit. | The API returns the expected page. |
| MCP reports missing path parameter | Tool arguments use OpenAPI names; for example `delete_flow` uses `id`. Consult the tool schema. | The call reaches the intended authenticated operation. |
| A spec path is rejected as an argument | Specs use a stored slug; only file-reading commands accept a local document via `-f/--file`. | The relevant command example and [spec docs](../specs.md) match the invocation. |

Keep additional entries tied to reproducible symptoms, established causes and verification. Credentials remain outside documentation and logs.
