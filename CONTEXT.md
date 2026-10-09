# Domain context

| Term | Meaning |
| --- | --- |
| Profile | Local API target and credentials selection; distinct from an environment. |
| Organization | Tenant scope selected for API operations; `--org` is the visible flag. |
| Environment | Named overlay of organization variables. |
| Flow | Graph identified by an ID and free-text name; it has no slug. |
| Monitor | Recurring execution configuration selecting flows by tags or explicit IDs. |
| Probe | A monitor of type `probe` producing one health result from all selected flows and their executed assertions, with safe status-page publication policy. |
| Collection | API request grouping identified by an ID and name. |
| Spec | OpenAPI document stored in Echopoint, addressed by a unique slug with a separate title. |
| Cloud | Echopoint operates the execution runner. |
| Self-hosted | Customer operates a long-lived runner. |
| Ephemeral | Caller operates a short-lived runner; `flow run` embeds this mode locally. |
| Launch | `flow launch` asks the API to execute; it is distinct from local `flow run`. |
| MCP tool | OpenAPI operation exposed through the stdio MCP server under the contract's exposure rules. |
| Action version | Floating Action contract tag such as `v1`; independent from the CLI's semantic release version. |

[Command conventions](docs/development/command-conventions.md) own help/argument vocabulary. [Integration contracts](docs/technical/integration-contracts.md) own cross-repository compatibility.
