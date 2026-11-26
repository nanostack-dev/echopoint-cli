# CLI Variable Management

`org env`, `workspace env` and `flow env` manage the variables a flow execution reads.

## Layers

A **variable set** is what one owner has: its base variables plus its values in
named environments. An **environment** is a named overlay such as `dev` or `prd`:
one entry of the organization's catalog (Echopoint ADR-0043). A **shared**
environment (scope `organization`) is seen by every workspace. A **workspace**
environment is seen by its workspace only. A name is unique in the organization,
whatever its scope.

| Layer | Resource | Applied when |
| --- | --- | --- |
| **Org base** | `/organization/variables` | every execution |
| **Org environment** | `/organization/environments/{name}/variables/{key}` | flow launch via `environment_key` |
| **Workspace base** | `/workspace/variables` | every execution in the workspace |
| **Workspace environment** | `/workspace/environments/{name}/variables/{key}` | flow launch via `environment_key` |
| **Flow** | `/flows/{id}/variables` | every execution of that flow |

Resolution precedence at launch, highest first: **flow > workspace environment >
workspace base > org environment > org base**. `-e` on `flow run` and `flow launch`
names an environment the workspace sees, shared or its own.

`workspace env` acts in the workspace `--workspace` (or `ECHOPOINT_WORKSPACE_ID`)
selects. Without one, the API uses the organization's oldest workspace.

## Secrets

A variable can be stored as a secret. It is encrypted at rest, a read never
returns its value, and execution results, progress events and flow exports
replace it with `***`.

A plain variable can become a secret. The reverse is refused: delete it and set
it again. That asymmetry is deliberate — the one-way direction only ever adds
protection, and there is no way to reveal a value that has already been hidden.

`--show-values` prints `<secret>` for a secret, not an empty string, because the
value is withheld rather than unset.

## Command surface

```
echopoint org env view [-e <name>] [--show-values]
echopoint org env set --var K=V [-e <name>] [--secret]
echopoint org env unset KEY [KEY...] [-e <name>]
echopoint org env import -f <env.json|.env> [-e <name>] [--secret]
echopoint org env delete                        # delete every organization value; environments stay (asks first)

echopoint org env environments list             # shared environments
echopoint org env environments create <name>
echopoint org env environments delete <name>    # drops it and the organization's values (asks first)

echopoint workspace env view|set|unset|import   # same flags as org env, in the selected workspace
echopoint workspace env environments list       # shared and own, with their scope
echopoint workspace env environments create <name>
echopoint workspace env environments delete <name>  # the workspace's own only (asks first)

echopoint flow env view|set|unset|delete <flow-id>   # delete asks first
echopoint flow env set <flow-id> -f <env.json|.env> [--var K=V] [--secret]
```

`-o json|yaml` works on every read. `workspace env environments list -o json` gives
`[{"name":"dev","scope":"organization"}, ...]`; the table prints a shared
environment as `shared`. `org env environments list -o json` stays a list of names.

A file is always given with `-f/--file`; a file typed as an argument fails with
`pass the file with -f`.

Deleting a whole resource asks; editing part of one does not. `flow env delete`,
`org env delete`, `org env environments delete` and `workspace env environments
delete` delete a whole variable set or environment, so they ask; `unset` removes
single variables and does not. On a terminal they ask `Delete <thing>? [y/N]` on
stderr, and anything but `y` or `yes` cancels with exit code 2. Without a terminal
they refuse unless `--yes` (`-y`) is given:
`pass --yes to delete every organization variable without a prompt`. Environment
names complete with Tab for `-e` and for `environments delete`: the organization
commands complete its shared environments, and `flow run`, `flow launch` and the
`workspace env` commands the environments the workspace sees.

## Rules the API enforces

| Command | Refusal |
| --- | --- |
| `environments create` | 409 `ENVIRONMENT_ALREADY_EXISTS` when the name exists anywhere in the organization; the answer never names the workspace that holds it. `workspace env environments create` also refuses a 51st environment of the workspace (`TOO_MANY_ENVIRONMENTS`). |
| `org env environments delete` | 409 `ENVIRONMENT_IN_USE` while a workspace holds values for it. Those workspaces remove their values first (`workspace env unset -e <name> ...`). |
| `workspace env environments delete` | 404 for a shared environment: delete it with `org env`, or remove the workspace's values with `unset`. |
| `org env delete` | Deletes values only. The shared environments, and the workspaces' values in them, stay. |

Rights: creating a workspace environment needs `workspace.environments:create`,
writing a value `workspace.environments:update`, deleting an environment or value
`workspace.environments:delete`. Workspace admins hold all three, editors hold
`update`, viewers none. An API key uses `environment:create|update|delete`. Reads
need `workspace.flows:read` (`flows:read` for a key).

## One write per variable

Every write addresses one variable: `PUT /organization/variables/{key}`,
`PUT /workspace/variables/{key}`, or their environment-scoped forms. There is no whole-object upsert, so `set` and `unset`
touch only the keys they are given and cannot clobber a concurrent write to a
different key.

An environment has to exist before a variable can be written into it. A
misspelled `-e` name fails with a 404 instead of silently creating an overlay
nothing reads, which is why `environments create` exists.

## Refreshing the contract

`internal/api/client.gen.go` and the embedded `internal/api/openapi.yaml` are
generated. To refresh them:

```bash
python3 scripts/strip_gotype.py <path-to-echopoint>/cmd/http/openapi.yaml internal/api/openapi.yaml
cd internal/api && go generate ./...
```

The stripping step removes `x-go-type` and `x-go-type-import`, whose import
paths are not reachable from this module.
