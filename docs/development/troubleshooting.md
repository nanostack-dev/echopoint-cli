# Troubleshooting

| Symptom | Cause and repair | Verification |
| --- | --- | --- |
| Regenerated client imports private server types | The source schema was used without stripping backend Go annotations. Follow integration-contract refresh through `strip_gotype.py`. | Generated client builds and its tests pass. |
| Runner upgrade did not reach users | A `chore:`-only change does not trigger CLI publication. Follow the release workflow's `feat:`/`fix:` rule for shipped behavior. | A CLI release embeds the expected runner module version. |
| Action rejects a removed flag | Action code and downloaded CLI differ. Verify the Action ref, `cli-version`, and the `v1` release step before rerunning. | Action tests and a pinned sandbox invocation agree with the flow-run contract. |
| `flow list --limit` is rejected | The API caps this limit at 100 and the CLI does not clamp it. Supply a valid limit. | The API returns the expected page. |
| `--cursor cannot be combined with --offset`, or the API answers `INVALID_CURSOR` | A cursor continues after the last result of a page and cannot be offset; a cursor also belongs to the filters and sort it was issued for. Drop `--offset`, and pass the `next_cursor` of the same command and filters unchanged. | `-o json` of the continued call returns the next page, and `next_cursor` is absent on the last. |
| In GitHub Actions, a second single-flow `flows run` in one job answers `409 IDEMPOTENCY_KEY_CONFLICT` | Up to v4.1.0 the derived idempotency key was one per job attempt, and a single flow used it unchanged, so another flow launched with the same key ([infra run 43953894269](https://github.com/nanostack-dev/infra/actions/runs/43953894269): 3 of 4 transport reruns refused). The derived key now includes the step id (`GITHUB_ACTION`) and is always per flow ([idempotency](../github-action.md#idempotency-and-retries)). On an older CLI, pass `--idempotency-key` with a value unique to that launch. | `TestDerivedKeyDiffersPerFlowForSingleFlowRuns` and `TestDerivedKeyDiffersPerStep`; each rerun creates a new execution in `GET /flows/<id>/executions`. |
| A spec path is rejected as an argument | Specs use a stored slug; only file-reading commands accept a local document via `-f/--file`. | The relevant command example and [spec docs](../specs.md) match the invocation. |

Keep additional entries tied to reproducible symptoms, established causes and verification. Credentials remain outside documentation and logs.
