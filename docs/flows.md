# Flow Management

The Echopoint CLI provides comprehensive commands for managing flows, including granular control over nodes, outputs, assertions, and edges.

`echopoint flow` is the command; `echopoint flows` is a permanent alias, so both spellings work everywhere below.

## Overview

Flows are the core of Echopoint - they define automated sequences of API requests with data extraction and validation. The CLI supports both bulk operations (create/update from JSON) and granular incremental modifications.

Every command that takes a flow takes it as `<flow-id>`: the flow's id, which `flow list` shows in its ID column and Tab completes. Anything that is not an id fails before any request with `"x" is not a flow id; list the flows with: echopoint flow list`. Flow ids, folders, executions, collections, environments and profiles complete with Tab (see [Completion](../README.md#completion)).

## Basic Commands

### List Flows
The table has NAME, ID and UPDATED columns.
```bash
echopoint flow list
echopoint flow list -o json
echopoint flow list --limit 50

# scope the listing to one branch of the folder tree
echopoint flow list --folder "Anchor/Identity"
echopoint flow list --uncategorized
```

### View a Flow
```bash
# a summary: name, id, version, dates, node and edge counts
echopoint flow view <flow-id>

# the whole flow, definition included
echopoint flow view <flow-id> -o json
echopoint flow view <flow-id> -o yaml
```
`get` and `show` are aliases of `view`.

### Create a Flow
Pass exactly one of `--name` and `-f`:
```bash
# an empty flow, to build with `flow node add`
echopoint flow create --name "Checkout smoke"

# a flow from a CreateFlowRequest JSON file
echopoint flow create -f flow-definition.json
```

### Update a Flow
Pass a file, or flags; exactly one of the two.
```bash
echopoint flow update <flow-id> -f updated-flow.json
echopoint flow update <flow-id> --name "Checkout smoke test" --description "Runs after deploy"
```
Only the flags given change; the other fields stay.

### Run or Launch a Flow

A flow runs on **Cloud** (EchoPoint runs it), on a **Self-hosted** runner (a long-lived runner you operate), or on an **Ephemeral** runner (a short-lived runner the caller operates). Two commands start one, and they are different:

```bash
# this CLI is the Ephemeral runner: it waits, shows live progress, and exits with the result
echopoint flow run <flow-id>
echopoint flow run <flow-id> <flow-id> --parallel 2 -e dev
echopoint flow run --tag smoke -o json

# EchoPoint runs it on Cloud (or a Self-hosted runner): it prints the execution id and returns
echopoint flow launch <flow-id>
echopoint flow launch <flow-id> --runner self_hosted -e prd
```

`flow run` exits 0 when every flow passed, 1 when a flow failed, 2 when cancelled, 3 on an API or runner error and 4 on timeout. `-o json` prints one JSON object on stdout (the GitHub Action reads it); `-o yaml` prints the same object as YAML; the default `table` prints a summary on stderr. See [Running flows in CI](github-action.md).

`-e/--environment` overlays a named organization environment (`dev`, `prd`) on the flow's variables, on both commands.

### Executions
```bash
echopoint flow execution list <flow-id>
echopoint flow execution view <flow-id> <execution-id>
echopoint flow execution view <flow-id> <execution-id> -o json
```
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
Removes a node and all connected edges automatically.

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

**Flags:**
- `--name` (required): Output name for referencing in other nodes
- `--extractor` (required): Type - `json_path`, `status_code`, `body`, or `header`
- `--path`: JSONPath expression (for json_path extractor)
- `--header-name`: Header name (for header extractor)

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
