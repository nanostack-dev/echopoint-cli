package commands

import (
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/output"
)

var (
	testMonitorID    = idFor("100")
	testMonitorRunID = idFor("200")
	monitorBase      = time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
)

func sampleMonitor(id uuid.UUID, name string, healthy bool) api.Monitor {
	monitor := api.Monitor{
		Id: id, Name: name, Enabled: true, RunnerType: api.Cloud, EnvironmentKey: "production",
		FlowIds: []uuid.UUID{}, Tags: []string{"checkout"}, TagMatchMode: api.Any,
		RecipientEmails: []string{}, IgnoreModules: true, Revision: 4, TimeoutSeconds: 30,
		StartsAt: monitorBase, CreatedAt: monitorBase, UpdatedAt: monitorBase, NextRunAt: &monitorBase,
	}
	if healthy {
		monitor.IntervalSeconds = 300
		monitor.HealthPolicy = &api.MonitorHealthPolicy{ConfirmationRuns: 2, RecoveryRuns: 2, FreshnessSeconds: 900}
		monitor.Health = &api.MonitorHealth{State: api.MonitorHealthStateOperational, Reason: "all flows passed"}
	} else {
		cron := "0 9 * * *"
		monitor.CronExpression = &cron
	}
	return monitor
}

func sampleMonitorRun(id uuid.UUID, origin api.MonitorRunOrigin) api.MonitorRun {
	return api.MonitorRun{
		Id:               id,
		ScheduleId:       testMonitorID,
		Origin:           origin,
		Revision:         4,
		DueAt:            monitorBase,
		MatchedFlowCount: 2,
		Status:           api.ScheduleOccurrenceStatusSucceeded,
		LaunchFailures:   []api.ScheduleLaunchFailure{},
		CreatedAt:        monitorBase,
		UpdatedAt:        monitorBase,
		Observation:      &api.MonitorObservation{State: api.MonitorHealthStateOperational, Reason: "ok"},
	}
}

func monitorStub(t *testing.T) *apiStub {
	t.Helper()
	monitorPath := "/monitors/" + testMonitorID.String()
	healthy, cron := sampleMonitor(
		testMonitorID,
		"Checkout health",
		true,
	), sampleMonitor(
		idFor("101"),
		"Nightly",
		false,
	)
	paused := healthy
	paused.Enabled = false
	run := sampleMonitorRun(testMonitorRunID, api.MonitorRunOrigin("scheduled"))
	diagnostic := sampleMonitorRun(idFor("201"), api.MonitorRunOrigin("diagnostic"))
	message := "boom"
	return newAPIStub(t).
		onSearch("/monitors/search", healthy, cron).
		on(http.MethodPost, "/monitors", http.StatusCreated, healthy).
		on(http.MethodGet, monitorPath, http.StatusOK, healthy).
		on(http.MethodPut, monitorPath, http.StatusOK, healthy).
		on(http.MethodDelete, monitorPath, http.StatusNoContent, nil).
		on(http.MethodPost, monitorPath+"/pause", http.StatusOK, paused).
		on(http.MethodPost, monitorPath+"/resume", http.StatusOK, healthy).
		on(http.MethodPost, monitorPath+"/run", http.StatusAccepted, diagnostic).
		onSearch(monitorPath+"/runs/search", run, diagnostic).
		on(http.MethodGet, monitorPath+"/runs/"+testMonitorRunID.String(), http.StatusOK, api.MonitorRunDetail{
			Run: run,
			Executions: []api.ScheduleRunExecution{{
				Id: idFor("300"), FlowId: idFor("1"), Status: "failed", StartedAt: monitorBase, ErrorMessage: &message,
			}},
		}).
		on(http.MethodPost, "/monitors/estimate", http.StatusOK, api.MonitorEstimate{
			MatchedFlowCount: 4, ExecutionsPerOccurrence: 4, ExecutionsPerDay: 1152, ExecutionsPer30Days: 34560,
			EstimateNotes: []string{"Your Cloud monitors would plan 1,194 of 1,000 Cloud executions per day."},
			Flows:         []api.MonitorEstimateFlow{}, GeneratedAt: monitorBase,
			DailyCloudExecutionLimit: new(int64(1000)), OtherCloudExecutionsPerDay: new(int64(42)),
		}).
		on(http.MethodPost, "/monitors/preview-tags", http.StatusOK, api.ScheduleTagPreviewResponse{
			MatchedFlowCount: 1,
			Flows:            []api.FlowSearchItem{{Id: idFor("1"), Name: "Checkout"}},
		})
}

func monitorFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func jsonBody(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("body %q: %v", data, err)
	}
	return body
}

func TestEachMonitorCommandSendsItsRequestUnderTheSelectedOrganization(t *testing.T) {
	id, runID := testMonitorID.String(), testMonitorRunID.String()
	created := monitorFixture(t, "monitor.json")
	for _, tc := range []struct {
		name, method, path, stdin string
		args                      []string
		query                     map[string]string
	}{
		{
			"list", http.MethodPost, "/monitors/search", "",
			[]string{"list", "--limit", "7", "--offset", "2", "--enabled", "--has-health-policy=false"}, nil,
		},
		{"view", http.MethodGet, "/monitors/" + id, "", []string{"view", id}, nil},
		{"get alias", http.MethodGet, "/monitors/" + id, "", []string{"get", id}, nil},
		{"create", http.MethodPost, "/monitors", created, []string{"create", "-f", "-"}, nil},
		{"update", http.MethodPut, "/monitors/" + id, created, []string{"update", id, "-f", "-"}, nil},
		{"delete", http.MethodDelete, "/monitors/" + id, "", []string{"delete", id}, nil},
		{"pause", http.MethodPost, "/monitors/" + id + "/pause", "", []string{"pause", id}, nil},
		{"resume", http.MethodPost, "/monitors/" + id + "/resume", "", []string{"resume", id}, nil},
		{"run", http.MethodPost, "/monitors/" + id + "/run", "", []string{"run", id}, nil},
		{
			"runs", http.MethodPost, "/monitors/" + id + "/runs/search", "",
			[]string{"runs", id, "--limit", "5", "--offset", "1"}, nil,
		},
		{"run-view", http.MethodGet, "/monitors/" + id + "/runs/" + runID, "", []string{"run-view", id, runID}, nil},
		{
			"estimate", http.MethodPost, "/monitors/estimate",
			`{"tags":["checkout"],"runner_type":"cloud","interval_seconds":300,"health_policy":true}`,
			[]string{"estimate", "-f", "-"}, nil,
		},
		{
			"preview-tags", http.MethodPost, "/monitors/preview-tags", "",
			[]string{"preview-tags", "--tag", "checkout", "--tag", "smoke", "--match-mode", "all", "--ignore-modules=false"}, nil,
		},
	} {
		for _, format := range []output.Format{output.FormatTable, output.FormatJSON, output.FormatYAML} {
			t.Run(tc.name+" "+string(format), func(t *testing.T) {
				stub := monitorStub(t)
				state := stub.state(t)
				state.OutputFormat = format
				state.AssumeYes = true

				stdout, _, err := execute(t, newMonitorCmd(state), tc.stdin, tc.args...)

				if err != nil {
					t.Fatal(err)
				}
				requests := stub.requestsTo(tc.method, tc.path)
				if len(requests) != 1 || stub.requestCount() != 1 {
					t.Fatalf("requests = %+v", stub.requests)
				}
				request := requests[0]
				if request.header.Get("X-Organization-Id") != "org_test" ||
					request.header.Get("X-Api-Key") != "test-key" {
					t.Errorf("tenant headers = %v", request.header)
				}
				for key, want := range tc.query {
					if !strings.Contains("&"+request.query+"&", "&"+key+"="+want+"&") {
						t.Errorf("query %q lacks %s=%s", request.query, key, want)
					}
				}
				if tc.name == "preview-tags" {
					got := jsonBody(t, request.body)
					want := map[string]any{
						"tags":           []any{"checkout", "smoke"},
						"tag_match_mode": "all",
						"ignore_modules": false,
					}
					if !reflect.DeepEqual(got, want) {
						t.Errorf("preview body = %v", got)
					}
				}
				if tc.stdin != "" && !reflect.DeepEqual(jsonBody(t, request.body), jsonBody(t, []byte(tc.stdin))) {
					t.Errorf("body changed: %s", request.body)
				}
				switch format {
				case output.FormatJSON:
					if !json.Valid([]byte(stdout)) {
						t.Errorf("not JSON: %s", stdout)
					}
				case output.FormatYAML:
					if strings.HasPrefix(strings.TrimSpace(stdout), "{") || stdout == "" {
						t.Errorf("not YAML: %s", stdout)
					}
				case output.FormatTable:
					if strings.TrimSpace(stdout) == "" || json.Valid([]byte(stdout)) {
						t.Errorf("not a table summary: %q", stdout)
					}
				}
			})
		}
	}
}

func TestMonitorListAndViewTablesShowCadenceAndHealth(t *testing.T) {
	stub := monitorStub(t)
	list, _, err := execute(t, newMonitorCmd(stub.tableState(t)), "", "list")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Total: 2", testMonitorID.String(), "Checkout health", "every 300s", "operational", "0 9 * * *", "enabled"} {
		if !strings.Contains(list, want) {
			t.Errorf("list lacks %q:\n%s", want, list)
		}
	}
	view, _, err := execute(t, newMonitorCmd(stub.tableState(t)), "", "view", testMonitorID.String())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Revision: 4", "tags checkout (any)", "confirm 2, recover 2, fresh 900s", "Health: operational"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
	runView, _, err := execute(
		t,
		newMonitorCmd(stub.tableState(t)),
		"",
		"run-view",
		testMonitorID.String(),
		testMonitorRunID.String(),
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Origin: scheduled", "Observation: operational: ok", idFor("300").String(), "boom"} {
		if !strings.Contains(runView, want) {
			t.Errorf("run-view lacks %q:\n%s", want, runView)
		}
	}
}

func TestMonitorListSendsNoFilterUnlessAskedTo(t *testing.T) {
	stub := monitorStub(t)
	if _, _, err := execute(t, newMonitorCmd(stub.state(t)), "", "list"); err != nil {
		t.Fatal(err)
	}
	body := bodyOf(t, stub.requestsTo(http.MethodPost, "/monitors/search")[0])
	if _, filtered := body["filter"]; filtered {
		t.Errorf("body = %v", body)
	}
}

func TestMonitorListAndRunsSearchWithTheFlagsAsTheRequestBody(t *testing.T) {
	id := testMonitorID.String()
	for name, tc := range map[string]struct {
		path string
		args []string
		want map[string]any
	}{
		"list": {
			"/monitors/search",
			[]string{"list", "--limit", "7", "--offset", "2", "--enabled", "--has-health-policy=false"},
			map[string]any{
				"pagination": map[string]any{"limit": float64(7), "offset": float64(2)},
				"filter":     map[string]any{"enabled": true, "has_health_policy": false},
			},
		},
		"list from a cursor": {
			"/monitors/search",
			[]string{"list", "--cursor", "abc"},
			map[string]any{"pagination": map[string]any{"limit": float64(20), "cursor": "abc"}},
		},
		"runs": {
			"/monitors/" + id + "/runs/search",
			[]string{"runs", id, "--limit", "5", "--cursor", "abc"},
			map[string]any{"pagination": map[string]any{"limit": float64(5), "cursor": "abc"}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			stub := monitorStub(t)

			if _, _, err := execute(t, newMonitorCmd(stub.state(t)), "", tc.args...); err != nil {
				t.Fatal(err)
			}

			if got := bodyOf(t, stub.requestsTo(http.MethodPost, tc.path)[0]); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("body = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMonitorCreateAndUpdateReadYAMLAndJSONFiles(t *testing.T) {
	stub := monitorStub(t)
	state := stub.state(t)

	if _, _, err := execute(t, newMonitorCmd(state), "", "create", "-f", "testdata/monitor.yaml"); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"name": "Nightly regression", "runner_type": "self_hosted", "cron_expression": "0 3 * * *",
		"starts_at": "2026-10-15T03:00:00Z", "enabled": true,
		"flow_ids": []any{"550e8400-e29b-41d4-a716-446655440001", "550e8400-e29b-41d4-a716-446655440002"},
	}
	if got := jsonBody(t, stub.requestsTo(http.MethodPost, "/monitors")[0].body); !reflect.DeepEqual(got, want) {
		t.Errorf("yaml create body = %v, want %v", got, want)
	}

	if _, _, err := execute(
		t,
		newMonitorCmd(state),
		"",
		"update",
		testMonitorID.String(),
		"-f",
		"testdata/monitor.json",
	); err != nil {
		t.Fatal(err)
	}
	sent := jsonBody(t, stub.requestsTo(http.MethodPut, "/monitors/"+testMonitorID.String())[0].body)
	if sent["interval_seconds"] != float64(300) || sent["health_policy"] == nil {
		t.Errorf("json update body = %v", sent)
	}
}

func TestMonitorWritesRefuseBadInputWithoutARequest(t *testing.T) {
	valid := monitorFixture(t, "monitor.json")
	id := testMonitorID.String()
	for name, tc := range map[string]struct {
		args  []string
		stdin string
		want  string
	}{
		"unknown field":        {[]string{"create", "-f", "-"}, strings.Replace(valid, `"name"`, `"capabilities": [], "name"`, 1), "capabilities"},
		"interval too short":   {[]string{"create", "-f", "-"}, strings.Replace(valid, `"interval_seconds": 300`, `"interval_seconds": 59`, 1), "CreateMonitorRequest"},
		"no runner":            {[]string{"create", "-f", "-"}, strings.Replace(valid, `"runner_type": "cloud",`, "", 1), "runner_type"},
		"not an object":        {[]string{"create", "-f", "-"}, `[]`, "must be an object"},
		"not json or yaml":     {[]string{"create", "-f", "-"}, "name: [", "invalid monitor input"},
		"update as create":     {[]string{"update", id, "-f", "-"}, `{"enabled": true}`, "UpdateMonitorRequest"},
		"positional file":      {[]string{"create", "monitor.json"}, "", "pass the file with -f"},
		"file typed on update": {[]string{"update", id, "monitor.json"}, "", "pass the file with -f"},
		"missing -f":           {[]string{"create"}, "", `required flag(s) "file" not set`},
		"empty -f":             {[]string{"create", "-f", ""}, "", "-f needs a file path"},
		"bad id":               {[]string{"update", "checkout", "-f", "-"}, valid, `"checkout" is not a monitor id; list the monitors with: echopoint monitor list`},
		"estimate no runner":   {[]string{"estimate", "-f", "-"}, `{"tags":["a"]}`, "runner_type"},
	} {
		t.Run(name, func(t *testing.T) {
			stub := monitorStub(t)

			_, _, err := execute(t, newMonitorCmd(stub.state(t)), tc.stdin, tc.args...)

			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
			if stub.requestCount() != 0 {
				t.Errorf("sent %d requests", stub.requestCount())
			}
		})
	}
}

func TestMonitorArgumentsMustBeIDs(t *testing.T) {
	id := testMonitorID.String()
	for _, args := range [][]string{
		{"view", "nope"}, {"delete", "nope"}, {"pause", "nope"}, {"resume", "nope"}, {"run", "nope"},
		{"runs", "nope"}, {"run-view", "nope", testMonitorRunID.String()},
		{"run-view", id, "nope"},
	} {
		stub := monitorStub(t)
		_, _, err := execute(t, newMonitorCmd(stub.state(t)), "", args...)
		if err == nil || !strings.Contains(err.Error(), "is not a ") || stub.requestCount() != 0 {
			t.Errorf("%v: err = %v, %d requests", args, err, stub.requestCount())
		}
	}
	for _, args := range [][]string{{"list", "--limit", "0"}, {"list", "--limit", "101"}, {"list", "--offset", "-1"}, {"runs", id, "--limit", "0"}} {
		stub := monitorStub(t)
		if _, _, err := execute(t, newMonitorCmd(stub.state(t)), "", args...); err == nil || stub.requestCount() != 0 {
			t.Errorf("%v: err = %v, %d requests", args, err, stub.requestCount())
		}
	}
}

func TestMonitorDeleteAsksFirstLikeEveryWholeResourceDelete(t *testing.T) {
	id := testMonitorID.String()
	path := "/monitors/" + id

	t.Run("no terminal refuses", func(t *testing.T) {
		stub := monitorStub(t)
		state := stub.state(t)
		state.IsTerminal = func() bool { return false }
		_, _, err := execute(t, newMonitorCmd(state), "", "delete", id)
		if err == nil || err.Error() != "pass --yes to delete monitor "+id+" without a prompt" {
			t.Errorf("err = %v", err)
		}
		if len(stub.requestsTo(http.MethodDelete, path)) != 0 {
			t.Error("deleted without asking")
		}
	})
	t.Run("terminal decline cancels", func(t *testing.T) {
		stub := monitorStub(t)
		state := stub.state(t)
		state.IsTerminal = func() bool { return true }
		_, stderr, err := execute(t, newMonitorCmd(state), "n\n", "delete", id)
		if exitCodeFrom(err) != exitCancelled || !strings.Contains(stderr, "Delete monitor "+id+"? [y/N]") {
			t.Errorf("err = %v, prompt %q", err, stderr)
		}
		if len(stub.requestsTo(http.MethodDelete, path)) != 0 {
			t.Error("deleted after a no")
		}
	})
	t.Run("terminal yes deletes", func(t *testing.T) {
		stub := monitorStub(t)
		state := stub.state(t)
		state.IsTerminal = func() bool { return true }
		if _, _, err := execute(t, newMonitorCmd(state), "y\n", "delete", id); err != nil {
			t.Fatal(err)
		}
		if len(stub.requestsTo(http.MethodDelete, path)) != 1 {
			t.Error("not deleted")
		}
	})
	t.Run("json result", func(t *testing.T) {
		stub := monitorStub(t)
		state := stub.state(t)
		state.OutputFormat = output.FormatJSON
		state.AssumeYes = true
		stdout, _, err := execute(t, newMonitorCmd(state), "", "delete", id)
		got := jsonBody(t, []byte(stdout))
		if err != nil || got["id"] != id || got["deleted"] != true {
			t.Errorf("stdout %q, err %v", stdout, err)
		}
	})
}

func TestMonitorAPIErrorsAreReported(t *testing.T) {
	stub := newAPIStub(
		t,
	).on(http.MethodPost, "/monitors/"+testMonitorID.String()+"/run", http.StatusUnprocessableEntity,
		map[string]any{
			"errors": []any{
				map[string]any{
					"code":    "VALIDATION",
					"message": "only a monitor with a health policy has diagnostic runs",
				},
			},
		})

	_, _, err := execute(t, newMonitorCmd(stub.state(t)), "", "run", testMonitorID.String())

	if err == nil || !strings.Contains(err.Error(), "api error (422): only a monitor with a health policy") {
		t.Errorf("err = %v", err)
	}
}

func TestMonitorHasNoCloudBudgetOrProbeCommand(t *testing.T) {
	for _, args := range [][]string{
		{"monitor", "cloud-budget"}, {"monitor", "budget"}, {"probe", "list"}, {"probes", "list"},
		{"status-page", "binding-options"},
	} {
		stub := newAPIStub(t)
		isolateCLIEnvironment(t, stub.server.URL)
		_, stderr, code := runCLI(t, args...)
		if code == 0 || !strings.Contains(stderr, "unknown command") {
			t.Errorf("%v: exit %d, stderr %q", args, code, stderr)
		}
		if stub.requestCount() != 0 {
			t.Errorf("%v sent %d requests", args, stub.requestCount())
		}
	}
	walkCommands(NewRootCmd(), func(path string, _ *cobra.Command) {
		if strings.Contains(path, "budget") || strings.Contains(path, "probe") {
			t.Errorf("%s is a command", path)
		}
	})
}

func TestMonitorRootHelpAndPluralAlias(t *testing.T) {
	stdout, _, err := runRoot(t, "monitors", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, leaf := range []string{"list", "view", "create", "update", "delete", "pause", "resume", "run", "runs", "run-view", "estimate", "preview-tags"} {
		if !strings.Contains(stdout, "\n  "+leaf+" ") {
			t.Errorf("help does not list %s:\n%s", leaf, stdout)
		}
	}
	root := NewRootCmd()
	monitor := find(t, root, "monitor")
	if !slices.Contains(monitor.Aliases, "monitors") || monitor.GroupID != resourcesGroupID {
		t.Errorf("aliases %v, group %q", monitor.Aliases, monitor.GroupID)
	}
}

func TestMonitorIDsAndRunsComplete(t *testing.T) {
	stub := monitorStub(t)
	isolateCLIEnvironment(t, stub.server.URL)
	id := testMonitorID.String()

	wantMonitors := []string{testMonitorID.String() + "\tCheckout health", idFor("101").String() + "\tNightly"}
	for _, args := range [][]string{
		{"monitor", "view", ""}, {"monitors", "view", ""}, {"monitor", "update", ""}, {"monitor", "delete", ""},
		{"monitor", "pause", ""}, {"monitor", "resume", ""}, {"monitor", "run", ""}, {"monitor", "runs", ""},
		{"monitor", "run-view", ""},
	} {
		if got := completeCLI(t, args...); !slices.Equal(got, wantMonitors) {
			t.Errorf("%v = %q, want %q", args, got, wantMonitors)
		}
	}
	wantRuns := []string{
		testMonitorRunID.String() + "\tscheduled · succeeded · 2026-10-15 08:00",
		idFor("201").String() + "\tdiagnostic · succeeded · 2026-10-15 08:00",
	}
	if got := completeCLI(t, "monitor", "run-view", id, ""); !slices.Equal(got, wantRuns) {
		t.Errorf("runs = %q, want %q", got, wantRuns)
	}
	for _, args := range [][]string{{"monitor", "view", id, ""}, {"monitor", "run-view", id, testMonitorRunID.String(), ""}} {
		if got := completeCLI(t, args...); len(got) != 0 {
			t.Errorf("%v = %q, want nothing", args, got)
		}
	}
	if got := completeCLI(
		t,
		"monitor",
		"preview-tags",
		"--match-mode",
		"",
	); !slices.Equal(
		got,
		[]string{"any", "all"},
	) {
		t.Errorf("match-mode = %q", got)
	}
}
