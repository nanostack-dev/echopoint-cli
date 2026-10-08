package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/client"
)

func TestProbeToolsDispatchScopeSourceAndRevision(t *testing.T) {
	catalog, err := buildCatalog(api.OpenAPISpec)
	if err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]toolDef, len(catalog))
	for _, td := range catalog {
		byName[td.Name] = td
	}
	for _, tc := range []struct {
		name, method, path, args string
		required                 []string
	}{
		{"list_probes", "GET", "/probes", `{"limit":7,"offset":2}`, []string{"limit"}},
		{"create_probe", "POST", "/probes", `{"config":{"name":"API","environment_key":"production"}}`, []string{"config"}},
		{"get_probe", "GET", "/probes/probe-id", `{"probeId":"probe-id"}`, []string{"probeId"}},
		{"update_probe", "PUT", "/probes/probe-id", `{"probeId":"probe-id","expected_revision":4,"config":{"name":"API","environment_key":"production"}}`, []string{"probeId", "expected_revision", "config"}},
		{"delete_probe", "DELETE", "/probes/probe-id", `{"probeId":"probe-id","expected_revision":4}`, []string{"probeId", "expected_revision"}},
		{"pause_probe", "POST", "/probes/probe-id/pause", `{"probeId":"probe-id","expected_revision":4}`, []string{"probeId", "expected_revision"}},
		{"resume_probe", "POST", "/probes/probe-id/resume", `{"probeId":"probe-id","expected_revision":4}`, []string{"probeId", "expected_revision"}},
		{"run_probe", "POST", "/probes/probe-id/run", `{"probeId":"probe-id","expected_revision":4}`, []string{"probeId", "expected_revision"}},
		{"list_probe_runs", "GET", "/probes/probe-id/runs", `{"probeId":"probe-id","limit":7,"offset":2}`, []string{"probeId", "limit"}},
		{"get_probe_run", "GET", "/probes/probe-id/runs/run-id", `{"probeId":"probe-id","runId":"run-id"}`, []string{"probeId", "runId"}},
		{"get_probe_source_options", "GET", "/probes/source-options", `{"flow_id":"flow","environment_key":"production"}`, []string{"flow_id"}},
		{"estimate_probe", "POST", "/probes/estimate", `{"tags":["production"],"tag_match_mode":"all","interval_seconds":60,"runner_type":"cloud","environment_key":"production"}`, []string{"interval_seconds", "runner_type"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			td, found := byName[tc.name]
			if !found {
				t.Fatal("missing tool")
			}
			var schema struct {
				Required   []string                   `json:"required"`
				Properties map[string]json.RawMessage `json:"properties"`
			}
			if err := json.Unmarshal(td.InputSchema, &schema); err != nil {
				t.Fatal(err)
			}
			for _, name := range tc.required {
				if !slices.Contains(schema.Required, name) {
					t.Errorf("missing required argument %s", name)
				}
			}
			if _, exists := schema.Properties["origin"]; exists {
				t.Fatal("manual run exposes a policy origin override")
			}
			if tc.name == "get_probe_source_options" {
				if _, exists := schema.Properties["version_id"]; exists {
					t.Fatal("source discovery still requires a published version")
				}
			}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != tc.method || r.URL.Path != tc.path {
					t.Errorf("request = %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("X-Api-Key") != "test-key" || r.Header.Get("X-Organization-ID") != "org_test" {
					t.Error("tenant authentication missing")
				}
				var arguments map[string]any
				if err := json.Unmarshal([]byte(tc.args), &arguments); err != nil {
					t.Fatal(err)
				}
				if tc.method == http.MethodPost || tc.method == http.MethodPut {
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					for key, location := range td.Locations {
						if location == locBody && !reflect.DeepEqual(body[key], arguments[key]) {
							t.Errorf("lost body field %s", key)
						}
					}
				}
				for key, location := range td.Locations {
					if location != locQuery {
						continue
					}
					if _, exists := arguments[key]; exists && r.URL.Query().Get(key) == "" {
						t.Errorf("lost query field %s", key)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"id":"probe-id","origin":"diagnostic"}`)
			}))
			defer server.Close()
			cli, err := client.NewWithAPIKey(server.URL, "test-key", "org_test", time.Second)
			if err != nil {
				t.Fatal(err)
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
			if err != nil || result.IsError || calls != 1 {
				t.Fatalf("dispatch result=%+v error=%v calls=%d", result, err, calls)
			}
		})
	}
}
