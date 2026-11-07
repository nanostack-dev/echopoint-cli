package mcp

// buildFlowDescription is the tool's prompt. It leads with a complete worked
// example because a concrete sample cuts authoring mistakes more than schema
// prose: the model anchors on the shape it can see.
const buildFlowDescription = `Create (or replace by name) an echopoint flow from a FLAT node list.

Prefer this over create_flow for authoring flows: you supply plain nodes and
"source>target" edges; the server maps them to the full flow document (node data
wrapper, nested assertions, output extractors, edge handles, layout) and
validates every {{node.output}} reference and edge endpoint BEFORE saving. On a
problem it returns the list of issues to fix — nothing is persisted.

Refs: {{nodeId.output}} pulls an upstream node's declared output; a node's
output must be listed in its "outputs" to be referenceable. Bare {{name}} (no
dot) is an environment/initial input. A node named in an assert "value" or a
url/body/header is checked too.

Asserts: {"status":200} is shorthand for status_code==200. Otherwise use
{"path":"$.x","op":"equals","value":"y"} — op defaults to equals and supports
not_equals, contains, not_contains, empty, not_empty, greater_than, less_than,
starts_with, ends_with, regex (empty/not_empty need no value).

Nodes: kind "request" (default) needs method+url; kind "module" runs a sub-flow
and needs flow_id, with "outputs" mapping a local name to "childNodeId.childKey".
run_when defaults on_success; use "always" for cleanup nodes so they run even
after an upstream failure.

Integration nodes: a node with "integration" calls a vendor operation or waits
for a vendor event; launch fills the method, url, auth and body, so give no
method, url, headers or body. {"integration":{"id":"stripe","operation":
"customer.create","inputs":{"email":"..."}}} is a request node: its "inputs"
are the operation params, and the operation's outputs (see
search_integration_items) are referenceable as {{nodeId.output}} without
declaring them. {"integration":{"id":"stripe","event":"checkout.session.completed"}}
is a wait node (optional "timeout_ms"): it passes when that event reaches the
connection's capture webhook during the run; assert on payload paths, not status.
Every run of a connection shares its capture webhook, for every vendor, so a
concurrent run's event of the same type can arrive first. An event's asserts
select the delivery: one that fails them is skipped, and the wait keeps
listening. Give every event an assert on a value this run created, for example
{"path":"$.data.object.id","value":"{{customer.id}}"}; an assert on a literal
or an environment value also matches another run's event. build_flow warns
when an event has no such assert.
Set up an integration before using it:
  1. search_integrations — find the integration id;
  2. search_integration_connections, then connect_integration if not connected;
  3. search_integration_items — the operation/event keys, params and outputs;
     set_integration_choices to accept an item the workspace refused;
  4. build_flow with the integration nodes.

Secrets: list request outputs that hold a credential or token in
"secret_outputs" (e.g. "secret_outputs": ["jwt"]). Their values show as *** in
results; downstream nodes still receive the real value. A module re-exporting a
child's secret output is masked too, so only the producing node is flagged.

Worked example — create a flow that calls an API and asserts the result:
{
  "name": "demo: smoke",
  "tags": ["demo"],
  "nodes": [
    {"id": "create", "method": "POST", "url": "{{baseUrl}}/v1/widgets",
     "headers": {"Authorization": "Bearer {{token}}"},
     "body": "{\"name\":\"w1\"}",
     "outputs": {"widgetId": "$.id"},
     "asserts": [{"status": 201}, {"path": "$.name", "value": "w1"}]},
    {"id": "get", "method": "GET", "url": "{{baseUrl}}/v1/widgets/{{create.widgetId}}",
     "headers": {"Authorization": "Bearer {{token}}"},
     "asserts": [{"status": 200}, {"path": "$.id", "op": "not_empty"}]},
    {"id": "cleanup", "method": "DELETE", "url": "{{baseUrl}}/v1/widgets/{{create.widgetId}}",
     "headers": {"Authorization": "Bearer {{token}}"},
     "run_when": "always", "asserts": [{"status": 204}]}
  ],
  "edges": ["create>get", "get>cleanup"]
}

Integration example — create a Stripe customer, then wait for checkout:
{
  "name": "demo: stripe checkout",
  "nodes": [
    {"id": "customer", "integration": {"id": "stripe", "operation": "customer.create",
     "inputs": {"email": "{{email}}"}}, "asserts": [{"status": 200}]},
    {"id": "paid", "integration": {"id": "stripe", "event": "checkout.session.completed"},
     "timeout_ms": 300000, "asserts": [{"path": "$.data.object.customer", "value": "{{customer.id}}"}]}
  ],
  "edges": ["customer>paid"]
}`

// buildFlowInputSchema is the JSON Schema advertised to the MCP client.
var buildFlowInputSchema = []byte(`{
  "type": "object",
  "required": ["name", "nodes", "edges"],
  "properties": {
    "name": {
      "type": "string",
      "description": "Flow name. An existing flow with the same name is replaced."
    },
    "tags": {
      "type": "array",
      "items": {"type": "string"},
      "description": "Optional flat tags."
    },
    "nodes": {
      "type": "array",
      "description": "Flat list of request, module and integration nodes.",
      "items": {
        "type": "object",
        "required": ["id"],
        "properties": {
          "id": {"type": "string", "description": "Unique node id, referenced by edges and {{id.output}}."},
          "kind": {"type": "string", "enum": ["request", "module"], "description": "Defaults to request."},
          "run_when": {"type": "string", "enum": ["on_success", "always"], "description": "Defaults on_success; use always for cleanup."},
          "method": {"type": "string", "description": "request: HTTP method (GET, POST, ...)."},
          "url": {"type": "string", "description": "request: URL; may use {{env}} and {{nodeId.output}} refs."},
          "headers": {"type": "object", "additionalProperties": {"type": "string"}, "description": "request: header name -> value."},
          "body": {"type": "string", "description": "request: raw JSON string body."},
          "outputs": {
            "type": "object",
            "additionalProperties": {"type": "string"},
            "description": "request: outputName -> json_path (e.g. {\"id\":\"$.id\"}). module: localName -> childNodeId.childKey."
          },
          "secret_outputs": {
            "type": "array",
            "items": {"type": "string"},
            "description": "request: names from outputs whose values are credentials or tokens. The runner masks each as *** in every result, error and downstream input; the flow still uses the real value. Not for module nodes: flag the child node that extracts the value."
          },
          "asserts": {
            "type": "array",
            "description": "request: assertions on the response.",
            "items": {
              "type": "object",
              "properties": {
                "status": {"type": "integer", "description": "Shorthand: assert HTTP status equals this."},
                "path": {"type": "string", "description": "json_path to extract from the body."},
                "op": {"type": "string", "description": "Operator; defaults equals."},
                "value": {"type": "string", "description": "Expected value for operators that need one."}
              }
            }
          },
          "integration": {
            "type": "object",
            "required": ["id"],
            "description": "Integration node: set exactly one of operation and event. Leave out kind, method, url, headers and body.",
            "properties": {
              "id": {"type": "string", "description": "Integration id from search_integrations (e.g. stripe)."},
              "operation": {"type": "string", "description": "Operation key from search_integration_items; the node calls it."},
              "event": {"type": "string", "description": "Event key from search_integration_items; the node waits for it."},
              "inputs": {"type": "object", "description": "operation: param name -> value; values may use {{refs}}."}
            }
          },
          "timeout_ms": {"type": "integer", "description": "integration event: how long to wait, in milliseconds."},
          "flow_id": {"type": "string", "description": "module: id of the sub-flow to run."},
          "inputs": {"type": "object", "additionalProperties": {"type": "string"}, "description": "module: input binding name -> value."}
        }
      }
    },
    "edges": {
      "type": "array",
      "items": {"type": "string"},
      "description": "Success edges as \"sourceId>targetId\", e.g. \"create>get\"."
    }
  }
}`)
