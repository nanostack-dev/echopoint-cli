# OpenAPI specs

`echopoint spec` works with OpenAPI 3.0 and 3.1 documents. `validate`, `fmt`, `diff`, `lint`, and the edit commands (`route`, `method`, `schema`, `property`, `param`, `response`) run on local files and need no account. `list`, `push`, `pull`, and `check` work with the specs EchoPoint keeps (API › Specs).

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

## Editing

The edit commands change a local OpenAPI file one step at a time, so an agent or a script can build a spec without rewriting the whole document. Each command takes `--file` and edits that file in place.

An edit rewrites only the lines of the nodes it changes. Comments, key order, blank lines, and quoting everywhere else stay byte for byte as they were, in YAML. A JSON file is written again as JSON with the indentation it had. The file is replaced atomically and keeps its mode. A refused command (adding an operation that exists, removing a schema that is still referenced) prints `✗ <reason>`, exits 1, and leaves the file as it was.

```bash
echopoint spec schema add Pet --file openapi.yaml          # ✓ Added schema Pet to openapi.yaml
echopoint spec route add POST /pets --file openapi.yaml --status 201 --operation-id createPet
echopoint spec route add get /pets/{petId} --file openapi.yaml      # METHOD is case-insensitive
```

Every edit command also takes:

| Flag | Does |
| --- | --- |
| `--file <path>` | The document to edit. Required for now. |
| `--dry-run` | Print the edited document to stdout and write nothing. Not combined with `-o json` or `-o yaml`. |
| `-o json`, `-o yaml` | Print `{file, commands, changed}` instead of the summary line. `commands` are the edits applied, in the form EchoPoint's editor sends. |

An update with no flag to change anything is an error that names the flags it accepts.

### Routes and methods

```bash
echopoint spec route add POST /pets --file openapi.yaml --status 201 --tag pets --summary "Create a pet"
echopoint spec route update GET /pets --file openapi.yaml --summary "List pets" --tag pets,store
echopoint spec route update GET /pets --file openapi.yaml --clear-tags
echopoint spec route update GET /pets --file openapi.yaml --path /animals   # renames the whole path item
echopoint spec route update GET /pets --file openapi.yaml --method head
echopoint spec method update PUT /pets/{petId} --file openapi.yaml --to PATCH
echopoint spec route remove DELETE /pets/{petId} --file openapi.yaml
```

`route add` creates the path when it is new, gives the operation a response for `--status` (200 by default), and declares a `{param}` of the path that is not declared yet as a required string path parameter. `route update --path` renames the whole path item: every method on the path moves, not only the one named. An empty `--summary`, `--description`, or `--operation-id` removes the field. `method update --to` is the same as `route update --method`.

### Schemas and properties

```bash
echopoint spec schema add Pet --file openapi.yaml --description "A pet in the store."
echopoint spec schema update Pet --file openapi.yaml --description "A pet."
echopoint spec schema remove Pet --file openapi.yaml       # refused while a $ref points at it

echopoint spec property add Pet name --file openapi.yaml --required --description "Its name."
echopoint spec property add Pet status --file openapi.yaml --enum available,sold
echopoint spec property add Pet owner --file openapi.yaml --ref Owner
echopoint spec property add Pet address.city --file openapi.yaml        # a nested property, with dots
echopoint spec property update Pet name --file openapi.yaml --name title --required=false
echopoint spec property update Pet age --file openapi.yaml --type integer --format int32 --nullable
echopoint spec property update Pet status --file openapi.yaml --clear-enum
echopoint spec property remove Pet address.city --file openapi.yaml
```

A schema is object-typed and a property is string-typed unless `--type` says otherwise. `--ref` points a property at a schema of `components/schemas` and cannot be combined with `--type`, `--format`, `--nullable`, or `--enum`. `--format` and `--nullable` are part of the type: on an update, pass `--type` with them, and `--type` resets the ones you leave out. A property name that holds a dot cannot be addressed this way.

### Parameters

```bash
echopoint spec param add GET /pets limit --file openapi.yaml --in query --type integer --required
echopoint spec param update GET /pets limit --file openapi.yaml --in query --description "Page size" --required=false
echopoint spec param update GET /pets limit --file openapi.yaml --in query --name max --new-in header
echopoint spec param remove GET /pets limit --file openapi.yaml --in query
```

A parameter is found by its name and where it is (`--in query|path|header|cookie`), never by its position, so `limit` in the query and `limit` in a header are two parameters. A parameter that the path item declares for every method is found too. `--in` names where the parameter is now; `--new-in` moves it. A path parameter stays required.

### Responses

```bash
echopoint spec response add GET /pets/{petId} 404 --file openapi.yaml --schema Error
echopoint spec response add GET /pets 200 --file openapi.yaml --type string --description "A list."
echopoint spec response update GET /pets 200 --file openapi.yaml --schema PetList
echopoint spec response remove GET /pets 404 --file openapi.yaml
```

The status is a code (`404`), a range (`4XX`), or `default`. `--schema` gives the `application/json` body a `$ref` to a schema of `components/schemas`; `--type` gives it an inline type. Without `--description`, a new response gets the standard reason of its status.

### Editing a spec in EchoPoint

Editing the Live version of a spec in EchoPoint with `--spec <slug> --live` instead of `--file` is coming. It sends the same commands to EchoPoint instead of changing a file.

## Conventions

`lint` reports where a document departs from the conventions the rest of it follows. EchoPoint stores the same findings with every Live version and shows them in the docs.

```bash
echopoint spec lint openapi.yaml                       # every finding
echopoint spec lint --base main.yaml openapi.yaml      # only what openapi.yaml adds
echopoint spec lint --spec pets-api openapi.yaml       # only what it adds to Live (needs specs:read)
echopoint spec lint --fail-on-findings openapi.yaml    # exit 1 on a finding
echopoint spec lint openapi.yaml -o json
```

| Rule | Reports |
| --- | --- |
| `property-casing` | A property or query parameter name in another casing than 80% of the multi-word property names |
| `operation-id-casing` | An operation ID in another casing than 80% of them |
| `missing-standard-responses` | An operation without a response status most operations of its tag declare |
| `error-shape` | An error response whose schema differs from the one most error responses use |
| `missing-description` | An operation or property without a description where most have one |
| `missing-extension` | An operation without an `x-` extension most operations of its tag set |
| `missing-security` | An operation without `security` where most declare it |
| `path-parameters` | A `{param}` in a path that is not declared, or a declared path parameter missing from the path |

Lint exits 0 even with findings: a convention is the document's own habit, not a rule. Add `--fail-on-findings` to enforce it, usually with `--base` or `--spec` so only new departures fail the build.

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
