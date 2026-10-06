package commands

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestResolveFlowIDReadsAFlowIDWithoutARequest(t *testing.T) {
	stub := newAPIStub(t)
	for _, arg := range []string{
		"550e8400-e29b-41d4-a716-446655440001",
		"550E8400-E29B-41D4-A716-446655440001",
	} {
		id, err := resolveFlowID(context.Background(), stub.state(t), arg)

		if err != nil || id != flowUUID() {
			t.Errorf("%q: id %v, err %v", arg, id, err)
		}
	}
	if stub.requestCount() != 0 {
		t.Errorf("an id made %d requests", stub.requestCount())
	}
}

func TestResolveFlowIDRefusesAnythingElseWithoutARequest(t *testing.T) {
	stub := newAPIStub(t)
	for _, arg := range []string{"", "checkout", "Not A Flow", "550e8400", "../flows/x", "550e8400-e29b-41d4-a716-44665544000"} {
		_, err := resolveFlowID(context.Background(), stub.state(t), arg)

		want := `"` + arg + `" is not a flow id; list the flows with: echopoint flow list`
		if err == nil || err.Error() != want {
			t.Errorf("%q: err = %v, want %q", arg, err, want)
		}
		var notAnID *notAFlowIDError
		if !errors.As(err, &notAnID) || errors.Unwrap(err) == nil {
			t.Errorf("%q: the error keeps no cause: %v", arg, err)
		}
	}
	if stub.requestCount() != 0 {
		t.Errorf("invalid input made %d requests", stub.requestCount())
	}
}

// Every command with a <flow-id> argument sends it through resolveFlowID.
func TestEveryFlowArgumentGoesThroughTheResolver(t *testing.T) {
	file := writeTemp(t, "flow.json", `{"name":"x"}`)
	execution := idFor("20").String()
	for name, args := range map[string][]string{
		"view":                  {"view", "Not A Flow"},
		"get":                   {"get", "Not A Flow"},
		"show":                  {"show", "Not A Flow"},
		"update":                {"update", "Not A Flow", "-f", file},
		"delete":                {"delete", "Not A Flow"},
		"launch":                {"launch", "Not A Flow"},
		"validate":              {"validate", "Not A Flow"},
		"execution list":        {"execution", "list", "Not A Flow"},
		"execution view":        {"execution", "view", "Not A Flow", execution},
		"node add":              {"node", "add", "Not A Flow", "--type", "delay", "--name", "x"},
		"node remove":           {"node", "remove", "Not A Flow", "n"},
		"node update":           {"node", "update", "Not A Flow", "n"},
		"node output add":       {"node", "output", "add", "Not A Flow", "n", "--name", "x", "--extractor", "body"},
		"node output remove":    {"node", "output", "remove", "Not A Flow", "n", "x"},
		"node assertion add":    {"node", "assertion", "add", "Not A Flow", "n", "--extractor", "body", "--operator", "equals"},
		"node assertion remove": {"node", "assertion", "remove", "Not A Flow", "n", "0"},
		"node expect add":       {"node", "expect", "add", "Not A Flow", "n", "--name", "e", "--match", "$.type equals x"},
		"node expect remove":    {"node", "expect", "remove", "Not A Flow", "n", "e"},
		"edge add":              {"edge", "add", "Not A Flow", "--from", "a", "--to", "b"},
		"edge remove":           {"edge", "remove", "Not A Flow", "e"},
		"env view":              {"env", "view", "Not A Flow"},
		"env get":               {"env", "get", "Not A Flow"},
		"env set":               {"env", "set", "Not A Flow", "--var", "A=1"},
		"env unset":             {"env", "unset", "Not A Flow", "A"},
		"env delete":            {"env", "delete", "Not A Flow"},
		"move":                  {"move", "Not A Flow", "--to", "uncategorized"},
	} {
		t.Run(name, func(t *testing.T) {
			stub := newAPIStub(t)
			state := stub.state(t)
			state.AssumeYes = true

			_, _, err := execute(t, newFlowCmd(state), "", args...)

			if err == nil ||
				!strings.Contains(
					err.Error(),
					`"Not A Flow" is not a flow id; list the flows with: echopoint flow list`,
				) {
				t.Errorf("err = %v", err)
			}
			if stub.requestCount() != 0 {
				t.Errorf("%d requests were made for an argument that is not a flow id", stub.requestCount())
			}
		})
	}
}

func TestFlowTagAndMoveReportWhyAnArgumentIsNotAFlow(t *testing.T) {
	stub := newAPIStub(t)
	isolateCLIEnvironment(t, stub.server.URL)
	const want = `"Not A Flow" is not a flow id; list the flows with: echopoint flow list`

	_, stderr, code := runCLI(t, "flow", "tag", "Not A Flow", "--add", "smoke")
	if code == 0 || !strings.Contains(stderr, want) || strings.Contains(stderr, "invalid flow id") {
		t.Errorf("tag: exit %d, stderr %q", code, stderr)
	}

	_, _, err := execute(t, newFlowCmd(stub.state(t)), "", "move", "Not A Flow", "--to", "uncategorized")
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("move: err = %v", err)
	}
	if stub.requestCount() != 0 {
		t.Errorf("%d requests", stub.requestCount())
	}
}

// 'flow run' keeps the words it has always used for an argument that is not a
// flow id, because the Action's consumers read them in the result.
func TestFlowRunKeepsItsOwnWordsForAnArgumentThatIsNotAFlowID(t *testing.T) {
	fake := newGoldenFlowServer(t, nil)
	isolateCLIEnvironment(t, fake.server.URL)

	stdout, _, code := runCLI(t, "flow", "run", "not-a-uuid", "-o", "json")

	if code != 3 ||
		!strings.Contains(stdout, `"error_message": "invalid flow id \"not-a-uuid\": invalid UUID length: 10"`) {
		t.Errorf("exit %d, stdout %q", code, stdout)
	}
	if fake.launchLines() != "" {
		t.Errorf("made requests %q", fake.launchLines())
	}
}
