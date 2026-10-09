package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"echopoint-cli/internal/api"
)

func TestAdministrationRoutesStayOffMCPEvenWhenAnnotatedSafe(t *testing.T) {
	spec, err := openapi3.NewLoader().LoadFromData(api.OpenAPISpec)
	if err != nil {
		t.Fatal(err)
	}
	path := spec.Paths.Value("/administrations/cloud-fleet")
	if path == nil || path.Get == nil || path.Put == nil {
		t.Fatal("matching Cloud fleet API contract missing")
	}
	// Simulate a contract annotation mistake on every administration operation.
	// The CLI mirrors these routes internally, but annotations cannot expose them.
	for route, item := range spec.Paths.Map() {
		if route == "/administrations" || strings.HasPrefix(route, "/administrations/") {
			for _, operation := range item.Operations() {
				operation.Extensions[extAITool] = true
				delete(operation.Extensions, extAIDanger)
			}
		}
	}
	permissiveSpec, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := buildCatalog(permissiveSpec)
	if err != nil {
		t.Fatal(err)
	}
	ordinaryToolFound := false
	for _, tool := range catalog {
		if tool.PathTemplate == "/administrations" || strings.HasPrefix(tool.PathTemplate, "/administrations/") {
			t.Errorf("privileged admin operation %q became an MCP tool", tool.Name)
		}
		ordinaryToolFound = ordinaryToolFound || tool.Name == "list_flows"
	}
	if !ordinaryToolFound {
		t.Error("ordinary list_flows tool was excluded")
	}
}

func TestAdministrationExclusionUsesExactPathBoundary(t *testing.T) {
	const spec = `{
  "openapi": "3.0.3",
  "info": {"title": "Exposure boundary", "version": "1"},
  "paths": {
    "/administrations": {"get": {"operationId": "getAdministration", "x-ai-tool": true, "responses": {"200": {"description": "OK"}}}},
    "/administrations/": {"post": {"operationId": "createAdministration", "x-ai-tool": true, "x-ai-danger": false, "responses": {"200": {"description": "OK"}}}},
    "/administrations/cloud-fleet": {"put": {"operationId": "updateCloudFleet", "x-ai-tool": true, "responses": {"200": {"description": "OK"}}}},
    "/administrations/nested/jobs": {"delete": {"operationId": "deleteAdminJob", "x-ai-tool": true, "responses": {"200": {"description": "OK"}}}},
    "/flows": {"get": {"operationId": "listFlows", "x-ai-tool": true, "responses": {"200": {"description": "OK"}}}},
    "/administrations-report": {"get": {"operationId": "listReports", "x-ai-tool": true, "responses": {"200": {"description": "OK"}}}},
    "/administrationsOther": {"get": {"operationId": "listOther", "x-ai-tool": true, "responses": {"200": {"description": "OK"}}}},
    "/projects/administrations": {"get": {"operationId": "listProjectAdministration", "x-ai-tool": true, "responses": {"200": {"description": "OK"}}}}
  }
}`
	catalog, err := buildCatalog([]byte(spec))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"list_flows": "/flows", "list_reports": "/administrations-report",
		"list_other": "/administrationsOther", "list_project_administration": "/projects/administrations",
	}
	if len(catalog) != len(want) {
		t.Fatalf("got %d tools, want %d: %+v", len(catalog), len(want), catalog)
	}
	for _, tool := range catalog {
		if want[tool.Name] != tool.PathTemplate {
			t.Errorf("unexpected tool %q for path %q", tool.Name, tool.PathTemplate)
		}
		delete(want, tool.Name)
	}
	if len(want) != 0 {
		t.Errorf("ordinary tools missing: %v", want)
	}
}
