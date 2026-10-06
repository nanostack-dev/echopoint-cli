# OpenAPI specs

`echopoint spec` works with the OpenAPI 3.0 and 3.1 specs EchoPoint keeps (API › Specs). Every command works on a spec in EchoPoint, named by its unique slug: the first argument. A spec's title is free text. Only the commands that read or write a real file take `-f` (the same flag as `--file`): `create`, `push`, `pull`, `check`, `lint`, and `diff`. Every command needs an account: `echopoint auth login`, or `ECHOPOINT_API_KEY` and `ECHOPOINT_ORGANIZATION_ID`.

```bash
echopoint spec list
echopoint spec view <slug>
echopoint spec versions <slug>
echopoint spec create <slug> -f <file> [--bundle]
echopoint spec push <slug> -f <file> [--bundle]
echopoint spec pull <slug> [--version <v>] [-f <file>]
echopoint spec check <slug> -f <file>
echopoint spec lint <slug> [--version <v>] [--fail-on-findings]
echopoint spec lint <slug> -f <file> [--fail-on-findings]
echopoint spec diff <slug> [--from <v>] [--to <v>]
echopoint spec diff <slug> -f <file>
echopoint spec route|method|schema|property|param|response <verb> <slug> … --live
```

## Names

A spec's name is unique in the organization: lowercase letters, digits, and single hyphens (`pets-api`), at most 64 characters. `echopoint spec list` shows them. Creating a spec with a name that is taken is refused (`SPEC_SLUG_TAKEN`).

A name that looks like a path (it holds a `/`, ends in `.yaml`, `.yml`, or `.json`, or is not a valid name and exists as a file here) is refused before anything is sent:

```
"openapi.yaml" is not a spec slug. Spec slugs are listed by echopoint spec list; pass a file with -f.
```

A slug EchoPoint does not know prints `no spec with slug "<slug>"` and points at `echopoint spec list`.

## Creating, pushing, and pulling

EchoPoint is the source of truth for a spec: every push becomes a Live version with a computed version number and a history, and the repository holds a pulled copy.

```bash
echopoint spec create pets-api -f openapi.yaml         # the spec's first Live version
echopoint spec push pets-api -f openapi.yaml           # publish the next Live version
echopoint spec push pets-api -f openapi.yaml --bundle  # inline external $ref first
echopoint spec pull pets-api -f openapi.yaml           # write Live, byte for byte
echopoint spec pull pets-api --version 1.2.0 -f old.yaml
echopoint spec pull pets-api                           # print Live to stdout
echopoint spec pull pets-api -f -                      # the same
echopoint spec check pets-api -f openapi.yaml          # exit 1 when the file differs from Live
```

`create`, `push`, and the edit commands need `specs:write`; `list`, `view`, `versions`, `pull`, `check`, `lint`, and `diff` need `specs:read`, so a CI API key with `specs:read` can run `check`, `lint`, and `diff`. Pushing a document identical to Live, `info.version` aside, is refused: there is nothing to publish.

`push` prints the version it published, what changed (see Diff and version bump), and the convention findings the version introduced.

## Reading

```bash
echopoint spec list                  # slug, title, Live version, bump, when
echopoint spec view pets-api         # one spec in full
echopoint spec versions pets-api     # the history, newest first
echopoint spec versions pets-api --limit 100 --offset 100
```

`view` shows the title, the slug, the Live version and its bump, the OpenAPI version, how many convention findings Live has, what Live changed against the version before it (counts by severity), when the spec was created and last updated, who published Live, and the spec's page in EchoPoint (`frontend_url` of the profile, then `/api/specs/<slug>`).

`versions` shows each version with its bump, change counts, findings, who published it, and when. The first line is Live. It pages like the other list commands: `--limit` (default 20; the API allows at most 100) and `--offset`. `-o json` and `-o yaml` print what the API returned.

In CI, fail the build when the repository copy drifts from Live, or when a change adds convention findings:

```yaml
- run: echopoint spec check pets-api -f openapi.yaml
  env:
    ECHOPOINT_API_KEY: ${{ secrets.ECHOPOINT_API_KEY }}
    ECHOPOINT_ORGANIZATION_ID: ${{ secrets.ECHOPOINT_ORGANIZATION_ID }}
- run: echopoint spec lint pets-api -f openapi.yaml --fail-on-findings
  env:
    ECHOPOINT_API_KEY: ${{ secrets.ECHOPOINT_API_KEY }}
    ECHOPOINT_ORGANIZATION_ID: ${{ secrets.ECHOPOINT_ORGANIZATION_ID }}
```

## Editing

The edit commands change the Live version of a spec in EchoPoint one step at a time, so an agent or a script can build a spec without rewriting the whole document. Each command takes the spec's name, then the arguments that find the node it edits, and `--live`: EchoPoint applies the edit to the Live version and publishes the result as the next Live version.

```sh
echopoint spec route add pets-api POST /pets --live --status 201
echopoint spec param update pets-api GET /pets limit --in query --live --description "Page size"
# ✓ Published pets-api 1.5.0 (minor): Added POST /pets
```

- `--live` is required. Without it the command stops with `drafts are not available yet: pass --live to write the Live version`.
- All the commands of one invocation travel in a single request, and EchoPoint applies them all or none. A refusal prints `✗ <the API's message>` and exits 1.
- Findings the edit introduced are listed under `New findings`, with the rule, the pointer and the message. `echopoint spec push` lists them too.
- Parameter commands find the parameter in the Live version the CLI pulls first, so the position it targets is the one Live had at that moment.
- It needs the `specs:write` permission. Publishing with nothing changed is refused by EchoPoint (nothing to publish).
- An update with no flag to change anything is an error that names the flags it accepts.

Every edit command also takes:

| Flag | Does |
| --- | --- |
| `--live` | Edit the Live version and publish the next one. Required. |
| `--dry-run` | Pull Live, print the edited document to stdout, and send nothing. Not combined with `-o json` or `-o yaml`. |
| `--command-id <uuid>` | The UUID that makes a retry safe (see below). |
| `-o json`, `-o yaml` | Print the published version plus `replayed`. |

`-f` is not accepted: an edit does not read or write a file.

### Routes and methods

```bash
echopoint spec route add pets-api POST /pets --live --status 201 --tag pets --summary "Create a pet"
echopoint spec route update pets-api GET /pets --live --summary "List pets" --tag pets,store
echopoint spec route update pets-api GET /pets --live --clear-tags
echopoint spec route update pets-api GET /pets --live --path /animals   # renames the whole path item
echopoint spec route update pets-api GET /pets --live --method head
echopoint spec method update pets-api PUT /pets/{petId} --live --to PATCH
echopoint spec route remove pets-api DELETE /pets/{petId} --live
```

`route add` creates the path when it is new, gives the operation a response for `--status` (200 by default), and declares a `{param}` of the path that is not declared yet as a required string path parameter. `route update --path` renames the whole path item: every method on the path moves, not only the one named. An empty `--summary`, `--description`, or `--operation-id` removes the field. `method update --to` is the same as `route update --method`. METHOD is case-insensitive.

### Schemas and properties

```bash
echopoint spec schema add pets-api Pet --live --description "A pet in the store."
echopoint spec schema update pets-api Pet --live --description "A pet."
echopoint spec schema remove pets-api Pet --live       # refused while a $ref points at it

echopoint spec property add pets-api Pet name --live --required --description "Its name."
echopoint spec property add pets-api Pet status --live --enum available,sold
echopoint spec property add pets-api Pet owner --live --ref Owner
echopoint spec property add pets-api Pet address.city --live        # a nested property, with dots
echopoint spec property update pets-api Pet name --live --name title --required=false
echopoint spec property update pets-api Pet age --live --type integer --format int32 --nullable
echopoint spec property update pets-api Pet status --live --clear-enum
echopoint spec property remove pets-api Pet address.city --live
```

A schema is object-typed and a property is string-typed unless `--type` says otherwise. `--ref` points a property at a schema of `components/schemas` and cannot be combined with `--type`, `--format`, `--nullable`, or `--enum`. `--format` and `--nullable` are part of the type: on an update, pass `--type` with them, and `--type` resets the ones you leave out. A property name that holds a dot cannot be addressed this way.

### Parameters

```bash
echopoint spec param add pets-api GET /pets limit --live --in query --type integer --required
echopoint spec param update pets-api GET /pets limit --live --in query --description "Page size" --required=false
echopoint spec param update pets-api GET /pets limit --live --in query --name max --new-in header
echopoint spec param remove pets-api GET /pets limit --live --in query
```

A parameter is found by its name and where it is (`--in query|path|header|cookie`), never by its position, so `limit` in the query and `limit` in a header are two parameters. A parameter that the path item declares for every method is found too. `--in` names where the parameter is now; `--new-in` moves it. A path parameter stays required.

### Responses

```bash
echopoint spec response add pets-api GET /pets/{petId} 404 --live --schema Error
echopoint spec response add pets-api GET /pets 200 --live --type string --description "A list."
echopoint spec response update pets-api GET /pets 200 --live --schema PetList
echopoint spec response remove pets-api GET /pets 404 --live
```

The status is a code (`404`), a range (`4XX`), or `default`. `--schema` gives the `application/json` body a `$ref` to a schema of `components/schemas`; `--type` gives it an inline type. Without `--description`, a new response gets the standard reason of its status.

### Retries and command IDs

Every request carries a `command_id` (a UUID v4 generated per run). After a network error the CLI retries once with the same ID, and EchoPoint applies a given ID at most once: a repeat answers `✓ Already applied: pets-api 1.5.0` and changes nothing. `-o json` and `-o yaml` show it as `replayed: true`.

The request also names the Live version the CLI pulled (`base_version`). When someone published another version in between, EchoPoint refuses with `SPEC_LIVE_MOVED` and applies nothing: run the command again, and it is written against the new Live version.

Pin the ID with `--command-id <uuid>` when a script reruns a step and must not apply it twice:

```sh
echopoint spec schema add pets-api Pet --live --command-id 5b2a1c0e-8f0d-4c1b-9d57-3a7f2e6c9b10
```

## Conventions

`lint` reports where a spec departs from the conventions the rest of it follows. EchoPoint stores the same findings with every Live version and shows them in the docs.

```bash
echopoint spec lint pets-api                          # the findings stored with Live
echopoint spec lint pets-api --version 1.3.0          # the findings stored with another version
echopoint spec lint pets-api -f openapi.yaml          # only what the file adds to Live
echopoint spec lint pets-api -f openapi.yaml --fail-on-findings   # exit 1 on a finding
echopoint spec lint pets-api -o json
```

Without `-f`, `lint` lists what EchoPoint stored for a version (Live unless `--version` says otherwise). The findings that version introduced, compared with the version before it, are marked `(new)`; `introduced` in `-o json`.

With `-f`, `lint` judges the file by Live's conventions and lists only the findings the file adds: the check to run before `push`. `--version` is not combined with `-f`.

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

Lint exits 0 even with findings: a convention is the spec's own habit, not a rule. Add `--fail-on-findings` to enforce it, usually with `-f` so only new departures fail the build.

## Diff and version bump

`diff` groups changes by what they mean for clients: **Breaks clients**, **Needs a look**, **Adds**, and **Edits**. The bump is the one EchoPoint applies to a Live version:

| Change | Bump |
| --- | --- |
| Breaks clients, or needs a look | major |
| A new operation, schema, optional field, or enum value; looser limits | minor |
| Descriptions, examples, `x-` extensions, other edits | patch |
| No change | none |

`info.version` is ignored: EchoPoint sets it.

```bash
echopoint spec diff pets-api                           # Live against the version before it
echopoint spec diff pets-api --to 1.3.0                # 1.3.0 against the version before it
echopoint spec diff pets-api --from 1.2.0              # 1.2.0 against Live
echopoint spec diff pets-api --from 1.2.0 --to 1.4.0
echopoint spec diff pets-api -f openapi.yaml           # what pushing the file would change in Live
echopoint spec diff pets-api -o json
```

Without `-f`, `diff` compares two versions of the spec. `--to` defaults to Live and `--from` to the version right before `--to`; the first version of a spec has nothing before it. With `-f`, it compares the file with Live and prints the bump a `push` of the file would make, so a change can be previewed before it is pushed. `--from` and `--to` are not combined with `-f`.

## What is refused

- Swagger 2.0 and OpenAPI versions other than 3.0.x and 3.1.x.
- External `$ref` (another file or a URL). `create` and `push` take `--bundle` to inline them; `lint -f` and `diff -f` need a self-contained file.

## Canonical layout

EchoPoint stores a spec in one layout: OpenAPI fields in a fixed order, extensions after them sorted by name, paths, schemas, and properties in your order, two-space indentation, no comments. `pull` writes it byte for byte, and `check` compares a file with it byte for byte, so a pulled copy passes `check` until Live moves. YAML comments are dropped, so keep notes in `description` fields.

## Shell completion

`echopoint completion <shell>` prints the completion script. The spec slug completes as the first argument of every spec command (`slug`, then `Live <version> · <title>`), and `--version`, `--from`, and `--to` complete from the versions of the spec already typed. `-f` completes `.yaml`, `.yml`, and `.json` files. Completion asks EchoPoint with your normal sign-in, gives up after two seconds, and completes nothing when it cannot ask.
