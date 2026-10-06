package commands

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"echopoint-cli/internal/api"
)

// Every -o yaml prints the key names of the API's JSON, never the lowercased Go
// field names a YAML encoder would make of the generated types.
func TestEveryYAMLOutputUsesTheJSONKeyNames(t *testing.T) {
	folder, parent := idFor("11"), idFor("10")
	started := time.Date(2026, 10, 5, 14, 30, 0, 0, time.UTC)
	stub := newAPIStub(t).onFlows(sampleFlow(idFor("1"), "Checkout smoke", &folder))
	stub.on(
		http.MethodGet,
		"/flows/"+idFor("1").String(),
		http.StatusOK,
		sampleFlow(idFor("1"), "Checkout smoke", &folder),
	)
	stub.on(http.MethodGet, "/flows/folders", http.StatusOK, api.FlowFolderListResponse{
		Count: 1, Total: 1, Items: []api.FlowFolder{sampleFolder(folder, "Identity", &parent)},
	})
	stub.on(http.MethodGet, "/collections", http.StatusOK, api.CollectionListResponse{
		Count: 1,
		Total: 1,
		Items: []api.Collection{{Id: idFor("30"), Name: "Payments", CreatedAt: started, UpdatedAt: started}},
	})
	stub.on(http.MethodGet, "/flows/"+idFor("1").String()+"/executions", http.StatusOK, api.FlowExecutionListResponse{
		Count: 1,
		Total: 1,
		Items: []api.FlowExecution{{Id: idFor("20"), FlowId: idFor("1"), Status: "completed", StartedAt: started}},
	})
	stub.on(http.MethodPost, "/flows/"+idFor("1").String()+"/launch", http.StatusAccepted, launchResponse(false))
	stub.on(http.MethodGet, "/organization/environments", http.StatusOK, api.EnvironmentListResponse{
		Items: []api.Environment{{Id: idFor("40"), Name: "dev"}},
	})
	isolateCLIEnvironment(t, stub.server.URL)
	flow := idFor("1").String()

	for _, tc := range []struct {
		args    []string
		present []string
		absent  []string
	}{
		{[]string{"flow", "list"}, []string{"name: Checkout smoke", "updated_at:", "flow_definition:", "folder_id:", "organization_id:"},
			[]string{"updatedat:", "flowdefinition:", "folderid:", "organizationid:"}},
		{[]string{"flow", "view", flow}, []string{"flow_definition:", "created_at:", "organization_id:"},
			[]string{"createdat:", "flowdefinition:", "organizationid:"}},
		{[]string{"flow", "folder", "list"}, []string{"parent_id:", "created_at:"}, []string{"parentid:", "createdat:"}},
		{[]string{"collection", "list"}, []string{"created_at:", "request_count:"}, []string{"createdat:", "requestcount:"}},
		{[]string{"flow", "execution", "list", flow}, []string{"started_at:", "flow_snapshot:"}, []string{"startedat:", "flowsnapshot:"}},
		{[]string{"flow", "launch", flow}, []string{"execution:", "started_at:", "runner_type:"},
			[]string{"startedat:", "runnertype:"}},
		{[]string{"config", "view"}, []string{"base_url:", "output_format:", "frontend_url:", "timeout: 30s"},
			[]string{"baseurl:", "outputformat:", "frontendurl:"}},
		{[]string{"org", "env", "environments", "list"}, []string{"- dev"}, nil},
	} {
		stdout, stderr, code := runCLI(t, append(tc.args, "-o", "yaml")...)

		if code != 0 {
			t.Errorf("%v: exit %d, stderr %q", tc.args, code, stderr)
			continue
		}
		for _, key := range tc.present {
			if !strings.Contains(stdout, key) {
				t.Errorf("%v: no %q in\n%s", tc.args, key, stdout)
			}
		}
		for _, key := range tc.absent {
			if strings.Contains(stdout, key) {
				t.Errorf("%v: the Go field name %q leaked into\n%s", tc.args, key, stdout)
			}
		}
	}
}

func TestConfigViewPrintsTheSameKeysInJSONAndYAML(t *testing.T) {
	stub := newAPIStub(t)
	isolateCLIEnvironment(t, stub.server.URL)

	asJSON, _, jsonCode := runCLI(t, "config", "view", "-o", "json")

	if jsonCode != 0 {
		t.Fatalf("exit %d", jsonCode)
	}
	for _, key := range []string{`"profile"`, `"api"`, `"base_url"`, `"timeout": "30s"`, `"frontend_url"`, `"defaults"`, `"output_format"`} {
		if !strings.Contains(asJSON, key) {
			t.Errorf("no %s in\n%s", key, asJSON)
		}
	}
}
