package commands

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"echopoint-cli/internal/api"
)

func TestFlowCreateWithNameMakesAnEmptyFlow(t *testing.T) {
	stub := newAPIStub(t)
	stub.on(http.MethodPost, "/flows", http.StatusCreated, sampleFlow(idFor("1"), "Checkout", nil))

	stdout, _, err := execute(t, newFlowCmd(stub.tableState(t)), "", "create", "--name", "Checkout")

	if err != nil {
		t.Fatal(err)
	}
	sent := bodyOf(t, stub.requestsTo(http.MethodPost, "/flows")[0])
	definition, _ := sent["flow_definition"].(map[string]any)
	if sent["name"] != "Checkout" || len(definition["nodes"].([]any)) != 0 || len(definition["edges"].([]any)) != 0 {
		t.Errorf("sent %v", sent)
	}
	if stdout != "ID: "+idFor("1").String()+"\nName: Checkout\n" {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestFlowCreateTakesExactlyOneOfNameAndFile(t *testing.T) {
	file := writeTemp(t, "flow.json", `{"name":"x","flow_definition":{"nodes":[],"edges":[]}}`)
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"neither":    {[]string{"create"}, "pass --name for an empty flow, or -f for a flow from a JSON file"},
		"both":       {[]string{"create", "--name", "x", "-f", file}, "pass either --name or -f, not both"},
		"empty name": {[]string{"create", "--name", " "}, "--name must not be empty"},
	} {
		t.Run(name, func(t *testing.T) {
			stub := newAPIStub(t)

			_, _, err := execute(t, newFlowCmd(stub.state(t)), "", tc.args...)

			if err == nil || err.Error() != tc.want || stub.requestCount() != 0 {
				t.Errorf("err = %v, %d requests", err, stub.requestCount())
			}
		})
	}
}

func TestFlowViewShowsTheSummaryAndWithAnOutputFormatTheWholeFlow(t *testing.T) {
	folder := idFor("10")
	flow := sampleFlow(idFor("1"), "Checkout", &folder)
	stub := newAPIStub(t)
	stub.on(http.MethodGet, "/flows/"+idFor("1").String(), http.StatusOK, flow)

	summary, _, err := execute(t, newFlowCmd(stub.tableState(t)), "", "view", idFor("1").String())
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"Name: Checkout", "ID: " + idFor("1").String(), "Version: 1.0", "  Nodes: 0", "  Edges: 0"} {
		if !strings.Contains(summary, line+"\n") {
			t.Errorf("the summary lacks %q:\n%s", line, summary)
		}
	}

	jsonState := stub.state(t)
	jsonState.OutputFormat = "json"
	whole, _, err := execute(t, newFlowCmd(jsonState), "", "view", idFor("1").String())
	if err != nil {
		t.Fatal(err)
	}
	var got api.Flow
	if err := json.Unmarshal(
		[]byte(whole),
		&got,
	); err != nil || got.Id != flow.Id || got.FolderId == nil ||
		*got.FolderId != folder {
		t.Errorf("-o json = %q: %v", whole, err)
	}

	yamlState := stub.state(t)
	yamlState.OutputFormat = "yaml"
	asYAML, _, err := execute(t, newFlowCmd(yamlState), "", "view", idFor("1").String())
	if err != nil || !strings.Contains(asYAML, "flow_definition:") ||
		!strings.Contains(asYAML, "folder_id: "+folder.String()) {
		t.Errorf("-o yaml = %q: %v", asYAML, err)
	}
}

func TestFlowViewAnswersToGetAndShowToo(t *testing.T) {
	stub := newAPIStub(t)
	stub.on(http.MethodGet, "/flows/"+idFor("1").String(), http.StatusOK, sampleFlow(idFor("1"), "Checkout", nil))
	var outputs []string

	for _, verb := range []string{"view", "get", "show"} {
		stdout, _, err := execute(t, newFlowCmd(stub.tableState(t)), "", verb, idFor("1").String())
		if err != nil {
			t.Fatal(err)
		}
		outputs = append(outputs, stdout)
	}

	if outputs[0] != outputs[1] || outputs[0] != outputs[2] || outputs[0] == "" {
		t.Errorf("view, get and show differ: %q", outputs)
	}
}

func TestFlowLaunchAndRunSayWhatSeparatesThem(t *testing.T) {
	root := NewRootCmd()
	launch, run := find(t, root, "flow", "launch"), find(t, root, "flow", "run")

	for _, want := range []string{"Cloud", "Self-hosted", "without waiting"} {
		if !strings.Contains(launch.Short, want) {
			t.Errorf("launch.Short = %q lacks %q", launch.Short, want)
		}
	}
	if !strings.Contains(launch.Long, "print the execution id") ||
		!strings.Contains(launch.Long, "echopoint flow run") {
		t.Errorf("launch.Long does not print the execution id and point at run:\n%s", launch.Long)
	}
	if !strings.Contains(run.Short, "Ephemeral") || !strings.Contains(run.Short, "wait") {
		t.Errorf("run.Short = %q", run.Short)
	}
	if !strings.Contains(run.Long, "echopoint flow launch") || !strings.Contains(run.Long, "live progress") {
		t.Errorf("run.Long does not point at launch:\n%s", run.Long)
	}
	for _, cmd := range []string{"launch", "run"} {
		environment := find(t, root, "flow", cmd).Flags().Lookup("environment")
		if environment == nil || environment.Shorthand != "e" {
			t.Errorf("flow %s --environment = %+v, want it with -e", cmd, environment)
		}
	}
}

func TestFlowLaunchStillLaunchesWithoutWaiting(t *testing.T) {
	stub := newAPIStub(t)
	stub.on(http.MethodPost, "/flows/"+idFor("1").String()+"/launch", http.StatusAccepted, launchResponse(false))

	stdout, _, err := execute(
		t,
		newFlowCmd(stub.tableState(t)),
		"",
		"launch",
		idFor("1").String(),
		"--runner",
		"self_hosted",
		"-e",
		"dev",
	)

	if err != nil || !strings.Contains(stdout, "Execution ID: "+executionUUID().String()) {
		t.Fatalf("stdout %q, err %v", stdout, err)
	}
	sent := bodyOf(t, stub.requestsTo(http.MethodPost, "/flows/"+idFor("1").String()+"/launch")[0])
	if sent["runner_type"] != "self_hosted" || sent["environment_key"] != "dev" {
		t.Errorf("sent %v", sent)
	}
	if stub.requestCount() != 1 {
		t.Errorf("launch made %d requests, want only the launch", stub.requestCount())
	}
}
