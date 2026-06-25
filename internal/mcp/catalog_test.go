package mcp

import (
	"encoding/json"
	"net/http"
	"testing"

	"echopoint-cli/internal/api"
)

func TestBuildCatalogFromEmbeddedSpec(t *testing.T) {
	tools, err := buildCatalog(api.OpenAPISpec)
	if err != nil {
		t.Fatalf("buildCatalog: %v", err)
	}

	byName := make(map[string]toolDef, len(tools))
	for _, td := range tools {
		byName[td.Name] = td
	}

	want := []string{
		"get_me", "list_flows", "get_flow", "search_flows",
		"launch_flow", "list_collections", "get_collection", "list_webhooks",
		"get_current_api_key",
	}
	for _, n := range want {
		if _, ok := byName[n]; !ok {
			t.Errorf("missing expected tool %q", n)
		}
	}
	if len(tools) != len(want) {
		names := make([]string, len(tools))
		for i, td := range tools {
			names[i] = td.Name
		}
		t.Errorf("got %d tools, want %d: %v", len(tools), len(want), names)
	}

	// launch_flow exercises the param+body merge.
	lf, ok := byName["launch_flow"]
	if !ok {
		t.Fatal("launch_flow not built")
	}
	if lf.Method != http.MethodPost {
		t.Errorf("launch_flow method = %q, want POST", lf.Method)
	}
	if lf.PathTemplate != "/flows/{id}/launch" {
		t.Errorf("launch_flow path = %q", lf.PathTemplate)
	}
	if lf.Locations["id"] != locPath {
		t.Errorf("launch_flow arg id location = %q, want path", lf.Locations["id"])
	}

	var schema map[string]any
	if err := json.Unmarshal(lf.InputSchema, &schema); err != nil {
		t.Fatalf("launch_flow schema not valid JSON: %v", err)
	}
	if schema["type"] != "object" {
		t.Errorf("launch_flow schema type = %v, want object", schema["type"])
	}
	if _, ok := schema["properties"].(map[string]any)["id"]; !ok {
		t.Error("launch_flow schema missing 'id' property")
	}

	// get_flow declares its `id` at the path-item level, not the operation
	// level: it must still be picked up as a required path argument.
	gf, ok := byName["get_flow"]
	if !ok {
		t.Fatal("get_flow not built")
	}
	if gf.Locations["id"] != locPath {
		t.Errorf("get_flow arg id location = %q, want path", gf.Locations["id"])
	}
	var gfSchema map[string]any
	if err := json.Unmarshal(gf.InputSchema, &gfSchema); err != nil {
		t.Fatalf("get_flow schema invalid: %v", err)
	}
	req, _ := gfSchema["required"].([]any)
	if len(req) != 1 || req[0] != "id" {
		t.Errorf("get_flow required = %v, want [id]", gfSchema["required"])
	}
}

func TestNewServerBuildsAllSchemas(t *testing.T) {
	// nil client is fine: NewServer only builds the catalog and validates each
	// input schema against the SDK's jsonschema; it never dispatches.
	if _, err := NewServer(nil, "test"); err != nil {
		t.Fatalf("NewServer: %v", err)
	}
}

func TestToSnake(t *testing.T) {
	cases := map[string]string{
		"getMe":          "get_me",
		"listFlows":      "list_flows",
		"launchFlow":     "launch_flow",
		"listAPIKeys":    "list_api_keys",
		"searchFlows":    "search_flows",
		"getOpenAPISync": "get_open_api_sync",
	}
	for in, want := range cases {
		if got := toSnake(in); got != want {
			t.Errorf("toSnake(%q) = %q, want %q", in, got, want)
		}
	}
}
