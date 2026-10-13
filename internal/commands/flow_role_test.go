package commands

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"echopoint-cli/internal/api"
)

func moduleFlow(name string) api.Flow {
	flow := sampleFlow(idFor("1"), name, nil)
	flow.Role = api.FlowRoleModule
	return flow
}

func TestFlowRoleShowsTheCurrentRole(t *testing.T) {
	stub := newAPIStub(t)
	stub.on(http.MethodGet, "/flows/"+idFor("1").String(), http.StatusOK, moduleFlow("Login"))

	stdout, _, err := execute(t, newFlowCmd(stub.tableState(t)), "", "role", idFor("1").String())

	if err != nil || stdout != "module\n" {
		t.Errorf("stdout = %q, err = %v", stdout, err)
	}
	if puts := stub.requestsTo(http.MethodPut, "/flows/"+idFor("1").String()); len(puts) != 0 {
		t.Errorf("reading the role sent %d updates", len(puts))
	}
}

func TestFlowRoleSetModuleSendsOnlyTheRole(t *testing.T) {
	flow := idFor("1")
	stub := newAPIStub(t)
	stub.on(http.MethodPut, "/flows/"+flow.String(), http.StatusOK, moduleFlow("Login"))

	stdout, _, err := execute(t, newFlowCmd(stub.tableState(t)), "", "role", flow.String(), "--set", "module")

	if err != nil || stdout != "module\n" {
		t.Errorf("stdout = %q, err = %v", stdout, err)
	}
	sent := bodyOf(t, stub.requestsTo(http.MethodPut, "/flows/"+flow.String())[0])
	if len(sent) != 1 || sent["role"] != "module" {
		t.Errorf("sent %v, want only role=module", sent)
	}
}

func TestFlowRoleSetStandaloneSendsOnlyTheRole(t *testing.T) {
	flow := idFor("1")
	stub := newAPIStub(t)
	stub.on(http.MethodPut, "/flows/"+flow.String(), http.StatusOK, sampleFlow(flow, "Login", nil))

	stdout, _, err := execute(t, newFlowCmd(stub.tableState(t)), "", "role", flow.String(), "--set", "standalone")

	if err != nil || stdout != "standalone\n" {
		t.Errorf("stdout = %q, err = %v", stdout, err)
	}
	sent := bodyOf(t, stub.requestsTo(http.MethodPut, "/flows/"+flow.String())[0])
	if len(sent) != 1 || sent["role"] != "standalone" {
		t.Errorf("sent %v, want only role=standalone", sent)
	}
}

func TestFlowRoleRejectsAnUnknownRoleBeforeAnyRequest(t *testing.T) {
	for _, value := range []string{"library", "Module", ""} {
		t.Run(value, func(t *testing.T) {
			stub := newAPIStub(t)

			_, _, err := execute(t, newFlowCmd(stub.tableState(t)), "", "role", idFor("1").String(), "--set", value)

			want := `invalid role "` + value + `"; must be "standalone" or "module"`
			if err == nil || err.Error() != want || stub.requestCount() != 0 {
				t.Errorf("err = %v, %d requests", err, stub.requestCount())
			}
		})
	}
}

func TestFlowRolePrintsTheIDAndRoleAsJSON(t *testing.T) {
	flow := idFor("1")
	stub := newAPIStub(t)
	stub.on(http.MethodGet, "/flows/"+flow.String(), http.StatusOK, moduleFlow("Login"))
	stub.on(http.MethodPut, "/flows/"+flow.String(), http.StatusOK, sampleFlow(flow, "Login", nil))
	state := stub.state(t)
	state.OutputFormat = "json"

	for _, args := range [][]string{
		{"role", flow.String()},
		{"role", flow.String(), "--set", "standalone"},
	} {
		stdout, _, err := execute(t, newFlowCmd(state), "", args...)
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]string
		if err := json.Unmarshal([]byte(stdout), &got); err != nil || len(got) != 2 || got["id"] != flow.String() {
			t.Fatalf("%v: stdout %q, err %v", args, stdout, err)
		}
		want := "module"
		if len(args) > 2 {
			want = "standalone"
		}
		if got["role"] != want {
			t.Errorf("%v: role = %q, want %q", args, got["role"], want)
		}
	}
}

func TestFlowRoleRejectsAnArgumentThatIsNotAFlowID(t *testing.T) {
	stub := newAPIStub(t)

	_, _, err := execute(t, newFlowCmd(stub.tableState(t)), "", "role", "login")

	if err == nil || !strings.Contains(err.Error(), "is not a flow id") || stub.requestCount() != 0 {
		t.Errorf("err = %v, %d requests", err, stub.requestCount())
	}
}
