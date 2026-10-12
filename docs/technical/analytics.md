# Execution analytics

`echopoint analytics` reads the organization's finished runs: how often they ran and
failed, how long they took, and the HTTP calls they sent. It is read-only and needs
the `flows:read` permission. `--profile`, `--org`, session or API-key authentication
and `-o` work as on every other command.

| Command | API operation | MCP tool |
| --- | --- | --- |
| `analytics summary [--window]` | `GET /analytics/summary` | `get_execution_analytics_summary` |
| `analytics flows [--window]` | `GET /analytics/flows` | `list_flow_analytics` |
| `analytics endpoints [--window] [--flow-id]` | `GET /analytics/endpoints` | `list_endpoint_analytics` |

```bash
echopoint analytics summary                      # last 7 days
echopoint analytics summary --window 24h -o json
echopoint analytics flows --window 30d
echopoint analytics endpoints --flow-id <flow-id>
```

## Window

`--window` is `24h`, `7d` (default) or `30d`. Any other value fails before a request
with `invalid --window "<value>": use 24h, 7d or 30d`. `24h` covers the current hour
and the 23 before it in hourly buckets; `7d` and `30d` cover the current UTC day and
the 6 or 29 before it in daily buckets. A run counts in the window its completion
time falls in, and reaches analytics a few seconds after it finishes.

## Output

`-o json` and `-o yaml` print the API body as received, field names and order
included, so scripts also see fields this CLI does not know yet. The table output is
a summary for people:

- `summary`: runs, failed runs with the failure rate, cancelled runs, run time
  (total, average, p95), Cloud run time, measured runs, HTTP calls, failed calls with
  the classes that occurred (`4xx`, `5xx`, `dns`, `connect`, `tls`, `timeout`,
  `no response`), and the run error codes, most frequent first.
- `flows`: one row per flow that finished a run, most runs first: id, name
  (`(deleted)` for a deleted flow), runs, failed, average and p95 run time, HTTP calls,
  and the peak calls of one run (`-` when no run was measured).
- `endpoints`: method, host, path template, calls, failed calls, and average, maximum
  and p95 duration. The API gives the p95 as an upper bound from 100 ms to 10 s, shown
  as `<=250ms`, or `>10s` when more than 5% of the calls took longer. At most 100
  endpoints are returned; the table says so when more exist.

Durations read `850ms`, `2.1s` or `4m12s`; times are UTC.

## Measured runs and endpoints

A measured run is one whose runner recorded its HTTP call count. Runs on an older
runner count in `runs` but not in `measured_runs`, so call totals cover measured runs
only. An endpoint is the request's URL template before resolution: `/users/{{userId}}`
stays one endpoint whatever the user id, and no resolved URL, query string or body is
kept.

## Exposure

The three reads are `x-ai-tool` operations, so `echopoint mcp` serves them with the
same arguments (`window`, and `flow_id` on endpoints). The rebuild,
`POST /administrations/analytics/rebuild`, is an administration route: it has no
command and no MCP tool, following [administration scope](administration-scope.md). The
[catalog test](../../internal/mcp/catalog_test.go) and the
[command tests](../../internal/commands/analytics_test.go), which also drive the built
binary and its stdio MCP server, pin this.

An Echopoint API deployed before these operations does not serve them; the command
then reports the API's error.
