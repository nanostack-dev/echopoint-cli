package mcp

import (
	"strings"
	"testing"
)

func TestSplitArgumentsMissingPathParamNamesIt(t *testing.T) {
	td := toolDef{
		Name:         "delete_flow",
		PathTemplate: "/flows/{id}",
		Locations:    map[string]paramLoc{"id": locPath},
	}

	_, _, _, err := splitArguments(td, map[string]any{"flowId": "flow_123"})
	if err == nil {
		t.Fatal("expected an error when path parameter id is omitted")
	}
	if !strings.Contains(err.Error(), "missing required path parameter id") {
		t.Fatalf("error should name the expected parameter, got: %v", err)
	}
}

func TestSplitArgumentsSplitsByLocation(t *testing.T) {
	td := toolDef{
		Name:         "launch_flow",
		PathTemplate: "/flows/{id}/launch",
		Locations: map[string]paramLoc{
			"id":      locPath,
			"verbose": locQuery,
			"inputs":  locBody,
		},
	}

	path, query, body, err := splitArguments(td, map[string]any{
		"id":      "a/b",
		"verbose": true,
		"inputs":  map[string]any{"k": "v"},
	})
	if err != nil {
		t.Fatalf("splitArguments: %v", err)
	}
	if path != "/flows/a%2Fb/launch" {
		t.Errorf("path = %q, want escaped id substituted", path)
	}
	if query.Get("verbose") != "true" {
		t.Errorf("query verbose = %q, want true", query.Get("verbose"))
	}
	if _, ok := body["inputs"]; !ok || len(body) != 1 {
		t.Errorf("body = %v, want only inputs", body)
	}
}
