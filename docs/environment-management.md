# CLI Variable Management

`org env` and `flows env` manage the variables a flow execution reads.

## Layers

A **variable set** is what one owner has: its base variables plus its named
environments. An **environment** is a named overlay such as `dev` or `prd`, and
nothing else — the container is the variable set.

| Layer | Resource | Applied when |
| --- | --- | --- |
| **Org base** | `/organization/variables` | every execution |
| **Named environment** | `/organization/environments/{name}/variables/{key}` | flow launch via `environment_key` |
| **Flow** | `/flows/{id}/variables` | every execution of that flow |

Resolution precedence at launch, highest first: **flow > org environment > org base**.

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
echopoint org env get [-e <name>] [--show-values]
echopoint org env set --var K=V [-e <name>] [--secret]
echopoint org env unset KEY [KEY...] [-e <name>]
echopoint org env import --file <env.json|.env> [-e <name>] [--secret]
echopoint org env delete                        # delete the whole variable set

echopoint org env environments list
echopoint org env environments create <name>
echopoint org env environments delete <name>    # drops the overlay and its variables

echopoint flows env get|set|unset|delete <flow-id>
```

`-o json|yaml` works on every read.

## One write per variable

Every write addresses one variable: `PUT /organization/variables/{key}`, or the
environment-scoped form. There is no whole-object upsert, so `set` and `unset`
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
