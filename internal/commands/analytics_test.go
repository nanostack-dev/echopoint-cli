package commands

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"echopoint-cli/internal/output"
)

func analyticsSummaryBody() map[string]any {
	return map[string]any{
		"window": "24h", "window_start": "2026-10-11T13:00:00Z", "window_end": "2026-10-12T13:00:00Z",
		"bucket_size": "hour", "runs": 120, "failed_runs": 8, "cancelled_runs": 1,
		"run_ms_total": 252_000, "run_ms_average": 2_100, "run_ms_p95": 5_400, "cloud_run_ms_total": 230_000,
		"measured_runs": 110, "http_calls": 3400, "failed_http_calls": 25,
		"run_failures": []map[string]any{
			{"error_code": "HTTP_CALL_LIMIT_EXCEEDED", "runs": 5},
			{"runs": 3},
		},
		"call_failures": map[string]any{
			"status_4xx": 10, "status_5xx": 8, "dns": 1, "connect": 2, "tls": 0, "timeout": 4, "no_response": 0,
		},
		"series": []map[string]any{},
		// a field this CLI does not know yet must still reach -o json
		"added_later": "kept",
	}
}

func flowAnalyticsBody() map[string]any {
	return map[string]any{
		"window": "7d", "window_start": "2026-10-06T00:00:00Z", "window_end": "2026-10-13T00:00:00Z",
		"flows": []map[string]any{
			{
				"flow_id": idFor("1").String(), "flow_name": "Checkout", "runs": 40, "failed_runs": 2,
				"cancelled_runs": 0, "run_ms_total": 84_000, "run_ms_average": 2_100, "run_ms_p95": 4_800,
				"run_ms_max": 9_000, "cloud_run_ms_total": 84_000, "measured_runs": 40, "http_calls": 640,
				"failed_http_calls": 3, "http_call_peak": 22, "last_completed_at": "2026-10-12T12:00:00Z",
				"last_status": "completed",
			},
			{
				"flow_id": idFor("2").String(), "flow_name": "", "runs": 3, "failed_runs": 3,
				"cancelled_runs": 0, "run_ms_total": 900, "run_ms_average": 300, "run_ms_p95": 450,
				"run_ms_max": 450, "cloud_run_ms_total": 0, "measured_runs": 0, "http_calls": 0,
				"failed_http_calls": 0, "last_completed_at": "2026-10-10T08:00:00Z", "last_status": "failed",
			},
		},
	}
}

func endpointAnalyticsBody() map[string]any {
	failures := map[string]any{
		"status_4xx": 0, "status_5xx": 1, "dns": 0, "connect": 0, "tls": 0, "timeout": 0, "no_response": 0,
	}
	return map[string]any{
		"window": "7d", "window_start": "2026-10-06T00:00:00Z", "window_end": "2026-10-13T00:00:00Z",
		"flow_id": idFor("1").String(), "truncated": true,
		"endpoints": []map[string]any{
			{
				"method": "GET", "host": "api.example.com", "path_template": "/users/{{userId}}", "flows": 1,
				"calls": 500, "failed_calls": 1, "failures": failures, "duration_ms_average": 120,
				"duration_ms_max": 2_300, "duration_ms_p95_at_most": 250,
			},
			{
				"method": "POST", "host": "api.example.com", "path_template": "/reports", "flows": 1,
				"calls": 12, "failed_calls": 0, "failures": failures, "duration_ms_average": 9_000,
				"duration_ms_max": 45_000,
			},
		},
	}
}

func TestAnalyticsCommandsReadTheOrganizationWindowAndPrintTheAPIBody(t *testing.T) {
	flowID := idFor("1").String()
	for _, tc := range []struct {
		name  string
		path  string
		body  map[string]any
		args  []string
		query url.Values
	}{
		{
			"summary", "/analytics/summary", analyticsSummaryBody(),
			[]string{"summary", "--window", "24h"}, url.Values{"window": {"24h"}},
		},
		{
			"flows", "/analytics/flows", flowAnalyticsBody(),
			[]string{"flows"}, url.Values{"window": {"7d"}},
		},
		{
			"endpoints", "/analytics/endpoints", endpointAnalyticsBody(),
			[]string{"endpoints", "--window", "30d", "--flow-id", flowID},
			url.Values{"window": {"30d"}, "flow_id": {flowID}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := newAPIStub(t).on(http.MethodGet, tc.path, http.StatusOK, tc.body)
			state := stub.state(t)
			state.OutputFormat = output.FormatJSON

			stdout, _, err := execute(t, newAnalyticsCmd(state), "", tc.args...)
			if err != nil {
				t.Fatal(err)
			}

			requests := stub.requestsTo(http.MethodGet, tc.path)
			if len(requests) != 1 || stub.requestCount() != 1 {
				t.Fatalf("sent %d requests, want one GET %s", stub.requestCount(), tc.path)
			}
			request := requests[0]
			if request.header.Get("X-Api-Key") != "test-key" || request.header.Get("X-Organization-Id") != "org_test" {
				t.Errorf("request lost its authentication or organization: %v", request.header)
			}
			if query, _ := url.ParseQuery(request.query); !reflect.DeepEqual(query, tc.query) {
				t.Errorf("query = %v, want %v", query, tc.query)
			}
			var got, want any
			if err := json.Unmarshal([]byte(stdout), &got); err != nil {
				t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
			}
			raw, _ := json.Marshal(tc.body)
			_ = json.Unmarshal(raw, &want)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("stdout is not the API body:\n%s", stdout)
			}
		})
	}
}

func TestAnalyticsYAMLKeepsTheAPIKeyNames(t *testing.T) {
	stub := newAPIStub(t).on(http.MethodGet, "/analytics/summary", http.StatusOK, analyticsSummaryBody())
	state := stub.state(t)
	state.OutputFormat = output.FormatYAML

	stdout, _, err := execute(t, newAnalyticsCmd(state), "", "summary")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"failed_http_calls: 25", "added_later: kept", "status_4xx: 10"} {
		if !strings.Contains(stdout, key) {
			t.Errorf("yaml lacks %q:\n%s", key, stdout)
		}
	}
}

func TestAnalyticsRejectsAnUnknownWindowWithoutARequest(t *testing.T) {
	for _, leaf := range []string{"summary", "flows", "endpoints"} {
		stub := newAPIStub(t)
		_, _, err := execute(t, newAnalyticsCmd(stub.state(t)), "", leaf, "--window", "2d")
		if err == nil || err.Error() != `invalid --window "2d": use 24h, 7d or 30d` {
			t.Errorf("%s: err = %v", leaf, err)
		}
		if stub.requestCount() != 0 {
			t.Errorf("%s sent %d requests", leaf, stub.requestCount())
		}
	}
}

func TestAnalyticsEndpointsRejectsANonFlowIDWithoutARequest(t *testing.T) {
	stub := newAPIStub(t)
	_, _, err := execute(t, newAnalyticsCmd(stub.state(t)), "", "endpoints", "--flow-id", "checkout")
	if err == nil || err.Error() != `--flow-id: "checkout" is not a flow id; list the flows with: echopoint flow list` {
		t.Errorf("err = %v", err)
	}
	if stub.requestCount() != 0 {
		t.Errorf("sent %d requests", stub.requestCount())
	}
}

func TestAnalyticsReportsTheAPIError(t *testing.T) {
	stub := newAPIStub(t).on(http.MethodGet, "/analytics/flows", http.StatusForbidden,
		map[string]any{"errors": []map[string]any{{"code": "FORBIDDEN", "message": "missing permission flows:read"}}})
	_, _, err := execute(t, newAnalyticsCmd(stub.state(t)), "", "flows")
	if err == nil || err.Error() != "api error (403): missing permission flows:read" {
		t.Errorf("err = %v", err)
	}
}

func TestAnalyticsSummaryTable(t *testing.T) {
	stub := newAPIStub(t).on(http.MethodGet, "/analytics/summary", http.StatusOK, analyticsSummaryBody())

	stdout, _, err := execute(t, newAnalyticsCmd(stub.tableState(t)), "", "summary", "--window", "24h")
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"Window: 24h, 2026-10-11 13:00 to 2026-10-12 13:00 UTC",
		"Failed          8 (6.7%)",
		"Run time        total 4m12s, avg 2.1s, p95 5.4s",
		"Cloud run time  3m50s",
		"HTTP calls      3400",
		"Failed calls    25 (4xx 10, 5xx 8, dns 1, connect 2, timeout 4)",
		"HTTP_CALL_LIMIT_EXCEEDED  5",
		"(no code)                 3",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("table lacks %q:\n%s", want, stdout)
		}
	}
}

func TestAnalyticsSummaryTableOfAnEmptyWindow(t *testing.T) {
	body := analyticsSummaryBody()
	for _, key := range []string{"runs", "failed_runs", "cancelled_runs", "run_ms_total", "cloud_run_ms_total",
		"measured_runs", "http_calls", "failed_http_calls"} {
		body[key] = 0
	}
	delete(body, "run_ms_average")
	delete(body, "run_ms_p95")
	body["run_failures"] = []any{}
	body["call_failures"] = map[string]any{
		"status_4xx": 0, "status_5xx": 0, "dns": 0, "connect": 0, "tls": 0, "timeout": 0, "no_response": 0,
	}
	stub := newAPIStub(t).on(http.MethodGet, "/analytics/summary", http.StatusOK, body)

	stdout, _, err := execute(t, newAnalyticsCmd(stub.tableState(t)), "", "summary")
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"Failed          0 (-)", "total 0ms, avg -, p95 -", "Failed calls    0\n"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("table lacks %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "error codes") {
		t.Errorf("an empty window lists error codes:\n%s", stdout)
	}
}

func TestAnalyticsFlowsTable(t *testing.T) {
	stub := newAPIStub(t).on(http.MethodGet, "/analytics/flows", http.StatusOK, flowAnalyticsBody())

	stdout, _, err := execute(t, newAnalyticsCmd(stub.tableState(t)), "", "flows")
	if err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 5 {
		t.Fatalf("want a window line, a blank line, a header and two flows:\n%s", stdout)
	}
	for i, want := range [][]string{
		{"ID", "Name", "Runs", "Failed", "Avg", "P95", "Calls", "Peak"},
		{idFor("1").String(), "Checkout", "40", "2", "2.1s", "4.8s", "640", "22"},
		{idFor("2").String(), "(deleted)", "3", "3", "300ms", "450ms", "0", "-"},
	} {
		if got := strings.Fields(lines[i+2]); !reflect.DeepEqual(got, want) {
			t.Errorf("row %d = %v, want %v", i, got, want)
		}
	}
}

func TestAnalyticsEndpointsTable(t *testing.T) {
	stub := newAPIStub(t).on(http.MethodGet, "/analytics/endpoints", http.StatusOK, endpointAnalyticsBody())

	stdout, _, err := execute(t, newAnalyticsCmd(stub.tableState(t)), "", "endpoints", "--flow-id", idFor("1").String())
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(stdout, "Window: 7d, 2026-10-06 00:00 to 2026-10-13 00:00 UTC, flow "+idFor("1").String()) {
		t.Errorf("window line:\n%s", stdout)
	}
	lines := strings.Split(stdout, "\n")
	for i, want := range [][]string{
		{"Method", "Host", "Path", "Calls", "Failed", "Avg", "Max", "P95"},
		{"GET", "api.example.com", "/users/{{userId}}", "500", "1", "120ms", "2.3s", "<=250ms"},
		{"POST", "api.example.com", "/reports", "12", "0", "9.0s", "45.0s", ">10s"},
	} {
		if got := strings.Fields(lines[i+2]); !reflect.DeepEqual(got, want) {
			t.Errorf("row %d = %v, want %v", i, got, want)
		}
	}
	if !strings.Contains(stdout, "Only the 2 endpoints with the most calls are listed.") {
		t.Errorf("a truncated list does not say so:\n%s", stdout)
	}
}

func TestAnalyticsTablesOfAnEmptyWindow(t *testing.T) {
	flows := flowAnalyticsBody()
	flows["flows"] = []any{}
	endpoints := endpointAnalyticsBody()
	endpoints["endpoints"] = []any{}
	endpoints["truncated"] = false
	delete(endpoints, "flow_id")
	stub := newAPIStub(t).
		on(http.MethodGet, "/analytics/flows", http.StatusOK, flows).
		on(http.MethodGet, "/analytics/endpoints", http.StatusOK, endpoints)

	for leaf, want := range map[string]string{
		"flows":     "No flow finished a run in the window.",
		"endpoints": "No HTTP call was recorded in the window.",
	} {
		stdout, _, err := execute(t, newAnalyticsCmd(stub.tableState(t)), "", leaf)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(stdout, want+"\n") || strings.Contains(stdout, "Calls") {
			t.Errorf("%s:\n%s", leaf, stdout)
		}
	}
}

func TestFormatMillis(t *testing.T) {
	for ms, want := range map[int64]string{
		0: "0ms", 850: "850ms", 999: "999ms", 1_000: "1.0s", 2_149: "2.1s", 59_949: "59.9s",
		60_000: "1m0s", 252_400: "4m12s", 3_723_000: "1h2m3s",
	} {
		if got := formatMillis(ms); got != want {
			t.Errorf("formatMillis(%d) = %q, want %q", ms, got, want)
		}
	}
}

// TestAnalyticsThroughTheBinaryAndMCP runs the released surface end to end: the
// command with a profile, environment credentials and -o json, and the stdio MCP
// tools of the same operations.
func TestAnalyticsThroughTheBinaryAndMCP(t *testing.T) {
	flowID := idFor("1").String()
	stub := newAPIStub(t).
		on(http.MethodGet, "/analytics/summary", http.StatusOK, analyticsSummaryBody()).
		on(http.MethodGet, "/analytics/endpoints", http.StatusOK, endpointAnalyticsBody())
	isolateCLIEnvironment(t, stub.server.URL)

	stdout, stderr, code := runCLI(t, "analytics", "summary", "--window", "24h", "--profile", "default", "-o", "json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var summary map[string]any
	if err := json.Unmarshal([]byte(stdout), &summary); err != nil || summary["added_later"] != "kept" {
		t.Fatalf("stdout is not the API body: %v\n%s", err, stdout)
	}
	if _, stderr, code := runCLI(t, "analytics", "flows", "--window", "1y"); code == 0 ||
		!strings.Contains(stderr, `invalid --window "1y"`) {
		t.Errorf("a bad window: exit %d, stderr %q", code, stderr)
	}

	command := exec.Command(echopointBinary(t), "mcp", "--profile", "default")
	command.Stderr = io.Discard
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "analytics-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcpsdk.CommandTransport{Command: command}, nil)
	if err != nil {
		t.Fatalf("connect to CLI stdio MCP process: %v", err)
	}
	defer session.Close()

	result, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "list_endpoint_analytics", Arguments: map[string]any{"window": "7d", "flow_id": flowID},
	})
	if err != nil || result.IsError {
		t.Fatalf("list_endpoint_analytics: result=%v, error=%v", result, err)
	}
	requests := stub.requestsTo(http.MethodGet, "/analytics/endpoints")
	if len(requests) != 1 || requests[0].header.Get("X-Api-Key") != "golden-key" ||
		requests[0].header.Get("X-Organization-Id") != "org_test" {
		t.Fatalf("the MCP tool did not send one authenticated, scoped request: %+v", requests)
	}
	if query, _ := url.ParseQuery(requests[0].query); query.Get("window") != "7d" || query.Get("flow_id") != flowID {
		t.Errorf("query = %q", requests[0].query)
	}
	_, err = session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "rebuild_execution_analytics", Arguments: map[string]any{},
	})
	if err == nil || !strings.Contains(err.Error(), `unknown tool "rebuild_execution_analytics"`) {
		t.Errorf("rebuild_execution_analytics: %v", err)
	}
	if len(stub.requestsTo(http.MethodPost, "/administrations/analytics/rebuild")) != 0 {
		t.Error("the administration rebuild was called")
	}
}
