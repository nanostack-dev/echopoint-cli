# Flow Management

The Echopoint CLI provides comprehensive commands for managing flows, including granular control over nodes, outputs, assertions, and edges.

## Overview

Flows are the core of Echopoint - they define automated sequences of API requests with data extraction and validation. The CLI supports both bulk operations (create/update from JSON) and granular incremental modifications.

## Basic Commands

### List Flows
```bash
echopoint flows list
echopoint flows list -o json
echopoint flows list --limit 50

# scope the listing to one branch of the folder tree
echopoint flows list --folder "Anchor/Identity"
echopoint flows list --uncategorized
```

### Get Flow Details
```bash
echopoint flows get <flow-id>
echopoint flows show <flow-id>
```

### Create Flow (JSON)
```bash
echopoint flows create --file flow-definition.json
```

### Create Flow (Interactive)
```bash
echopoint flows create-interactive --name "My Flow"
```

### Update Flow
```bash
echopoint flows update <flow-id> --file updated-flow.json
```

### Tag Flows
Add or remove tags on flows. Select flows by ID, or by a search filter (the same
search that backs the list). A filter is required for search selection — tagging
every flow in the org is intentionally not supported.
```bash
# tag specific flows
echopoint flows tag <flow-id> <flow-id> --add anchor

# tag every flow matched by a search filter
echopoint flows tag --query anchor --add anchor
echopoint flows tag --match-tag staging --match-mode any --add anchor

# remove a tag
echopoint flows tag <flow-id> --remove deprecated
```
Tags are merged with each flow's existing tags; no other fields change. Tags are
lowercased and de-duplicated server-side.

### Delete Flow
```bash
echopoint flows delete <flow-id>
```

---

## Folders

Flows live in a folder tree — the same tree the Flow Library renders. Folders are
addressed either by id or by a `/`-separated path of folder names from the root,
matched case-insensitively:

```bash
# show the tree with a flow count per folder
echopoint flows folder list

# create a folder; every missing segment of the path is created, so re-running is safe
echopoint flows folder create "Anchor"
echopoint flows folder create "Anchor/Identity/Roles"
echopoint flows folder create "Identity" --parent Anchor

# rename, and reparent (--to root moves it back to the top level)
echopoint flows folder rename "Anchor/Identity" "Identity & Access"
echopoint flows folder move "Identity" --to "Anchor"
echopoint flows folder move "Anchor/Identity" --to root
```

### Move Flows Between Folders

Select flows by ID, or by a search filter — with a filter every matching flow
moves, not just the first page. As with tagging, a filter is required for search
selection; moving every flow in the org is intentionally not supported.

```bash
# move specific flows
echopoint flows move <flow-id> <flow-id> --to "Anchor/Identity"

# move every flow carrying a tag, creating the destination path if needed
echopoint flows move --match-tag anchor --to "Anchor" --create

# pull flows back out of every folder
echopoint flows move <flow-id> --to uncategorized
```

The move runs as one server-side transaction and serializes on the
per-organization folder-tree lock, so a concurrent tree change returns `409`.

### Delete a Folder

```bash
# the folder and its descendants go away; the flows inside become uncategorized
echopoint flows folder delete "Anchor/Identity"

# also delete every flow in the subtree, with its execution history (irreversible)
echopoint flows folder delete "Anchor/Identity" --delete-flows --yes
```

`--delete-flows` is irreversible and therefore refuses to run without `--yes`.

---

## Granular Node Management

Build flows incrementally by adding, updating, and removing individual nodes.

### Add Node

**Request Node:**
```bash
echopoint flows node add <flow-id> \
  --type request \
  --name "API Call" \
  --method POST \
  --url "https://api.example.com/endpoint" \
  --headers '{"Content-Type": "application/json", "Authorization": "Bearer token"}' \
  --body '{"key": "value"}'
```

**Delay Node:**
```bash
echopoint flows node add <flow-id> \
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
echopoint flows node remove <flow-id> <node-id>
```
Removes a node and all connected edges automatically.

### Update Node
```bash
echopoint flows node update <flow-id> <node-id> \
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
echopoint flows node output add <flow-id> <node-id> \
  --name "token" \
  --extractor jsonPath \
  --path "$.accessToken"
```

**Status Code Extractor:**
```bash
echopoint flows node output add <flow-id> <node-id> \
  --name "status" \
  --extractor statusCode
```

**Body Extractor:**
```bash
echopoint flows node output add <flow-id> <node-id> \
  --name "response" \
  --extractor body
```

**Header Extractor:**
```bash
echopoint flows node output add <flow-id> <node-id> \
  --name "contentType" \
  --extractor header \
  --header-name "Content-Type"
```

**Flags:**
- `--name` (required): Output name for referencing in other nodes
- `--extractor` (required): Type - `jsonPath`, `statusCode`, `body`, or `header`
- `--path`: JSONPath expression (for jsonPath extractor)
- `--header-name`: Header name (for header extractor)

### Remove Output
```bash
echopoint flows node output remove <flow-id> <node-id> <output-name>
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
echopoint flows node output add <flow-id> <node1-id> --name "token" --extractor jsonPath --path "$.token"

# Node 2 uses the token in headers
echopoint flows node add <flow-id> --type request --name "Authenticated Request" \
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
echopoint flows node assertion add <flow-id> <node-id> \
  --extractor statusCode \
  --operator equals \
  --value "200"
```

**JSONPath Assertion:**
```bash
echopoint flows node assertion add <flow-id> <node-id> \
  --extractor jsonPath \
  --path "$.status" \
  --operator equals \
  --value "success"
```

**Body Contains Assertion:**
```bash
echopoint flows node assertion add <flow-id> <node-id> \
  --extractor body \
  --operator contains \
  --value "expected text"
```

**Flags:**
- `--extractor` (required): Type - `statusCode`, `jsonPath`, `body`, or `header`
- `--path`: Path for jsonPath extractor
- `--operator` (required): Comparison operator
- `--value`: Expected value for comparison

**Available Operators:**
- `equals` - Exact match
- `notEquals` - Not equal
- `contains` - Contains substring
- `notContains` - Does not contain
- `greaterThan` - Numeric greater than
- `lessThan` - Numeric less than
- `greaterThanOrEqual` - Numeric >=
- `lessThanOrEqual` - Numeric <=
- `empty` - Empty value
- `notEmpty` - Non-empty value
- `startsWith` - Starts with prefix
- `endsWith` - Ends with suffix
- `regex` - Matches regex pattern

### Remove Assertion
```bash
echopoint flows node assertion remove <flow-id> <node-id> <index>
```

View assertions with `echopoint flows get <flow-id>` to find the index.

---

## Edge Management

Connect nodes to define execution flow.

### Add Edge

**Success Edge:**
```bash
echopoint flows edge add <flow-id> \
  --from <source-node-id> \
  --to <target-node-id> \
  --type success
```

**Failure Edge:**
```bash
echopoint flows edge add <flow-id> \
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
echopoint flows edge remove <flow-id> <edge-id>
```

View edge IDs with `echopoint flows get <flow-id> -o json`.

---

## Complete Example

Create a complete CRUD flow step by step:

```bash
#!/bin/bash
set -e

# Authenticate
echopoint auth login --local

# Create empty flow
FLOW_ID=$(echopoint flows create-interactive --name "Product API Test" | grep "ID:" | awk '{print $2}')

# Step 1: Login and extract token
LOGIN_NODE=$(echopoint flows node add "$FLOW_ID" \
  --type request \
  --name "Login" \
  --method POST \
  --url "https://api.example.com/auth/login" \
  --headers '{"Content-Type": "application/json"}' \
  --body '{"email": "{{input.email}}", "password": "{{input.password}}"}' \
  | grep -o 'Node added: [^[:space:]]*' | awk '{print $3}')

echopoint flows node output add "$FLOW_ID" "$LOGIN_NODE" \
  --name "token" \
  --extractor jsonPath \
  --path "$.accessToken"

# Step 2: Create resource
CREATE_NODE=$(echopoint flows node add "$FLOW_ID" \
  --type request \
  --name "Create Product" \
  --method POST \
  --url "https://api.example.com/products" \
  --headers "{\"Authorization\": \"Bearer {{$LOGIN_NODE.outputs.token}}\"}" \
  --body '{"name": "{{input.product_name}}"}' \
  | grep -o 'Node added: [^[:space:]]*' | awk '{print $3}')

echopoint flows node output add "$FLOW_ID" "$CREATE_NODE" \
  --name "product_id" \
  --extractor jsonPath \
  --path "$.id"

echopoint flows node assertion add "$FLOW_ID" "$CREATE_NODE" \
  --extractor statusCode \
  --operator equals \
  --value "201"

# Step 3: Get resource
GET_NODE=$(echopoint flows node add "$FLOW_ID" \
  --type request \
  --name "Get Product" \
  --method GET \
  --url "https://api.example.com/products/{{$CREATE_NODE.outputs.product_id}}" \
  --headers "{\"Authorization\": \"Bearer {{$LOGIN_NODE.outputs.token}}\"}" \
  | grep -o 'Node added: [^[:space:]]*' | awk '{print $3}')

echopoint flows node assertion add "$FLOW_ID" "$GET_NODE" \
  --extractor statusCode \
  --operator equals \
  --value "200"

# Connect nodes
echopoint flows edge add "$FLOW_ID" --from "$LOGIN_NODE" --to "$CREATE_NODE" --type success
echopoint flows edge add "$FLOW_ID" --from "$CREATE_NODE" --to "$GET_NODE" --type success

echo "Flow created: $FLOW_ID"
```

---

## Tips

1. **Node IDs**: Use `echopoint flows get <flow-id> -o json` to see all node IDs
2. **Testing**: Use `echopoint flows show <flow-id>` for a quick overview
3. **Variables**: Use `{{input.<name>}}` for flow inputs and `{{<node-id>.outputs.<name>}}` for node outputs
4. **Validation**: Add assertions to validate responses before proceeding to next nodes
5. **Ordering**: Nodes execute in the order defined by edges, not creation order
