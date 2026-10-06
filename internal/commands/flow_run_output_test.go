package commands

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestFlowRunReadsTheGlobalOutputFlagInsteadOfDefiningItsOwn(t *testing.T) {
	run := find(t, NewRootCmd(), "flow", "run")

	if run.LocalNonPersistentFlags().Lookup("output") != nil {
		t.Error("flow run defines its own --output")
	}
	if inherited := run.InheritedFlags().Lookup("output"); inherited == nil || inherited.Shorthand != "o" {
		t.Errorf("flow run inherits --output as %+v, want the global -o", inherited)
	}
}

func TestFlowRunTableIsTheDefaultAndKeepsStdoutEmpty(t *testing.T) {
	for _, args := range [][]string{{goldenReplayFlow}, {goldenReplayFlow, "-o", "table"}} {
		fake := newGoldenFlowServer(t, nil)
		isolateCLIEnvironment(t, fake.server.URL)

		stdout, stderr, code := runCLI(t, append([]string{"flow", "run"}, args...)...)

		if code != 0 || stdout != "" || !strings.Contains(stderr, "✓ Flow "+goldenReplayFlow[:8]) {
			t.Errorf("%v: exit %d, stdout %q, stderr %q", args, code, stdout, stderr)
		}
	}
}

func TestFlowRunYAMLPrintsTheObjectJSONPrints(t *testing.T) {
	fake := newGoldenFlowServer(t, nil)
	isolateCLIEnvironment(t, fake.server.URL)
	flows := []string{goldenReplayFlow, goldenForbiddenFlow}

	asJSON, _, jsonCode := runCLI(t, append([]string{"flow", "run"}, append(flows, "-o", "json")...)...)
	asYAML, stderr, yamlCode := runCLI(t, append([]string{"flow", "run"}, append(flows, "-o", "yaml")...)...)

	var fromJSON, fromYAML map[string]any
	if err := json.Unmarshal([]byte(asJSON), &fromJSON); err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal([]byte(asYAML), &fromYAML); err != nil {
		t.Fatalf("%v\n%s", err, asYAML)
	}
	// numbers decode differently, so compare through JSON
	normalized, _ := json.Marshal(fromYAML)
	var again map[string]any
	_ = json.Unmarshal(normalized, &again)
	if !reflect.DeepEqual(fromJSON, again) || jsonCode != 3 || yamlCode != 3 {
		t.Errorf("json (exit %d) and yaml (exit %d) differ:\n%s\n%s", jsonCode, yamlCode, asJSON, asYAML)
	}
	if strings.Contains(stderr, "Launching flow") {
		t.Errorf("yaml output still prints progress:\n%s", stderr)
	}
}

func TestFlowRunAnOutputTypedOnTheCommandBeatsTheEnvironmentVariable(t *testing.T) {
	fake := newGoldenFlowServer(t, nil)
	isolateCLIEnvironment(t, fake.server.URL)
	t.Setenv("ECHOPOINT_OUTPUT_FORMAT", "table")

	stdout, _, code := runCLI(t, "flow", "run", goldenReplayFlow, "-o", "json")

	if code != 0 || !json.Valid([]byte(stdout)) {
		t.Errorf("exit %d, stdout %q", code, stdout)
	}
}

func TestFlowRunFollowsTheEnvironmentVariableWhenNoOutputIsTyped(t *testing.T) {
	fake := newGoldenFlowServer(t, nil)
	isolateCLIEnvironment(t, fake.server.URL)
	t.Setenv("ECHOPOINT_OUTPUT_FORMAT", "json")

	stdout, _, code := runCLI(t, "flow", "run", goldenReplayFlow)

	if code != 0 || !json.Valid([]byte(stdout)) {
		t.Errorf("exit %d, stdout %q", code, stdout)
	}
}
