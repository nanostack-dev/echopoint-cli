package mcp

import (
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/client"
)

func TestTheCatalogExposesMonitorsAndNoProbeOrBudgetTool(t *testing.T) {
	catalog, err := buildCatalog(api.OpenAPISpec)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, td := range catalog {
		names[td.Name] = td.PathTemplate
	}
	for _, name := range []string{
		"list_flow_schedules", "create_flow_schedule", "get_flow_schedule", "update_flow_schedule",
		"delete_flow_schedule", "pause_flow_schedule", "resume_flow_schedule", "run_monitor",
		"list_flow_schedule_runs", "get_flow_schedule_run", "preview_schedule_tags", "estimate_monitor",
	} {
		if !strings.HasPrefix(names[name], "/monitors") {
			t.Errorf("%s = %q, want a /monitors tool", name, names[name])
		}
	}
	for name, path := range names {
		if strings.Contains(name, "probe") || strings.Contains(path, "probes") ||
			strings.Contains(path, "flow-schedules") {
			t.Errorf("tool %s on %s survives the removed probe and flow-schedule API", name, path)
		}
	}
	for _, name := range []string{"get_monitor_cloud_budget", "get_status_page_binding_options"} {
		if _, exists := names[name]; exists {
			t.Errorf("%s must not be a tool", name)
		}
	}
}

func TestMonitorToolsDispatchScopeAndArguments(t *testing.T) {
	catalog, err := buildCatalog(api.OpenAPISpec)
	if err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]toolDef, len(catalog))
	for _, td := range catalog {
		byName[td.Name] = td
	}
	const monitor, run = "8fc078c1-d8e6-4059-9e6f-b69fb52b7b17", "1d02c6a0-0000-4000-8000-000000000001"
	for _, tc := range []struct {
		name, method, path, args string
		required                 []string
	}{
		{"list_flow_schedules", "GET", "/monitors", `{"limit":7,"offset":2,"enabled":true,"has_health_policy":true}`, []string{"limit"}},
		{"create_flow_schedule", "POST", "/monitors", `{"name":"API","runner_type":"cloud","tags":["checkout"],"interval_seconds":60,"health_policy":{"confirmation_runs":2,"recovery_runs":2,"freshness_seconds":180}}`, []string{"name", "runner_type"}},
		{"get_flow_schedule", "GET", "/monitors/" + monitor, `{"monitorId":"` + monitor + `"}`, []string{"monitorId"}},
		{"update_flow_schedule", "PUT", "/monitors/" + monitor, `{"monitorId":"` + monitor + `","name":"API","runner_type":"cloud","cron_expression":"0 9 * * *"}`, []string{"monitorId", "name", "runner_type"}},
		{"delete_flow_schedule", "DELETE", "/monitors/" + monitor, `{"monitorId":"` + monitor + `"}`, []string{"monitorId"}},
		{"pause_flow_schedule", "POST", "/monitors/" + monitor + "/pause", `{"monitorId":"` + monitor + `"}`, []string{"monitorId"}},
		{"resume_flow_schedule", "POST", "/monitors/" + monitor + "/resume", `{"monitorId":"` + monitor + `"}`, []string{"monitorId"}},
		{"run_monitor", "POST", "/monitors/" + monitor + "/run", `{"monitorId":"` + monitor + `"}`, []string{"monitorId"}},
		{"list_flow_schedule_runs", "GET", "/monitors/" + monitor + "/runs", `{"monitorId":"` + monitor + `","limit":7,"offset":2}`, []string{"monitorId", "limit"}},
		{"get_flow_schedule_run", "GET", "/monitors/" + monitor + "/runs/" + run, `{"monitorId":"` + monitor + `","runId":"` + run + `"}`, []string{"monitorId", "runId"}},
		{"preview_schedule_tags", "POST", "/monitors/preview-tags", `{"tags":["checkout"],"tag_match_mode":"all","ignore_modules":false}`, []string{"tags"}},
		{"estimate_monitor", "POST", "/monitors/estimate", `{"tags":["checkout"],"runner_type":"cloud","interval_seconds":300,"health_policy":true,"monitor_id":"` + monitor + `"}`, []string{"runner_type"}},
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
			var arguments map[string]any
			if err := json.Unmarshal([]byte(tc.args), &arguments); err != nil {
				t.Fatal(err)
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
				if slices.Contains(slices.Collect(maps.Values(td.Locations)), locBody) {
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					for key, location := range td.Locations {
						if _, sent := arguments[key]; location == locBody && sent &&
							!reflect.DeepEqual(body[key], arguments[key]) {
							t.Errorf("lost body field %s", key)
						}
					}
				}
				for key, location := range td.Locations {
					if _, exists := arguments[key]; exists && location == locQuery && r.URL.Query().Get(key) == "" {
						t.Errorf("lost query field %s", key)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"id":"`+monitor+`"}`)
			}))
			defer server.Close()
			cli, err := client.NewWithAPIKey(server.URL, "test-key", "org_test", time.Second)
			if err != nil {
				t.Fatal(err)
			}
			result, err := makeHandler(cli, td)(
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
