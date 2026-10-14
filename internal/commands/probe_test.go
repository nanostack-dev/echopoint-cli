package commands

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/output"
)

const testProbeID = "0ujtsYcgvSTl8PAuAdqWYSMnLOv"
const testProbeRunID = "0ujtsYcgvSTl8PAuAdqWYSMnLOw"

func probeFixture(t *testing.T, update bool) string {
	t.Helper()
	data, err := os.ReadFile("testdata/probe.json")
	if err != nil {
		t.Fatal(err)
	}
	if !update {
		return string(data)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	value["expected_revision"] = 4
	data, err = json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestProbeCommandsKeepScopeRevisionAndSource(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body string
		status                   int
		args                     []string
	}{
		{"list", http.MethodGet, "/probes", "", 200, []string{"list", "--limit", "7", "--offset", "2"}},
		{"view", http.MethodGet, "/probes/" + testProbeID, "", 200, []string{"view", testProbeID}},
		{"create", http.MethodPost, "/probes", probeFixture(t, false), 201, []string{"create", "-f", "-", "--environment", "development"}},
		{"update", http.MethodPut, "/probes/" + testProbeID, probeFixture(t, true), 200, []string{"update", testProbeID, "-f", "-"}},
		{"pause", http.MethodPost, "/probes/" + testProbeID + "/pause", `{"expected_revision":4}`, 200, []string{"pause", testProbeID, "--expected-revision", "4"}},
		{"resume", http.MethodPost, "/probes/" + testProbeID + "/resume", `{"expected_revision":4}`, 200, []string{"resume", testProbeID, "--expected-revision", "4"}},
		{"diagnostic", http.MethodPost, "/probes/" + testProbeID + "/run", `{"expected_revision":4}`, 202, []string{"run", testProbeID, "--expected-revision", "4"}},
		{"history", http.MethodGet, "/probes/" + testProbeID + "/runs", "", 200, []string{"history", testProbeID, "--limit", "7", "--offset", "2"}},
		{"execution list", http.MethodGet, "/probes/" + testProbeID + "/runs", "", 200, []string{"execution", "list", testProbeID}},
		{"execution view", http.MethodGet, "/probes/" + testProbeID + "/runs/" + testProbeRunID, "", 200, []string{"execution", "view", testProbeID, testProbeRunID}},
		{"delete", http.MethodDelete, "/probes/" + testProbeID, "", 204, []string{"delete", testProbeID, "--expected-revision", "4"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var received []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tc.method || r.URL.Path != tc.path {
					t.Errorf("request = %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("X-Api-Key") != "test-key" || r.Header.Get("X-Organization-Id") != "org_test" {
					t.Error("missing tenant authentication")
				}
				if (tc.name == "list" || tc.name == "history") &&
					(r.URL.Query().Get("limit") != "7" || r.URL.Query().Get("offset") != "2") {
					t.Error("lost pagination")
				}
				if tc.name == "delete" && r.URL.Query().Get("expected_revision") != "4" {
					t.Error("missing delete revision")
				}
				received, _ = io.ReadAll(r.Body)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				if tc.status != 204 {
					_, _ = io.WriteString(w, `{"id":"`+testProbeID+`","revision":4,"origin":"diagnostic","items":[]}`)
				}
			}))
			defer server.Close()
			state := makeState(t, "test-key", "", server.URL)
			state.AssumeYes = true
			stdout, _, err := execute(t, newProbeCmd(state), tc.body, tc.args...)
			if err != nil {
				t.Fatal(err)
			}
			if !json.Valid([]byte(stdout)) {
				t.Fatalf("unstructured output: %s", stdout)
			}
			if tc.body != "" {
				var want, got map[string]any
				if err := json.Unmarshal([]byte(tc.body), &want); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(received, &got); err != nil {
					t.Fatal(err)
				}
				if tc.name == "create" {
					want["config"].(map[string]any)["environment_key"] = "development"
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("body changed: %s", received)
				}
			}
		})
	}
}

func TestProbeOfflineValidationAndInputRejection(t *testing.T) {
	fixture := probeFixture(t, false)
	for _, input := range []string{
		`{}`, `null`, `[]`, fixture + `{}`,
		strings.Replace(fixture, `"interval_seconds": 60`, `"interval_seconds": 59`, 1),
		strings.Replace(fixture, `"runner_type": "cloud"`, `"runner_type": "ephemeral"`, 1),
		strings.Replace(fixture, `"config": {`, `"origin":"scheduled", "config": {`, 1),
		strings.Replace(fixture, `"config": {`, `"config": {"capabilities": [{"id":"api","checks":[]}] ,`, 1),
		strings.Replace(fixture, `"config": {`, `"config": {"checks": [{"node_id":"health","assertion_index":0}] ,`, 1),
		strings.Replace(fixture, `"flow_ids":`, `"tags":["production"],"flow_ids":`, 1),
		strings.Replace(fixture, `"flow_ids": ["550e8400-e29b-41d4-a716-446655440001", "550e8400-e29b-41d4-a716-446655440002"]`, `"flow_ids": []`, 1),
		strings.Replace(fixture, `"flow_ids":`, `"version_id":"550e8400-e29b-41d4-a716-446655440003","flow_ids":`, 1),
	} {
		_, _, err := execute(t, newProbeCmd(&AppState{}), input, "validate", "-f", "-")
		if err == nil {
			t.Fatalf("accepted invalid input: %s", input)
		}
	}
	for _, tc := range []struct {
		input string
		args  []string
	}{
		{fixture, []string{"validate", "-f", "-"}},
		{probeFixture(t, true), []string{"validate", "--update", "-f", "-"}},
		{strings.Replace(fixture, `"environment_key": "production",`, "", 1), []string{"validate", "-f", "-", "--environment", "development"}},
	} {
		out, _, err := execute(t, newProbeCmd(&AppState{}), tc.input, tc.args...)
		if err != nil || !strings.Contains(out, `"valid": true`) {
			t.Fatalf("validation: %s %v", out, err)
		}
	}
	_, _, err := execute(t, newProbeCmd(&AppState{}), strings.Repeat(" ", 1_048_577), "validate", "-f", "-")
	if err == nil || !strings.Contains(err.Error(), "exceeds 1 MiB") {
		t.Fatalf("size limit: %v", err)
	}
}

func TestProbeSelectorsAndForecastKeepCallerIntent(t *testing.T) {
	for _, tc := range []struct {
		name, path, input string
		args              []string
		want              map[string]any
	}{
		{"flow set", "/probes", "", []string{"create", "--name", "Production", "--flow-id", idFor("1").String(), "--flow-id", idFor("2").String(), "--environment", "production", "--paused"}, map[string]any{"flow_ids": []any{idFor("1").String(), idFor("2").String()}, "environment_key": "production", "enabled": false, "interval_seconds": float64(60)}},
		{"tag selector", "/probes", "", []string{"create", "--name", "Production", "--tag", "api", "--tag", "production", "--match-mode", "all", "--interval", "120"}, map[string]any{"tags": []any{"api", "production"}, "tag_match_mode": "all", "interval_seconds": float64(120)}},
		{"file selector override", "/probes", probeFixture(t, false), []string{"create", "-f", "-", "--tag", "production", "--match-mode", "all"}, map[string]any{"tags": []any{"production"}, "tag_match_mode": "all", "name": "Production API"}},
		{"forecast", "/probes/estimate", "", []string{"estimate", "--flow-id", idFor("1").String(), "--flow-id", idFor("2").String(), "--environment", "production"}, map[string]any{"flow_ids": []any{idFor("1").String(), idFor("2").String()}, "environment_key": "production", "runner_type": "cloud", "interval_seconds": float64(60)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost || r.URL.Path != tc.path ||
					r.Header.Get("X-Organization-ID") != "org_test" ||
					r.Header.Get("X-Api-Key") != "test-key" {
					t.Errorf("wrong request or scope")
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				value := body
				if tc.path == "/probes" {
					value = body["config"].(map[string]any)
				}
				for key, want := range tc.want {
					if !reflect.DeepEqual(value[key], want) {
						t.Errorf("%s = %#v, want %#v", key, value[key], want)
					}
				}
				for _, forbidden := range []string{"flow_id", "version_id", "origin", "capabilities", "checks", "depends_on"} {
					if _, ok := value[forbidden]; ok {
						t.Errorf("unexpected old source %s", forbidden)
					}
				}
				if _, hasFlows := value["flow_ids"]; hasFlows {
					if _, hasTags := value["tags"]; hasTags {
						t.Error("both source selectors sent")
					}
				}
				w.Header().Set("Content-Type", "application/json")
				if tc.path == "/probes" {
					w.WriteHeader(http.StatusCreated)
				}
				_, _ = io.WriteString(
					w,
					`{"id":"`+testProbeID+`","matched_flow_count":2,"requests_exact":false,"estimate_notes":["Dynamic requests vary"]}`,
				)
			}))
			defer server.Close()
			out, _, err := execute(t, newProbeCmd(makeState(t, "test-key", "", server.URL)), tc.input, tc.args...)
			if err != nil || !json.Valid([]byte(out)) || calls != 1 {
				t.Fatalf("calls=%d output=%s error=%v", calls, out, err)
			}
		})
	}
}

func TestProbeInvalidArgumentsMakeNoRequests(t *testing.T) {
	stub := newAPIStub(t)
	for _, args := range [][]string{
		{"pause", testProbeID}, {"resume", testProbeID, "--expected-revision", "0"}, {"run", testProbeID},
		{"delete", testProbeID}, {"list", "--limit", "101"}, {"history", testProbeID, "--offset", "-1"},
		{"view", "not-an-id"}, {"execution", "view", testProbeID, "bad-run"},
		{"source-options", "--flow-id", "name"},
		{"source-options", "--flow-id", idFor("1").String(), "--version-id", "bad-version"},
		{"estimate"}, {"estimate", "--flow-id", "name"},
		{"estimate", "--tag", "production", "--flow-id", idFor("1").String()},
		{"estimate", "--tag", "production", "--interval", "59"},
		{"estimate", "--tag", "production", "--match-mode", "invalid"},
		{"create"}, {"create", "--name", "Production"},
		{"create", "--name", "Production", "--flow-id", "name"},
		{"create", "probe.json"}, {"update", testProbeID, "probe.json"},
		{"run", testProbeID, "--expected-revision", "1", "--origin", "scheduled"},
	} {
		_, _, err := execute(t, newProbeCmd(stub.state(t)), "", args...)
		if err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	if stub.requestCount() != 0 {
		t.Fatal("invalid arguments reached API")
	}
}

func TestProbeDeleteRefusesUnattendedWithoutYes(t *testing.T) {
	stub := newAPIStub(t)
	state := stub.state(t)
	state.IsTerminal = func() bool { return false }
	_, _, err := execute(t, newProbeCmd(state), "", "delete", testProbeID, "--expected-revision", "1")
	if err == nil || !strings.Contains(err.Error(), "pass --yes") || stub.requestCount() != 0 {
		t.Fatalf("delete consent: %v, requests=%d", err, stub.requestCount())
	}
}

func TestProbeConflictAndPermissionErrorsAreNotRetried(t *testing.T) {
	for _, status := range []int{http.StatusConflict, http.StatusForbidden, http.StatusNotFound, http.StatusTooManyRequests} {
		stub := newAPIStub(
			t,
		).on(http.MethodPost, "/probes/"+testProbeID+"/run", status, map[string]any{"errors": []any{map[string]any{"code": "PROBE_REVISION_CONFLICT", "message": "Reload before running."}}})
		_, _, err := execute(t, newProbeCmd(stub.state(t)), "", "run", testProbeID, "--expected-revision", "1")
		if err == nil || !strings.Contains(err.Error(), "Reload before running") || stub.requestCount() != 1 {
			t.Fatalf("error=%v requests=%d", err, stub.requestCount())
		}
	}
}

func TestProbeYAMLUsesAPIKeys(t *testing.T) {
	stub := newAPIStub(
		t,
	).on(http.MethodGet, "/probes/"+testProbeID, http.StatusOK, api.Probe{Id: testProbeID, Revision: 4, Config: api.ProbeConfig{EnvironmentKey: "production"}})
	state := stub.state(t)
	state.OutputFormat = output.FormatYAML
	stdout, _, err := execute(t, newProbeCmd(state), "", "view", testProbeID)
	if err != nil || !strings.Contains(stdout, "environment_key: production") ||
		!strings.Contains(stdout, "revision: 4") {
		t.Fatalf("yaml=%s error=%v", stdout, err)
	}
}

func TestProbeRootExplicitOrganizationAndPluralAlias(t *testing.T) {
	stub := newAPIStub(
		t,
	).on(http.MethodGet, "/probes", http.StatusOK, map[string]any{"items": []any{}, "count": 0, "total": 0})
	isolateCLIEnvironment(t, stub.server.URL)
	stdout, stderr, code := runCLI(t, "probes", "list", "--org", "org_explicit", "-o", "json")
	requests := stub.requestsTo(http.MethodGet, "/probes")
	if code != 0 || !json.Valid([]byte(stdout)) || len(requests) != 1 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if requests[0].header.Get("X-Organization-Id") != "org_explicit" {
		t.Fatal("explicit organization ignored")
	}
}

func TestProbeAndRunCompletionUsesSavedIdentity(t *testing.T) {
	stub := newAPIStub(t).
		on(http.MethodGet, "/probes", http.StatusOK, api.ProbeListResponse{Items: []api.Probe{{Id: testProbeID, Config: api.ProbeConfig{Name: "Production API"}}}}).
		on(http.MethodGet, "/probes/"+testProbeID+"/runs", http.StatusOK, api.ProbeRunListResponse{Items: []api.ProbeRun{{Id: testProbeRunID, Origin: api.ProbeDiagnostic, Status: api.ProbeCompleted, DueAt: time.Now()}}})
	state := stub.state(t)
	cmd := newProbeCmd(state)
	probes, _ := completeProbeArgs(state)(cmd, nil, testProbeID[:4])
	if len(probes) != 1 || !strings.Contains(probes[0], testProbeID+"\tProduction API") {
		t.Fatalf("probe completion = %v", probes)
	}
	runs, _ := completeProbeExecutionArgs(state)(cmd, []string{testProbeID}, testProbeRunID[:4])
	if len(runs) != 1 || !strings.Contains(runs[0], testProbeRunID+"\tdiagnostic · completed") {
		t.Fatalf("run completion = %v", runs)
	}
	for _, request := range stub.requests {
		if request.header.Get("X-Organization-Id") != "org_test" {
			t.Fatal("completion lost tenant scope")
		}
	}
}
