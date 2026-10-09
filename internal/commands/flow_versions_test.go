package commands

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestFlowPublishedVersionCommands(t *testing.T) {
	flow, version := idFor("21").String(), idFor("22").String()
	for _, tc := range []struct {
		name, method, path string
		status             int
		args               []string
	}{
		{"publish", http.MethodPost, "/flows/" + flow + "/publish", http.StatusCreated, []string{"publish", flow}},
		{"list", http.MethodGet, "/flows/" + flow + "/versions", http.StatusOK, []string{"version", "list", flow, "--limit", "7", "--offset", "2"}},
		{"view", http.MethodGet, "/flows/" + flow + "/versions/" + version, http.StatusOK, []string{"version", "view", flow, version}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := newAPIStub(t).on(tc.method, tc.path, tc.status, map[string]any{"id": version, "items": []any{}})
			out, _, err := execute(t, newFlowCmd(stub.state(t)), "", tc.args...)
			if err != nil {
				t.Fatal(err)
			}
			requests := stub.requestsTo(tc.method, tc.path)
			if len(requests) != 1 {
				t.Fatalf("requests = %d", len(requests))
			}
			if requests[0].header.Get("X-Api-Key") != "test-key" ||
				requests[0].header.Get("X-Organization-Id") != "org_test" {
				t.Fatal("tenant credentials missing")
			}
			if tc.name == "list" && !strings.Contains(requests[0].query, "limit=7") {
				t.Fatalf("query = %s", requests[0].query)
			}
			if !json.Valid([]byte(out)) {
				t.Fatalf("output = %s", out)
			}
		})
	}
}

func TestFlowVersionsRejectBadArgumentsWithoutRequests(t *testing.T) {
	stub := newAPIStub(t)
	for _, args := range [][]string{
		{"publish", "not-an-id"},
		{"version", "view", idFor("21").String(), "not-a-version"},
		{"version", "list", idFor("21").String(), "--limit", "101"},
		{"version", "list", idFor("21").String(), "--offset", "-1"},
	} {
		_, _, err := execute(t, newFlowCmd(stub.state(t)), "", args...)
		if err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	if stub.requestCount() != 0 {
		t.Fatal("invalid input reached API")
	}
}
