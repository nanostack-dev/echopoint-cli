package mcp

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"echopoint-cli/internal/api"
)

func TestCloudFleetAdministrationStaysOffMCP(t *testing.T) {
	spec, err := openapi3.NewLoader().LoadFromData(api.OpenAPISpec)
	if err != nil {
		t.Fatal(err)
	}
	path := spec.Paths.Value("/administrations/cloud-fleet")
	if path == nil || path.Get == nil || path.Put == nil {
		t.Fatal("matching Cloud fleet API contract missing")
	}
	for _, operation := range []*openapi3.Operation{path.Get, path.Put} {
		if !present(operation.Extensions[extAIDanger]) {
			t.Errorf("%s does not explicitly exclude privileged product administration", operation.OperationID)
		}
	}
	catalog, err := buildCatalog(api.OpenAPISpec)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range catalog {
		if tool.Name == "get_cloud_fleet" || tool.Name == "update_cloud_fleet" {
			t.Errorf("privileged admin operation %q became an MCP tool", tool.Name)
		}
	}
}
