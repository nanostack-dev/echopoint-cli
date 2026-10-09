package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/client"
)

func TestStatusPageToolsDispatchWithTenantScope(t *testing.T) {
	catalog, err := buildCatalog(api.OpenAPISpec)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, method, path, args string }{
		{"get_status_page", "GET", "/status-pages/current", `{}`},
		{"save_status_page", "PUT", "/status-pages/current", `{"expected_draft_version":7,"slug":"example","config":{"theme":"orbit","services":[{"id":"api","messages":{"operational":"Checkout is available","outage":"Checkout is unavailable","unknown":"We are checking checkout"}}]},"probe_bindings":[{"service_id":"api","probe_id":"probe"}]}`},
		{"get_status_page_binding_options", "GET", "/status-pages/binding-options", `{"schedule_id":"schedule","flow_id":"flow"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				if r.Method != tc.method || r.URL.Path != tc.path {
					t.Errorf("request = %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("X-Api-Key") != "test-key" || r.Header.Get("X-Organization-ID") != "org_test" {
					t.Error("tenant auth missing")
				}
				if tc.method == "PUT" {
					data, _ := io.ReadAll(r.Body)
					var input map[string]any
					if err := json.Unmarshal(data, &input); err != nil {
						t.Error(err)
					}
					if input["expected_draft_version"] != float64(7) || input["slug"] != "example" {
						t.Errorf("body = %s", data)
					}
					bindings, ok := input["probe_bindings"].([]any)
					if !ok || len(bindings) != 1 {
						t.Errorf("probe mapping dropped: %s", data)
					}
				}
				if tc.name == "get_status_page_binding_options" &&
					(r.URL.Query().Get("flow_id") != "flow" || r.URL.Query().Get("schedule_id") != "schedule") {
					t.Error("missing query")
				}
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, `{"ok":true}`)
			}))
			defer server.Close()
			cli, err := client.NewWithAPIKey(server.URL, "test-key", "org_test", time.Second)
			if err != nil {
				t.Fatal(err)
			}
			for _, td := range catalog {
				if td.Name != tc.name {
					continue
				}
				result, err := makeHandler(
					cli,
					td,
				)(
					t.Context(),
					&mcpsdk.CallToolRequest{
						Params: &mcpsdk.CallToolParamsRaw{Name: tc.name, Arguments: json.RawMessage(tc.args)},
					},
				)
				if err != nil {
					t.Fatal(err)
				}
				if result.IsError {
					t.Fatalf("tool failed: %+v", result)
				}
			}
			if !called {
				t.Fatal("tool never dispatched")
			}
		})
	}
}
