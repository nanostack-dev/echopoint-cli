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

Asserts: {"status":200} is shorthand for statusCode==200. Otherwise use
{"path":"$.x","op":"equals","value":"y"} — op defaults to equals and supports
notEquals, contains, notContains, empty, notEmpty, greaterThan, lessThan,
startsWith, endsWith, regex (empty/notEmpty need no value).

Nodes: kind "request" (default) needs method+url; kind "module" runs a sub-flow
and needs flow_id, with "outputs" mapping a local name to "childNodeId.childKey".
run_when defaults on_success; use "always" for cleanup nodes so they run even
after an upstream failure.

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
     "asserts": [{"status": 200}, {"path": "$.id", "op": "notEmpty"}]},
    {"id": "cleanup", "method": "DELETE", "url": "{{baseUrl}}/v1/widgets/{{create.widgetId}}",
     "headers": {"Authorization": "Bearer {{token}}"},
     "run_when": "always", "asserts": [{"status": 204}]}
  ],
  "edges": ["create>get", "get>cleanup"]
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
      "description": "Flat list of request and module nodes.",
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
            "description": "request: outputName -> jsonPath (e.g. {\"id\":\"$.id\"}). module: localName -> childNodeId.childKey."
          },
          "asserts": {
            "type": "array",
            "description": "request: assertions on the response.",
            "items": {
              "type": "object",
              "properties": {
                "status": {"type": "integer", "description": "Shorthand: assert HTTP status equals this."},
                "path": {"type": "string", "description": "jsonPath to extract from the body."},
                "op": {"type": "string", "description": "Operator; defaults equals."},
                "value": {"type": "string", "description": "Expected value for operators that need one."}
              }
            }
          },
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
