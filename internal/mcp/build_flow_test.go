package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateFlatFlow(t *testing.T) {
	tests := []struct {
		name     string
		flow     flatFlow
		wantOK   bool
		contains string // substring expected in a problem when wantOK is false
	}{
		{
			name: "valid flow with refs and module",
			flow: flatFlow{
				Name: "ok",
				Nodes: []flatNode{
					{ID: "setup", Kind: "module", FlowID: "abc",
						Outputs: map[string]string{"token": "login.token"}},
					{ID: "call", Method: "GET", URL: "{{base}}/x",
						Headers: map[string]string{"Authorization": "Bearer {{setup.token}}"},
						Outputs: map[string]string{"id": "$.id"},
						Asserts: []flatAssert{{Status: 200}}},
					{ID: "get", Method: "GET", URL: "{{base}}/x/{{call.id}}",
						Asserts: []flatAssert{{Path: "$.id", Op: "notEmpty"}}},
				},
				Edges: []string{"setup>call", "call>get"},
			},
			wantOK: true,
		},
		{
			name:     "missing name",
			flow:     flatFlow{Nodes: []flatNode{{ID: "a", Method: "GET", URL: "u"}}, Edges: nil},
			contains: "name is required",
		},
		{
			name: "ref to unknown node",
			flow: flatFlow{Name: "n", Nodes: []flatNode{
				{ID: "a", Method: "GET", URL: "{{ghost.id}}"},
			}},
			contains: `node "ghost" does not exist`,
		},
		{
			name: "ref to undeclared output",
			flow: flatFlow{Name: "n", Nodes: []flatNode{
				{ID: "a", Method: "POST", URL: "u", Outputs: map[string]string{"id": "$.id"}},
				{ID: "b", Method: "GET", URL: "{{a.missing}}"},
			}, Edges: []string{"a>b"}},
			contains: `declares no output "missing"`,
		},
		{
			name: "dangling edge target",
			flow: flatFlow{Name: "n", Nodes: []flatNode{
				{ID: "a", Method: "GET", URL: "u"},
			}, Edges: []string{"a>nope"}},
			contains: `target node "nope" not found`,
		},
		{
			name: "malformed edge",
			flow: flatFlow{Name: "n", Nodes: []flatNode{
				{ID: "a", Method: "GET", URL: "u"},
			}, Edges: []string{"a-b"}},
			contains: `must be "sourceId>targetId"`,
		},
		{
			name:     "request missing method and url",
			flow:     flatFlow{Name: "n", Nodes: []flatNode{{ID: "a"}}},
			contains: "has no method",
		},
		{
			name:     "module missing flow_id",
			flow:     flatFlow{Name: "n", Nodes: []flatNode{{ID: "a", Kind: "module"}}},
			contains: "has no flow_id",
		},
		{
			name: "duplicate node id",
			flow: flatFlow{Name: "n", Nodes: []flatNode{
				{ID: "a", Method: "GET", URL: "u"},
				{ID: "a", Method: "GET", URL: "u"},
			}},
			contains: "duplicate node id",
		},
		{
			name: "assert without status or path",
			flow: flatFlow{Name: "n", Nodes: []flatNode{
				{ID: "a", Method: "GET", URL: "u", Asserts: []flatAssert{{}}},
			}},
			contains: "needs status or path",
		},
		{
			name: "bare env ref is allowed",
			flow: flatFlow{Name: "n", Nodes: []flatNode{
				{ID: "a", Method: "GET", URL: "{{baseUrl}}/x"},
			}},
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			problems := validateFlatFlow(tt.flow)
			if tt.wantOK {
				if len(problems) != 0 {
					t.Fatalf("expected no problems, got: %v", problems)
				}
				return
			}
			if !containsSubstr(problems, tt.contains) {
				t.Fatalf("expected a problem containing %q, got: %v", tt.contains, problems)
			}
		})
	}
}

func TestBuildFlowDoc(t *testing.T) {
	flow := flatFlow{
		Name: "demo",
		Tags: []string{"demo"},
		Nodes: []flatNode{
			{ID: "setup", Kind: "module", FlowID: "abc",
				Inputs:  map[string]string{"prefix": "p"},
				Outputs: map[string]string{"token": "login.token"}},
			{ID: "call", Method: "POST", URL: "{{base}}/x",
				Headers: map[string]string{"Authorization": "Bearer {{setup.token}}"},
				Body:    `{"k":"v"}`,
				Outputs: map[string]string{"id": "$.id"},
				Asserts: []flatAssert{{Status: 201}, {Path: "$.name", Value: "v"}}},
			{ID: "cleanup", Method: "DELETE", URL: "{{base}}/x/{{call.id}}",
				RunWhen: "always", Asserts: []flatAssert{{Status: 204}}},
		},
		Edges: []string{"setup>call", "call>cleanup"},
	}

	doc := buildFlowDoc(flow)

	// Round-trip through JSON to assert the wire shape the backend expects.
	raw, _ := json.Marshal(doc)
	var got struct {
		Name       string `json:"name"`
		AutoLayout bool   `json:"auto_layout"`
		Tags       []string
		FlowDef    struct {
			Version string
			Nodes   []struct {
				ID         string         `json:"id"`
				Type       string         `json:"type"`
				RunWhen    string         `json:"run_when"`
				Data       map[string]any `json:"data"`
				Outputs    []map[string]any
				Assertions []map[string]any
			}
			Edges []struct {
				ID, Source, Target, Type string
			}
		} `json:"flow_definition"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal doc: %v", err)
	}

	if got.Name != "demo" || !got.AutoLayout || got.FlowDef.Version != "1.0" {
		t.Fatalf("unexpected top-level: %+v", got)
	}
	if len(got.FlowDef.Nodes) != 3 || len(got.FlowDef.Edges) != 2 {
		t.Fatalf("node/edge count wrong: %+v", got.FlowDef)
	}

	module := got.FlowDef.Nodes[0]
	if module.Type != "module" || module.Data["flow_id"] != "abc" {
		t.Fatalf("module node wrong: %+v", module)
	}
	if module.Data["output_bindings"] == nil || module.Data["input_bindings"] == nil {
		t.Fatalf("module bindings missing: %+v", module.Data)
	}

	call := got.FlowDef.Nodes[1]
	if call.Type != "request" || call.Data["method"] != "POST" {
		t.Fatalf("call node wrong: %+v", call)
	}
	if call.RunWhen != "on_success" {
		t.Fatalf("default run_when should be on_success, got %q", call.RunWhen)
	}
	if len(call.Outputs) != 1 || call.Outputs[0]["name"] != "id" {
		t.Fatalf("call outputs wrong: %+v", call.Outputs)
	}
	// First assert is the status shorthand -> statusCode equals 201.
	a0 := call.Assertions[0]
	if a0["extractor_type"] != "statusCode" || a0["operator_type"] != "equals" {
		t.Fatalf("status assert wrong: %+v", a0)
	}
	if od, _ := a0["operator_data"].(map[string]any); od["value"] != "201" {
		t.Fatalf("status assert value wrong: %+v", a0)
	}
	// Second assert is a jsonPath equals.
	a1 := call.Assertions[1]
	if a1["extractor_type"] != "jsonPath" || a1["operator_type"] != "equals" {
		t.Fatalf("path assert wrong: %+v", a1)
	}

	cleanup := got.FlowDef.Nodes[2]
	if cleanup.RunWhen != "always" {
		t.Fatalf("cleanup run_when should be always, got %q", cleanup.RunWhen)
	}
	if got.FlowDef.Edges[0].Source != "setup" || got.FlowDef.Edges[0].Type != "success" {
		t.Fatalf("edge wrong: %+v", got.FlowDef.Edges[0])
	}
}

func TestBuildFlowInputSchemaIsValidJSON(t *testing.T) {
	var m map[string]any
	if err := json.Unmarshal(buildFlowInputSchema, &m); err != nil {
		t.Fatalf("input schema is not valid JSON: %v", err)
	}
	if m["type"] != "object" {
		t.Fatalf("input schema root type should be object")
	}
}

func containsSubstr(items []string, sub string) bool {
	for _, s := range items {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
