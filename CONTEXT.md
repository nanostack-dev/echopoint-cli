# Domain context

| Term | Meaning |
| --- | --- |
| Profile | Local API target and credentials selection; distinct from an environment. |
| Organization | Tenant scope selected for API operations; `--org` is the visible flag. |
| Environment | Named overlay of organization variables. |
| Flow | Graph identified by an ID and free-text name; it has no slug. |
| Monitor | Recurring execution configuration selecting flows by tags or explicit IDs, on a cron or interval cadence. It is the API's flow schedule: the `/monitors` paths keep the `FlowSchedule` operation names. |
| Health policy | The `health_policy` of a monitor (`confirmation_runs`, `recovery_runs`, `freshness_seconds`). A monitor with one runs on an interval of 60 to 86400 seconds, measures health from its scheduled runs and can be bound to a status page. Diagnostic runs never confirm health. A probe was this concept under another name and no longer exists. |
| Flow role | `standalone` (default) or `module`; set with `flow role <flow-id> --set`. |
| Module flow | Flow with role `module`: a reusable building block that other flows call through a module node. It can be launched directly to test it; monitors skip it when their tags match unless the monitor sets `ignore_modules` to false. `flow run --tag` runs it unless `--ignore-modules` is passed. |
| Collection | API request grouping identified by an ID and name. |
| Spec | OpenAPI document stored in Echopoint, addressed by a unique slug with a separate title. |
| Cloud | Echopoint operates the execution runner. |
| Self-hosted | Customer operates a long-lived runner. |
| Ephemeral | Caller operates a short-lived runner; `flow run` embeds this mode locally. |
| Launch | `flow launch` asks the API to execute; it is distinct from local `flow run`. |
| Analytics window | `24h`, `7d` or `30d` lookback of execution analytics; a run counts in the window its completion time falls in. |
| Measured run | Finished run whose runner recorded its HTTP call count; analytics call totals cover measured runs only. |
| Endpoint | Analytics grouping of HTTP calls by method, host and unresolved path template, such as `/users/{{userId}}`. |
| MCP tool | OpenAPI operation exposed through the stdio MCP server under the contract's exposure rules. |
| Action version | Floating Action contract tag such as `v1`; independent from the CLI's semantic release version. |

[Command conventions](docs/development/command-conventions.md) own help/argument vocabulary. [Integration contracts](docs/technical/integration-contracts.md) own cross-repository compatibility.
