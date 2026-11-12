# Flow Management

The Echopoint CLI provides comprehensive commands for managing flows, including granular control over nodes, outputs, assertions, and edges.

`echopoint flow` is the command; `echopoint flows` is a permanent alias, so both spellings work everywhere below.

## Overview

Flows are the core of Echopoint - they define automated sequences of API requests with data extraction and validation. The CLI supports both bulk operations (create/update from JSON) and granular incremental modifications.

Every command that takes a flow takes it as `<flow-id>`: the flow's id, which `flow list` shows in its ID column and Tab completes. Anything that is not an id fails before any request with `"x" is not a flow id; list the flows with: echopoint flow list`. Flow ids, folders, executions, collections, environments and profiles complete with Tab (see [Completion](../README.md#completion)).

## Basic Commands

### List Flows
The table has NAME, ID, ROLE and UPDATED columns.
```bash
echopoint flow list
echopoint flow list -o json
echopoint flow list --limit 50

# page by cursor: -o json carries next_cursor, the table hints it on stderr
echopoint flow list --limit 50 -o json
echopoint flow list --limit 50 --cursor <next_cursor> -o json

# every flow, following next_cursor to the end
echopoint flow list --all -o json

# scope the listing to one branch of the folder tree
echopoint flow list --folder "Anchor/Identity"
echopoint flow list --uncategorized
```

Listings in the CLI page by cursor (`--limit`, `--cursor`, `--all`; `--offset` still skips by count but cannot be combined with `--cursor`). `flow list -o json` prints `{count, items, next_cursor, total}` with the search's slim rows, which carry no `flow_definition`; see [cursor-paginated listings](technical/pagination.md).

### View a Flow
```bash
# a summary: name, id, role, version, dates, node and edge counts
echopoint flow view <flow-id>

# the whole flow, definition included
echopoint flow view <flow-id> -o json
echopoint flow view <flow-id> -o yaml
```
`get` and `show` are aliases of `view`.

### Publish and inspect immutable versions

```bash
echopoint --org <organization-id> flow publish <flow-id> -o json
echopoint --org <organization-id> flow version list <flow-id> --limit 20 -o json
echopoint --org <organization-id> flow version list <flow-id> --cursor <next_cursor>
echopoint --org <organization-id> flow version view <flow-id> <version-id> -o json
```

Publishing snapshots the saved definition. Version reads include
the definition and assertions, and default to JSON so nested fields are preserved.
`flow versions` is an alias of `flow version`; `get` and `show` alias `view`.

### Create a Flow
Pass exactly one of `--name` and `-f`:
```bash
# an empty flow, to build with `flow node add`
echopoint flow create --name "Checkout smoke"

# a flow file. The CLI compiles it and sends the JSON the API stores.
echopoint flow create -f flows/anchor/login.flow.yaml --root flows

# a CreateFlowRequest JSON file, sent unchanged
echopoint flow create -f flow-definition.json --json
```

### Compile a flow file

`flow create -f` and `flow update -f` compile a flow file and send the JSON. `flow compile` does the same compile and prints the JSON. It does not call the API. The API stores the JSON. Pass `--json` to create or update only when the file is already that JSON.

```bash
echopoint flow compile -f flows
echopoint flow compile -f flows/anchor/login.flow.yaml --root flows
echopoint flow compile -f flows --check
echopoint flow create -f flows/anchor/login.flow.yaml --root flows
echopoint flow update <flow-id> -f flows/anchor/login.flow.yaml --root flows
```

`--check` prints problems and no JSON. A warning still compiles. For a flow that uses an integration, `--check` also reads the workspace catalog to check the operation outputs other nodes read. `-o yaml` prints the same document with the JSON key names.

A file is named `*.flow.yaml`. One file is one flow. Nodes are a mapping keyed by the node id, and `next` / `on_failure` name the nodes they run, so the file has no edges array. `steps` is the same graph written as a list: each step runs after the one before it, unless the step sets `next` or is a branch.

```yaml
name: "echopoint: organization member role"
role: standalone
tags: [echopoint]
nodes:
  login:
    module: anchor/login
    inputs:
      email: "{{email}}"
      password: "{{password}}"
    next: member
  member:
    request:
      method: GET
      url: "{{echopointUrl}}/organizations/{{echopointOrgId}}/members"
      headers:
        Authorization: "Bearer {{login.accessToken}}"
    expect: "status: 200"
```

`module: anchor/login` is the file `anchor/login.flow.yaml` under `--root`. Compile that module, create it, and set its `id` to the flow id the API returned. The caller then compiles to a module node whose `flow_id` is that id. Outputs are inferred from the outputs the module declares. A module addressed by a flow id instead of a file has no file to read, so its outputs are written out: `accessToken: request.accessToken`.

```yaml
name: "anchor: login (reusable)"
role: module
id: eac266b3-2f3f-41b4-b061-f63b92b7b80e
inputs: [email, password]
nodes:
  request:
    request:
      method: POST
      url: "{{nanostackBaseUrl}}/v1/auth/login"
      body:
        email: "{{email}}"
        password: "{{password}}"
    outputs:
      accessToken:
        path: $.accessToken
        secret: true
      refreshToken: $.refreshToken
    expect:
      status: 200
```

`expect: "status: 200"` and a `status` mapping are the same check. A list holds several checks: `["status: 200", "$.id not_empty"]`. The other checks are `$.path <operator> [value]`, `header:<name>`, `query:<name>`, `html:<selector>[@attribute]`, `regex:<pattern>`, `body`, and `status <operator> [value]`. `between` takes two values, `$.n between 1 5` or `status between 200 299`, and compiles to `operator_data.value` as a `[low, high]` pair. That is the pair the runner reads.

An output is `$.path`, `header:<name>`, `html:<selector>[@attribute]`, `regex:<pattern>`, `body`, or `status`. `status` captures the HTTP status code. `html` reads an HTML body (or an email's HTML) with a CSS selector, the element text or its attribute; the mapping form `{html: a, attribute: href, all: true}` returns every match. `regex` returns the first match in the body (or an email's text), group 1 when the pattern has a group; the mapping form takes `group`. Add `secret: true` beside `path`, `header`, `html`, `regex` or `status: true` when the value must be masked.

A node has exactly one kind: `request`, `module`, `integration`, `delay`, `set`, `assert`, `branch`, `poll`, `loop`, `webhook_wait`, `email_wait`, or `sse`. `delay: 1000` waits that many milliseconds. `set` writes variables, and those names are its outputs. A branch names a `target` node per case and a `default`; compile adds the success edges. `folder` is a folder id. A folder path is a warning and is left unset, because the API wants the id.

An `integration` node uses an operation or an event of a connected integration (see `echopoint integration items <id>`). It sets `id` and exactly one of `operation` and `event`. An operation node calls the operation with its `inputs`; it compiles to a `request` node, and launch fills the method, URL, headers and body from the integration, so the node sets none of them. An event node waits for the integration's webhook; it compiles to a `webhook_wait` node, takes no inputs and no status check, and checks the event payload with `expect`. Both take `timeout_ms`; an event node also takes `settle_ms`. `flow create -f` and `flow update -f` check each integration node against the workspace catalog: the integration and item must exist, the item must not be refused, and the operation must take every input. `flow compile` works offline, so it prints the operation node with an empty method and URL.

```yaml
name: "stripe: customer created"
nodes:
  customer:
    integration:
      id: stripe
      operation: customer.create
      inputs:
        email: garbage+stripe@nanostack.dev
    expect: "status: 200"
    next: created
  created:
    integration:
      id: stripe
      event: customer.created
      timeout_ms: 60000
    expect: "$.data.object.id equals {{customer.id}}"
```

An operation node provides the outputs its catalog operation lists: `echopoint integration items stripe --search customer.create -o json` lists each output's name and path. Declare `outputs` only to read a value the catalog does not list, as on a `request` node. A node whose `expect` accepts no 2xx status, such as `status: 402` for a declined card, gets no catalog outputs, because the error body does not have them. `flow create` and `flow compile --check` both read the workspace catalog and reject a reference to an output that neither the catalog nor the file declares, so `--check` needs you signed in for a flow that uses an integration. An event node provides the same outputs as a `webhook_wait` node.

All runs of a connection share its capture webhook, for every integration, so a concurrent run's event of the same type can arrive first. An event node's checks select the delivery: launch adds them to every `expect` group, and a delivery that fails them is skipped while the wait keeps listening. Give every event node a check on a value this run created, such as `$.data.object.id equals {{customer.id}}` above. A check on a literal or an environment value also matches another run's event, so `flow compile` and `flow create -f` print a warning for an event node that has no check on another node's output, a loop variable or a flow input.

`{{node.output}}` must name a node in the same graph and an output that node declares. A longer path walks into that value, the same way the runner does: `{{list.items.0.id}}` is valid when `list` declares `items`. `{{var}}` with no dot is an initial input. `{{webhook.url}}` and `{{webhook.requests_url}}` are provided when a flow waits for a webhook and no node is named `webhook`. `{{inbox.address}}`, `{{inbox.local}}`, `{{inbox.domain}}` and `{{inbox.messages_url}}` are provided when a flow waits for an email and no node is named `inbox`. An `sse` node always provides `events`, `count`, and `last`. A `webhook_wait` node always provides `id`, `method`, `headers`, `query`, `body`, and `received_at`. An `email_wait` node always provides `id`, `message_id`, `from`, `to`, `envelope_from`, `envelope_to`, `subject`, `text`, `html`, `links`, `first_link`, `code`, and `received_at`, or only `received` when it has `expect` groups.

An `email_wait` node waits for an email to the run inbox. Its `expect` checks pick the message; without a check, the first message matches. It takes `timeout_ms` (default 60000, at most 7200000, the address lifetime), `link_pattern` (a regex that keeps the links it matches), `code_pattern` (default `\b\d{6}\b`; group 1 when it has a group), and `settle_ms` and `expect` groups as a `webhook_wait` does. A quoted check target is a Go string, so a regex with a space doubles its backslashes: `'"regex:code is \\d+" not_empty'`.

```yaml
name: "acme: signup is verified by email"
nodes:
  signup:
    request:
      method: post
      url: "{{apiUrl}}/signup"
      body:
        email: "{{inbox.local}}+alice@{{inbox.domain}}"
    expect: "status: 202"
    next: verify
  verify:
    email_wait:
      timeout_ms: 120000
      link_pattern: ^https://staging\.acme\.example/verify
    expect:
      - $.subject contains Verify
      - $.envelope_to contains +alice@
    outputs:
      code:
        path: $.code
        secret: true
      button: html:a.verify@href
    next: confirm
  confirm:
    request:
      method: get
      url: "{{verify.first_link}}"
    expect: "status: 302"
```

Echopoint saves and launches such a flow. The run gets an inbox, and `{{inbox.address}}` is its address. A runner that does not declare `email_wait` (a CLI or runner older than echopoint-runner v0.108.0) is refused the run with `RUNNER_UPGRADE_REQUIRED`.

The same output name on two nodes is normal: each one is `{{that-node.id}}`. A module infers one public output per name, so a repeated public name is a warning and that name is not inferred. List `outputs` on the file to choose which names a caller receives.

A JSONPath that contains a space is quoted, so a filter survives: `"$.items[?(@.name == 'a b')].id" equals 1`. A poll or loop body has `nodes` only. Compile rebuilds its edges. A name or version stored on that body is not part of the file.

A poll or loop body does not see the parent flow's node outputs: it sees its own nodes, the flow's initial inputs, and, for a loop, the current item and index (`item` and `index` unless `item_var` / `index_var` set others). The poll's own check reads the body outputs, for example `$["probe.state"] equals ready`. An `on_failure` target needs `run_when: always`, or it does not run after the failure; compile warns.

### Update a Flow
Pass a file, or flags; exactly one of the two.
```bash
echopoint flow update <flow-id> -f updated-flow.json
echopoint flow update <flow-id> --name "Checkout smoke test" --description "Runs after deploy"
```
Only the flags given change; the other fields stay.

### Flow Role
A flow is either **standalone** (the default: a test of its own) or a **module**: a reusable building block that other flows call through a module node. A Module flow can still be launched directly to test it, but a schedule skips it when its tags match the flow, unless the schedule sets `ignore_modules` to false (the API default is true). `flow run --tag` runs module flows too unless you pass `--ignore-modules`.
```bash
# show the role
echopoint flow role <flow-id>
echopoint flow role <flow-id> -o json

# change it; only the role is sent, every other field stays
echopoint flow role <flow-id> --set module
echopoint flow role <flow-id> --set standalone
```
`-o json` and `-o yaml` print `{"id": ..., "role": ...}` for both forms. An unknown role fails before any request. `flow view -o json` and `flow list -o json` carry `role`, and a `CreateFlowRequest` or `UpdateFlowRequest` file may set it.

### Run or Launch a Flow

A flow runs on **Cloud** (EchoPoint runs it), on a **Self-hosted** runner (a long-lived runner you operate), or on an **Ephemeral** runner (a short-lived runner the caller operates). Two commands start one, and they are different:

```bash
# this CLI is the Ephemeral runner: it waits, shows live progress, and exits with the result
echopoint flow run <flow-id>
echopoint flow run <flow-id> <flow-id> --parallel 2 -e dev
echopoint flow run --tag smoke -o json
echopoint flow run --tag smoke --ignore-modules

# EchoPoint runs it on Cloud (or a Self-hosted runner): it prints the execution id and returns
echopoint flow launch <flow-id>
echopoint flow launch <flow-id> --runner self_hosted -e prd
```

`flow run` exits 0 when every flow passed, 1 when a flow failed, 2 when cancelled, 3 on an API or runner error and 4 on timeout. `-o json` prints one JSON object on stdout (the GitHub Action reads it); `-o yaml` prints the same object as YAML; the default `table` prints a summary on stderr. See [Running flows in CI](github-action.md).

`--ignore-modules` (with `--tag` only; combining it with flow IDs is a usage error, exit 3) drops module flows from the tag matches before launch, as schedules do. The search endpoint does not filter by role, so the CLI pages through all matches and filters client-side. The 50-flow safety cap counts only the flows that will be launched; at most 1000 matches are scanned. A match made only of modules fails like "no flows matched" (exit 3), and the skipped count (`Skipped 2 module flow(s) matched by the tags.`) goes to stderr, never into the `-o json` result.

`-e/--environment` overlays a named organization environment (`dev`, `prd`) on the flow's variables, on both commands.

### Executions
```bash
echopoint flow execution list <flow-id>
echopoint flow execution list <flow-id> --limit 100 --cursor <next_cursor> -o json
echopoint flow execution view <flow-id> <execution-id>
echopoint flow execution view <flow-id> <execution-id> -o json
```
`execution list` is newest first and pages like `flow list`.
`get` is an alias of `view`.

### Tag Flows
Add or remove tags on flows. Select flows by ID, or by a search filter (the same
search that backs the list). A filter is required for search selection — tagging
every flow in the org is intentionally not supported.
```bash
# tag specific flows
echopoint flow tag <flow-id> <flow-id> --add anchor

# tag every flow matched by a search filter
echopoint flow tag --query anchor --add anchor
echopoint flow tag --match-tag staging --match-mode any --add anchor

# remove a tag
echopoint flow tag <flow-id> --remove deprecated
```
Tags are merged with each flow's existing tags; no other fields change. Tags are
lowercased and de-duplicated server-side.

### Delete Flow
```bash
echopoint flow delete <flow-id>
```

The rule: **deleting a whole resource asks; editing part of one does not.** `flow delete` asks, while removing one node, edge, assertion or output, or unsetting one variable, does not. On a terminal, `Delete flow <id>? [y/N]` is asked on stderr and anything but `y` or `yes` cancels with exit code 2. Without a terminal (a script, CI) they refuse unless `--yes` (`-y`) is given: `pass --yes to delete flow <id> without a prompt`.
```bash
echopoint flow delete <flow-id> --yes
```
The same holds for `flow env delete`, `collection delete`, `flow folder delete`, `org env delete`, `org env environments delete`, `profile delete`, `config reset` and `status-page unpublish`.

---

## Folders

Flows live in a folder tree — the same tree the Flow Library renders. Folders are
addressed either by id or by a `/`-separated path of folder names from the root,
matched case-insensitively:

```bash
# show the tree with a flow count per folder
echopoint flow folder list

# create a folder; every missing segment of the path is created, so re-running is safe
echopoint flow folder create "Anchor"
echopoint flow folder create "Anchor/Identity/Roles"
echopoint flow folder create "Identity" --parent Anchor

# rename, and reparent (--to root moves it back to the top level)
echopoint flow folder rename "Anchor/Identity" "Identity & Access"
echopoint flow folder move "Identity" --to "Anchor"
echopoint flow folder move "Anchor/Identity" --to root
```

### Move Flows Between Folders

Select flows by ID, or by a search filter — with a filter every matching flow
moves, not just the first page. As with tagging, a filter is required for search
selection; moving every flow in the org is intentionally not supported.

```bash
# move specific flows
echopoint flow move <flow-id> <flow-id> --to "Anchor/Identity"

# move every flow carrying a tag, creating the destination path if needed
echopoint flow move --match-tag anchor --to "Anchor" --create

# pull flows back out of every folder
echopoint flow move <flow-id> --to uncategorized
```

The move runs as one server-side transaction and serializes on the
per-organization folder-tree lock, so a concurrent tree change returns `409`.

### Delete a Folder

```bash
# the folder and its descendants go away; the flows inside become uncategorized
echopoint flow folder delete "Anchor/Identity"

# also delete every flow in the subtree, with its execution history (irreversible)
echopoint flow folder delete "Anchor/Identity" --delete-flows --yes
```

Both ask first, like every destructive command; the prompt of `--delete-flows` says
that the flows go too. Without a terminal, pass `--yes`.

---

## Granular Node Management

Build flows incrementally by adding, updating, and removing individual nodes.

### Add Node

**Request Node:**
```bash
echopoint flow node add <flow-id> \
  --type request \
  --name "API Call" \
  --method POST \
  --url "https://api.example.com/endpoint" \
  --headers '{"Content-Type": "application/json", "Authorization": "Bearer token"}' \
  --body '{"key": "value"}'
```

**Delay Node:**
```bash
echopoint flow node add <flow-id> \
  --type delay \
  --name "Wait 5 seconds" \
  --duration 5000
```

**Flags:**
- `--type` (required): Node type - `request` or `delay`
- `--name` (required): Display name for the node
- `--method`: HTTP method for request nodes (GET, POST, PUT, PATCH, DELETE)
- `--url`: Request URL for request nodes
- `--headers`: JSON object of HTTP headers
- `--body`: Request body string
- `--duration`: Delay duration in milliseconds for delay nodes

### Remove Node
```bash
echopoint flow node remove <flow-id> <node-id>
```
Removes a node and all connected edges automatically. Removal and edge endpoint
lookup recognize all schema node types, including branch, loop, poll, SSE and
webhook-wait nodes, and preserve the other nodes in a mixed flow.

### Update Node
```bash
echopoint flow node update <flow-id> <node-id> \
  --name "New Name" \
  --method PUT \
  --url "https://api.example.com/new-endpoint"
```

**Flags:**
- `--name`: New display name
- `--method`: New HTTP method (request nodes only)
- `--url`: New URL (request nodes only)

Incremental `node update` supports request, delay and module nodes. Other existing
types return an explicit unsupported-editing error; use a complete flow definition
with `flow update -f` to edit them. Graph edits and flow-variable requests use the
command's cancellation context, including the read and save stages of an edit.

---

## Output Management

Extract data from node responses for use in downstream nodes.

### Add Output

**JSONPath Extractor:**
```bash
echopoint flow node output add <flow-id> <node-id> \
  --name "token" \
  --extractor json_path \
  --path "$.accessToken"
```

**Status Code Extractor:**
```bash
echopoint flow node output add <flow-id> <node-id> \
  --name "status" \
  --extractor status_code
```

**Body Extractor:**
```bash
echopoint flow node output add <flow-id> <node-id> \
  --name "response" \
  --extractor body
```

**Header Extractor:**
```bash
echopoint flow node output add <flow-id> <node-id> \
  --name "contentType" \
  --extractor header \
  --header-name "Content-Type"
```

**Secret output** (a credential or token the flow obtains during the run):
```bash
echopoint flow node output add <flow-id> <node-id> \
  --name "jwt" \
  --extractor json_path \
  --path "$.jwt" \
  --secret
```
From the moment the node extracts it, the value is replaced by `***` in every execution result, progress event and error, including module outputs and the inputs of later nodes. Those nodes still send the real value. Matching is by text, so flag only credentials and tokens: a short or common value is replaced everywhere it appears. A runner older than v0.77.0 ignores the flag.

**Flags:**
- `--name` (required): Output name for referencing in other nodes
- `--extractor` (required): Type - `json_path`, `status_code`, `body`, or `header`
- `--path`: JSONPath expression (for json_path extractor)
- `--header-name`: Header name (for header extractor)
- `--secret`: Replace the extracted value with *** in results and events

### Remove Output
```bash
echopoint flow node output remove <flow-id> <node-id> <output-name>
```

### Using Outputs in Other Nodes

Reference outputs using the template syntax:
```json
{
  "Authorization": "Bearer {{<node-id>.outputs.<output-name>}}"
}
```

Example:
```bash
# Node 1 extracts token
echopoint flow node output add <flow-id> <node1-id> --name "token" --extractor json_path --path "$.token"

# Node 2 uses the token in headers
echopoint flow node add <flow-id> --type request --name "Authenticated Request" \
  --method GET \
  --url "https://api.example.com/protected" \
  --headers "{\"Authorization\": \"Bearer {{<node1-id>.outputs.token}}\"}"
```

---

## Assertion Management

Add validation assertions to ensure responses meet expectations.

### Add Assertion

**Status Code Assertion:**
```bash
echopoint flow node assertion add <flow-id> <node-id> \
  --extractor status_code \
  --operator equals \
  --value "200"
```

**JSONPath Assertion:**
```bash
echopoint flow node assertion add <flow-id> <node-id> \
  --extractor json_path \
  --path "$.status" \
  --operator equals \
  --value "success"
```

**Body Contains Assertion:**
```bash
echopoint flow node assertion add <flow-id> <node-id> \
  --extractor body \
  --operator contains \
  --value "expected text"
```

**Flags:**
- `--extractor` (required): Type - `status_code`, `json_path`, `body`, `header`, or `query_param` (webhook waits only)
- `--path`: Path for json_path extractor
- `--header-name`: Header name for the header extractor
- `--param-name`: Query param name for the query_param extractor
- `--operator` (required): Comparison operator
- `--value`: Expected value for comparison

**Available Operators:**
- `equals` - Exact match
- `not_equals` - Not equal
- `contains` - Contains substring
- `not_contains` - Does not contain
- `greater_than` - Numeric greater than
- `less_than` - Numeric less than
- `greater_than_or_equal` - Numeric >=
- `less_than_or_equal` - Numeric <=
- `empty` - Empty value
- `not_empty` - Non-empty value
- `starts_with` - Starts with prefix
- `ends_with` - Ends with suffix
- `regex` - Matches regex pattern

### Remove Assertion
```bash
echopoint flow node assertion remove <flow-id> <node-id> <index>
```

View assertions with `echopoint flow view <flow-id> -o json` to find the index.

---

## Webhook Waits and Expected Events

A webhook wait reads the requests a run's own webhook received. Launch injects its
URL as `{{webhook.url}}`: point the system under test at it (for example as a
product's event endpoint), then check what arrived.

Use one wait at the end of the flow as its final check. It expects a set of named
events and reports a verdict for each:

```bash
# The final check runs even when a trigger branch failed
echopoint flow node add <flow-id> --id events --type webhook_wait \
  --name "Every invitation event" --timeout 30000 --settle 3000 \
  --run-when always --after update-invite --after resend-invite

# One expected event per effect, tied to its resource with a template
echopoint flow node expect add <flow-id> events --name "Role changed" --once \
  --match '$.type equals organization.invitation.updated' \
  --match '$.data.invitation_id equals {{invite-member.id}}'

echopoint flow node expect add <flow-id> events --name "Resent with a new token" \
  --match '$.type equals organization.invitation.updated' \
  --match '$.data.invitation_id equals {{invite-resend.id}}'

# An event that must not happen
echopoint flow node expect add <flow-id> events --name "No accept after withdrawal" --never \
  --match '$.type equals organization.invitation.accepted' \
  --match '$.data.invitation_id equals {{invite-withdraw.id}}'

# Checks every event must pass
echopoint flow node assertion add <flow-id> events --extractor header \
  --header-name webhook-signature --operator starts_with --value "v1,"
```

How it judges:

- A `--match` is `<target> <operator> [value]`. The target is a JSONPath into the
  body (`$.type`), a header (`header:webhook-signature`), a query param (`query:q`), or the whole `body`.
  Values resolve `{{node.output}}` templates.
- An event satisfies an expected event when it passes all its checks. One event
  counts for one expected event only, so two expected events with the same checks
  need two events. Events are assigned oldest first, in the order the expected
  events were added: add the specific ones before the broad ones.
- `--once`, `--never`, `--min` and `--max` bound the count; the default is at least
  once. `--settle` keeps listening after every expected event has arrived, so a
  late extra still fails `--once` or `--never`.
- The node's own assertions run on every event an expected event claimed.
- A template with no value (its step failed) fails only its expected event, with
  `not evaluated`. The node fails with `WEBHOOK_WAIT_EXPECTATIONS_FAILED`, and for
  an expected event that found nothing, the result names the closest request and
  each check's expected and actual value.

Remove one with `echopoint flow node expect remove <flow-id> <node-id> "<name>"`.

A wait without expected events keeps its older behaviour: it waits for the first
request that passes its assertions.

The same commands manage an email wait (`--type email_wait`). Its checks read the
message: `$.subject`, `$.envelope_to`, `$.code` and the other outputs, `body` (the
text), `header:From`, `html:<selector>[@attribute]` and `regex:<pattern>`:

```bash
echopoint flow node add <flow-id> --id verify --type email_wait --name "Verification email" \
  --timeout 60000 --link-pattern '/verify' --after signup
echopoint flow node assertion add <flow-id> verify --extractor json_path --path '$.subject' \
  --operator contains --value Verify
echopoint flow node output add <flow-id> verify --name code --extractor json_path --path '$.code' --secret
```

## Edge Management

Connect nodes to define execution flow.

### Add Edge

**Success Edge:**
```bash
echopoint flow edge add <flow-id> \
  --from <source-node-id> \
  --to <target-node-id> \
  --type success
```

**Failure Edge:**
```bash
echopoint flow edge add <flow-id> \
  --from <source-node-id> \
  --to <error-handler-node-id> \
  --type failure
```

**Flags:**
- `--from` (required): Source node ID
- `--to` (required): Target node ID
- `--type`: Edge type - `success` (default) or `failure`

### Remove Edge
```bash
echopoint flow edge remove <flow-id> <edge-id>
```

View edge IDs with `echopoint flow view <flow-id> -o json`.

---

## Complete Example

Create a complete CRUD flow step by step:

```bash
#!/bin/bash
set -e

# Authenticate
echopoint auth login --local

# Create empty flow
FLOW_ID=$(echopoint flow create --name "Product API Test" | grep "ID:" | awk '{print $2}')

# Step 1: Login and extract token
LOGIN_NODE=$(echopoint flow node add "$FLOW_ID" \
  --type request \
  --name "Login" \
  --method POST \
  --url "https://api.example.com/auth/login" \
  --headers '{"Content-Type": "application/json"}' \
  --body '{"email": "{{input.email}}", "password": "{{input.password}}"}' \
  | grep -o 'Node added: [^[:space:]]*' | awk '{print $3}')

echopoint flow node output add "$FLOW_ID" "$LOGIN_NODE" \
  --name "token" \
  --extractor json_path \
  --path "$.accessToken"

# Step 2: Create resource
CREATE_NODE=$(echopoint flow node add "$FLOW_ID" \
  --type request \
  --name "Create Product" \
  --method POST \
  --url "https://api.example.com/products" \
  --headers "{\"Authorization\": \"Bearer {{$LOGIN_NODE.outputs.token}}\"}" \
  --body '{"name": "{{input.product_name}}"}' \
  | grep -o 'Node added: [^[:space:]]*' | awk '{print $3}')

echopoint flow node output add "$FLOW_ID" "$CREATE_NODE" \
  --name "product_id" \
  --extractor json_path \
  --path "$.id"

echopoint flow node assertion add "$FLOW_ID" "$CREATE_NODE" \
  --extractor status_code \
  --operator equals \
  --value "201"

# Step 3: Get resource
GET_NODE=$(echopoint flow node add "$FLOW_ID" \
  --type request \
  --name "Get Product" \
  --method GET \
  --url "https://api.example.com/products/{{$CREATE_NODE.outputs.product_id}}" \
  --headers "{\"Authorization\": \"Bearer {{$LOGIN_NODE.outputs.token}}\"}" \
  | grep -o 'Node added: [^[:space:]]*' | awk '{print $3}')

echopoint flow node assertion add "$FLOW_ID" "$GET_NODE" \
  --extractor status_code \
  --operator equals \
  --value "200"

# Connect nodes
echopoint flow edge add "$FLOW_ID" --from "$LOGIN_NODE" --to "$CREATE_NODE" --type success
echopoint flow edge add "$FLOW_ID" --from "$CREATE_NODE" --to "$GET_NODE" --type success

echo "Flow created: $FLOW_ID"
```

---

## Generated Values

A `{{$name}}` template is generated by the runner, fresh for each run: `{{$email}}`,
`{{$uuid}}`, `{{$runId}}` (the same everywhere in one run), `{{$int:1:100}}`. Use them for
anything a flow creates, so a second run does not collide with the first.

Time generators take an offset, a duration with a sign, applied to the start of the run:

```bash
# An invitation that expires 20 seconds into the run, then a delay past it
echopoint flow node add <flow-id> --id invite-x --type request --name "Invite X" --method POST \
  --url "{{apiUrl}}/invitations" \
  --body '{"email":"{{$email}}","expires_at":"{{$isoTimestamp:+20s}}"}'
echopoint flow node add <flow-id> --id wait-x --type delay --name "Wait For X To Expire" \
  --duration 21000 --after invite-x
```

`{{$isoTimestamp:+20s}}`, `{{$timestamp:-1h}}` and `{{$isoTimestamp:90m}}` all work. A malformed
offset is left in the request as written, so the call fails loudly instead of sending a wrong
time.

## Tips

1. **Node IDs**: Use `echopoint flow view <flow-id> -o json` to see all node IDs
2. **Overview**: Use `echopoint flow view <flow-id>` for a quick summary
3. **Variables**: Use `{{input.<name>}}` for flow inputs and `{{<node-id>.outputs.<name>}}` for node outputs
4. **Validation**: Add assertions to validate responses before proceeding to next nodes
5. **Ordering**: Nodes execute in the order defined by edges, not creation order
