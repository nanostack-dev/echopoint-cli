package commands

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
)

func moduleNodeChild(t *testing.T, request stubRequest) string {
	t.Helper()
	var body struct {
		FlowDefinition struct {
			Nodes []struct {
				Data struct {
					FlowID string `json:"flow_id"`
				} `json:"data"`
			} `json:"nodes"`
		} `json:"flow_definition"`
	}
	if err := json.Unmarshal(request.body, &body); err != nil || len(body.FlowDefinition.Nodes) != 1 {
		t.Fatalf("body %s: %v", request.body, err)
	}
	return body.FlowDefinition.Nodes[0].Data.FlowID
}

func moduleNodeStub(t *testing.T) *apiStub {
	t.Helper()
	parent := idFor("1")
	stub := newAPIStub(t)
	stub.on(http.MethodGet, "/flows/"+parent.String(), http.StatusOK, sampleFlow(parent, "Parent", nil))
	stub.on(http.MethodPut, "/flows/"+parent.String(), http.StatusOK, sampleFlow(parent, "Parent", nil))
	return stub
}

func TestNodeAddTakesTheSubflowThroughFlowID(t *testing.T) {
	parent, child := idFor("1").String(), idFor("2").String()
	for name, flag := range map[string][]string{
		"separate":   {"--flow-id", child},
		"with equal": {"--flow-id=" + child},
	} {
		t.Run(name, func(t *testing.T) {
			stub := moduleNodeStub(t)
			args := append([]string{"node", "add", parent, "--type", "module", "--name", "Login"}, flag...)

			_, _, err := execute(t, newFlowCmd(stub.state(t)), "", args...)

			if err != nil {
				t.Fatal(err)
			}
			if got := moduleNodeChild(t, stub.requestsTo(http.MethodPut, "/flows/"+parent)[0]); got != child {
				t.Errorf("the module runs flow %s, want %s", got, child)
			}
		})
	}
}

func TestNodeAddRefusesASubflowThatIsNotAnIDOrIsMissing(t *testing.T) {
	parent := idFor("1").String()
	for name, tc := range map[string]struct {
		flag []string
		want string
	}{
		"not an id": {[]string{"--flow-id", "Not A Flow"}, `--flow-id: "Not A Flow" is not a flow id; list the flows with: echopoint flow list`},
		"missing":   {nil, "--flow-id is required for module nodes"},
	} {
		t.Run(name, func(t *testing.T) {
			stub := moduleNodeStub(t)
			args := append([]string{"node", "add", parent, "--type", "module", "--name", "Login"}, tc.flag...)

			_, _, err := execute(t, newFlowCmd(stub.state(t)), "", args...)

			if err == nil || err.Error() != tc.want || len(stub.requestsTo(http.MethodPut, "/flows/"+parent)) != 0 {
				t.Errorf("err = %v", err)
			}
		})
	}
}

func TestFlowIDIsTheVisibleFlagOnBothCommandsAndThereIsNoFlowFlag(t *testing.T) {
	root := NewRootCmd()
	for _, path := range [][]string{{"flow", "node", "add"}, {"status-page", "binding-options"}} {
		cmd := find(t, root, path...)
		flowID := cmd.Flags().Lookup("flow-id")
		if flowID == nil || flowID.Hidden || cmd.Flags().Lookup("flow") != nil {
			t.Errorf("%v: --flow-id = %+v, --flow = %+v", path, flowID, cmd.Flags().Lookup("flow"))
		}
		if !strings.Contains(cmd.UsageString(), "--flow-id ") {
			t.Errorf("%v: the help lacks --flow-id:\n%s", path, cmd.UsageString())
		}
	}
}

func TestBindingOptionsTakesTheFlowIDAndRefusesAnythingElse(t *testing.T) {
	flow, schedule := idFor("1"), idFor("5").String()
	stub := newAPIStub(t)
	stub.on(http.MethodGet, "/status-pages/binding-options", http.StatusOK, map[string]any{"checks": []any{}})

	_, _, err := execute(
		t,
		newStatusPageCmd(stub.state(t)),
		"",
		"binding-options",
		"--schedule-id",
		schedule,
		"--flow-id",
		flow.String(),
	)
	if err != nil {
		t.Fatal(err)
	}
	sent := stub.requestsTo(http.MethodGet, "/status-pages/binding-options")
	if len(sent) != 1 || !strings.Contains(sent[0].query, "flow_id="+flow.String()) {
		t.Errorf("sent %+v", sent)
	}

	_, _, err = execute(
		t,
		newStatusPageCmd(stub.state(t)),
		"",
		"binding-options",
		"--schedule-id",
		schedule,
		"--flow-id",
		"Not A Flow",
	)
	if err == nil ||
		err.Error() != `--flow-id: "Not A Flow" is not a flow id; list the flows with: echopoint flow list` {
		t.Errorf("not an id: err = %v", err)
	}
	_, _, err = execute(t, newStatusPageCmd(stub.state(t)), "", "binding-options", "--schedule-id", schedule)
	if err == nil || err.Error() != `required flag(s) "flow-id" not set` {
		t.Errorf("without a flow: err = %v", err)
	}
	if len(stub.requestsTo(http.MethodGet, "/status-pages/binding-options")) != 1 {
		t.Error("a request was made for input that is not a flow id")
	}
}

func TestTheFlowIDFlagCompletesLikeAFlowArgument(t *testing.T) {
	completionStub(t)
	want := []string{
		idFor("1").String() + "\tCheckout · Anchor/Identity",
		idFor("2").String() + "\tLogin page",
	}

	for _, args := range [][]string{
		{"flow", "node", "add", idFor("1").String(), "--flow-id", ""},
		{"status-page", "binding-options", "--flow-id", ""},
	} {
		if got := completeCLI(t, args...); !slices.Equal(got, want) {
			t.Errorf("%v = %q, want %q", args, got, want)
		}
	}
}
