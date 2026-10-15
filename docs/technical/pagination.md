# Cursor-paginated listings

Every collection Echopoint owns is a `POST .../search` operation that pages with an opaque keyset cursor: a request carries `pagination: {limit, offset, cursor}` and a response carries `items`, `total`, `count` and, unless it is the last page, `next_cursor`. The 17 `GET` lists they replace (`listFlows`, `listFlowFolders`, `listEnvironments`, `listCollections`, `listFlowVersions`, `listFlowExecutions`, `listFlowSchedules`, `listFlowScheduleRuns`, `listSpecs`, `listSpecVersions` and the rest) are deprecated and keep working. The CLI and the MCP server read the searches. Echopoint's own reference is `docs/technical/cursor-search.md` in the Echopoint repository, which lists each search's scope and default order.

## Listing commands

These commands page by cursor and share one set of flags:

| Command | Search | Default order |
| --- | --- | --- |
| `flow list` (also `--folder`, `--uncategorized`) | `searchFlows` | newest first |
| `flow execution list <flow-id>` | `searchExecutions` with `filter.flow_ids` | newest first |
| `flow version list <flow-id>` | `searchFlowVersions` | newest first |
| `collection list` | `searchCollections` | newest first |
| `monitor list` | `searchFlowSchedules` | newest first |
| `monitor runs <monitor-id>` | `searchFlowScheduleRuns` | newest first |
| `spec list` | `searchSpecs` | by slug |
| `spec versions <slug>` | `searchSpecVersions` | newest first |

| Flag | Meaning |
| --- | --- |
| `--limit` | Results per page, 1 to 100 (default 20; `spec list` 100). With `--all` and no `--limit`, pages of 100. |
| `--cursor <next_cursor>` | Continue after the last result of a previous page, with the same filters. |
| `--all` | Follow `next_cursor` to the end and print every result as one page. |
| `--offset` | Still accepted: skips that many results, page-number style. It cannot be combined with `--cursor` (the API answers `400 INVALID_CURSOR`, so the CLI refuses first). Prefer a cursor: it stays correct while rows are created, and rows that tie on the sort value across a page boundary are neither skipped nor repeated. |

`-o json` and `-o yaml` print the API's envelope, `{count, items, next_cursor, total}`. `next_cursor` is absent on the last page, as in the API; with `--all` it is always absent. The table prints `Total: N` on stdout, and while there is more it writes `More results: add --cursor <next_cursor> to continue, or --all to list every page.` to stderr, so a script reading the table is unaffected. `flow version list` has no table: it prints JSON (or YAML with `-o yaml`) and the same hint on stderr.

`flow list -o json` items are the search's slim rows (`FlowSearchItem`): they carry no `flow_definition`. Read a whole flow with `flow view <flow-id> -o json`. `flow execution list` rows are the slim activity rows of `searchExecutions` (`flow_name`, `runner_type`, `status`, `started_at`, ...); read an execution's snapshot with `flow execution view`.

## Code that needs every row

Internal reads never page by offset. `walkSearch`, `walkAll` and `walkSome` in `internal/commands/pagination.go` follow `next_cursor`, stop on an absent cursor (never on `count` or `total`) and fail rather than loop when the API returns a cursor twice:

- `flow folder list` and every folder lookup read all folders (the tree needs them); `flow folder list --counts` reads all flows.
- `flow tag` with `--query` or `--match-tag`, and `flow run --tag --ignore-modules`, walk every match. `flow run --tag` without `--ignore-modules` is one request: its safety cap of 50 flows is checked against `total`.
- `org env environments list` prints the sorted names of all environments.
- Tab completion reads at most 5 pages of 100 (`completionMaxPages`) within the 2 second timeout and answers with what it read; completion of folder paths needs the whole tree and answers nothing if it cannot read it. Recent-execution and recent-run completions read the first page only.

## MCP

The search tools are named after the operation (`search_flows`, `search_collections`, `search_flow_folders`, `search_executions`, `search_flow_schedules`, `search_spec_versions`, ...). Their arguments are the path parameters plus the request body fields, flattened; the paging controls are the `pagination` object, so an agent continues a listing by passing `{"pagination": {"limit": 20, "cursor": "<next_cursor>"}}`. The deprecated `list_*` tools remain.
