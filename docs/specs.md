# OpenAPI specs

`echopoint spec` works with OpenAPI 3.0 and 3.1 documents. `validate`, `fmt`, and `diff` run on local files and need no account. `list`, `push`, `pull`, and `check` work with the specs EchoPoint keeps (API › Specs).

## Specs in EchoPoint

EchoPoint is the source of truth for a spec: every push becomes a Live version with a computed version number and a history, and the repository holds a pulled copy.

```bash
echopoint spec push --new --spec pets-api openapi.yaml   # create the spec; its first Live version
echopoint spec push --spec pets-api openapi.yaml         # publish the next Live version
echopoint spec push --bundle --spec pets-api openapi.yaml  # inline external $ref first
echopoint spec list
echopoint spec pull --spec pets-api openapi.yaml         # write Live, byte for byte
echopoint spec pull --spec pets-api --version 1.2.0 old.yaml
echopoint spec check --spec pets-api openapi.yaml        # exit 1 when the file differs from Live
```

`push` needs `specs:write`; `list`, `pull`, and `check` need `specs:read`, so a CI API key with `specs:read` can run `check`. Pushing a document identical to Live, `info.version` aside, is refused: there is nothing to publish.

In CI, fail the build when the repository copy drifts from Live:

```yaml
- run: echopoint spec check --spec pets-api openapi.yaml
  env:
    ECHOPOINT_API_KEY: ${{ secrets.ECHOPOINT_API_KEY }}
    ECHOPOINT_ORGANIZATION_ID: ${{ secrets.ECHOPOINT_ORGANIZATION_ID }}
```

## Local files

```bash
echopoint spec validate openapi.yaml          # exits 1 on any problem
echopoint spec fmt openapi.yaml               # print the canonical YAML layout
echopoint spec fmt --write openapi.yaml       # rewrite the file in place
echopoint spec fmt --check openapi.yaml       # exit 1 when not in the canonical layout
echopoint spec diff old.yaml new.yaml         # changes by consequence, and the version bump
echopoint spec diff old.yaml new.yaml -o json
```

## What is refused

- Swagger 2.0 and OpenAPI versions other than 3.0.x and 3.1.x.
- External `$ref` (another file or a URL). Bundle the document into one file first.

## Canonical layout

`fmt` writes the layout EchoPoint stores for a spec: OpenAPI fields in a fixed order, extensions after them sorted by name, paths, schemas, and properties in your order, two-space indentation, no comments. YAML comments are dropped, so keep notes in `description` fields.

## Diff and version bump

`diff` groups changes by what they mean for clients: **Breaks clients**, **Needs a look**, **Adds**, and **Edits**. The bump is the one EchoPoint applies to a Live version:

| Change | Bump |
| --- | --- |
| Breaks clients, or needs a look | major |
| A new operation, schema, optional field, or enum value; looser limits | minor |
| Descriptions, examples, `x-` extensions, other edits | patch |
| No change | none |

`info.version` is ignored: EchoPoint sets it.
