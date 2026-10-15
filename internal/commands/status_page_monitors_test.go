package commands

import (
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/output"
)

func statusPageWithMonitorBindings(t *testing.T) (api.SaveStatusPageRequest, []api.StatusPageMonitorBinding) {
	t.Helper()
	fixture, err := os.ReadFile("testdata/status-page.json")
	if err != nil {
		t.Fatal(err)
	}
	var request api.SaveStatusPageRequest
	if err := json.Unmarshal(fixture, &request); err != nil {
		t.Fatal(err)
	}
	bindings := []api.StatusPageMonitorBinding{{ServiceId: "api", MonitorId: testMonitorID}}
	request.MonitorBindings = &bindings
	operational, outage, unknown := "Checkout is available", "Checkout is unavailable", "We are checking checkout"
	request.Config.Services[0].Messages = &api.StatusPageServiceMessages{
		Operational: &operational, Outage: &outage, Unknown: &unknown,
	}
	return request, bindings
}

func TestStatusPageMonitorBindingsValidateSaveAndReadBack(t *testing.T) {
	request, bindings := statusPageWithMonitorBindings(t)
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := execute(t, newStatusPageCmd(&AppState{}), string(data), "validate", "-f", "-"); err != nil {
		t.Fatal(err)
	}

	stub := newAPIStub(t)
	stub.on(http.MethodPut, "/status-pages/current", http.StatusOK, api.StatusPageEditor{
		MonitorBindings: &bindings, DraftVersion: 1, Slug: "example-a1b2c3d4e5f6", Config: request.Config,
	})
	stub.on(http.MethodGet, "/status-pages/current", http.StatusOK, api.StatusPageEditor{
		MonitorBindings: &bindings, DraftVersion: 1, Slug: "example-a1b2c3d4e5f6", Config: request.Config,
	})
	state := stub.state(t)
	state.OutputFormat = output.FormatJSON
	if _, _, err := execute(t, newStatusPageCmd(state), string(data), "save", "-f", "-"); err != nil {
		t.Fatal(err)
	}
	var saved api.SaveStatusPageRequest
	sent := stub.requestsTo(http.MethodPut, "/status-pages/current")
	if len(sent) != 1 || json.Unmarshal(sent[0].body, &saved) != nil {
		t.Fatalf("save requests = %+v", sent)
	}
	if !reflect.DeepEqual(saved.MonitorBindings, request.MonitorBindings) {
		t.Errorf("monitor bindings changed on save: %s", sent[0].body)
	}
	if strings.Contains(string(sent[0].body), "probe") || strings.Contains(string(sent[0].body), `"binding"`) {
		t.Errorf("save body still carries a probe or assertion binding: %s", sent[0].body)
	}
	if !reflect.DeepEqual(saved.Config.Services[0].Messages, request.Config.Services[0].Messages) {
		t.Error("service public messages dropped on save")
	}

	out, _, err := execute(t, newStatusPageCmd(state), "", "view")
	if err != nil {
		t.Fatal(err)
	}
	var readBack api.StatusPageEditor
	if err := json.Unmarshal([]byte(out), &readBack); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(readBack.MonitorBindings, &bindings) {
		t.Fatalf("bindings = %+v", readBack.MonitorBindings)
	}
}

func TestStatusPageValidationRejectsTheRemovedProbeAndAssertionBindings(t *testing.T) {
	request, _ := statusPageWithMonitorBindings(t)
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	id := testMonitorID.String()
	for name, bad := range map[string]string{
		"empty monitor":       strings.Replace(string(data), `"monitor_id":"`+id+`"`, `"monitor_id":""`, 1),
		"not a uuid":          strings.Replace(string(data), `"monitor_id":"`+id+`"`, `"monitor_id":"api"`, 1),
		"probe binding":       strings.Replace(string(data), `"monitor_bindings":[{"monitor_id":"`+id+`"`, `"probe_bindings":[{"probe_id":"`+id+`"`, 1),
		"revision":            strings.Replace(string(data), `"monitor_id":"`+id+`"`, `"monitor_id":"`+id+`","revision":1`, 1),
		"assertion binding":   strings.Replace(string(data), `"monitor_bindings"`, `"binding":{"service_id":"api","schedule_id":"`+id+`"},"monitor_bindings"`, 1),
		"message too long":    strings.Replace(string(data), `"operational":"Checkout is available"`, `"operational":"`+strings.Repeat("a", 241)+`"`, 1),
		"unknown message key": strings.Replace(string(data), `"messages":{`, `"messages":{"error":"private assertion",`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if bad == string(data) {
				t.Fatal("the mutation changed nothing")
			}
			if _, _, err := execute(t, newStatusPageCmd(&AppState{}), bad, "validate", "-f", "-"); err == nil {
				t.Errorf("accepted: %s", bad)
			}
		})
	}
}

func TestStatusPageHasNoBindingOptionsCommand(t *testing.T) {
	stub := newAPIStub(t)

	_, _, err := execute(
		t,
		newStatusPageCmd(stub.state(t)),
		"",
		"binding-options",
		"--schedule-id",
		idFor("5").String(),
	)

	if err == nil ||
		!strings.Contains(err.Error(), `unknown command "binding-options"`) &&
			!strings.Contains(err.Error(), "binding-options") {
		t.Errorf("err = %v", err)
	}
	if stub.requestCount() != 0 {
		t.Errorf("sent %d requests", stub.requestCount())
	}
}
