package commands

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"echopoint-cli/internal/api"
)

func requestNodeFlow(t *testing.T, outputs string) api.Flow {
	t.Helper()
	flow := sampleFlow(idFor("1"), "Token flow", nil)
	var node api.FlowNode
	if err := json.Unmarshal([]byte(`{"id":"mint","type":"request","display_name":"Mint",
		"data":{"method":"POST","url":"https://api.example.com/token"},"outputs":`+outputs+`}`), &node); err != nil {
		t.Fatal(err)
	}
	flow.FlowDefinition.Nodes = append(flow.FlowDefinition.Nodes, node)
	return flow
}

// addOutput runs "flow node output add" against a stub and returns the outputs
// the command saved.
func addOutput(t *testing.T, flow api.Flow, args ...string) []map[string]any {
	t.Helper()
	stub := newAPIStub(t)
	path := "/flows/" + flow.Id.String()
	stub.on(http.MethodGet, path, http.StatusOK, flow)
	stub.on(http.MethodPut, path, http.StatusOK, flow)
	base := []string{"node", "output", "add", flow.Id.String(), "mint"}
	if _, _, err := execute(t, newFlowCmd(stub.state(t)), "", append(base, args...)...); err != nil {
		t.Fatal(err)
	}
	requests := stub.requestsTo(http.MethodPut, path)
	if len(requests) != 1 {
		t.Fatalf("got %d updates", len(requests))
	}
	var update struct {
		FlowDefinition struct {
			Nodes []struct {
				Outputs []map[string]any `json:"outputs"`
			} `json:"nodes"`
		} `json:"flow_definition"`
	}
	if err := json.Unmarshal(requests[0].body, &update); err != nil {
		t.Fatal(err)
	}
	return update.FlowDefinition.Nodes[0].Outputs
}

func TestOutputAddSecretFlagMarksTheOutputSecret(t *testing.T) {
	outputs := addOutput(t, requestNodeFlow(t, `[]`),
		"--name", "jwt", "--extractor", "json_path", "--path", "$.jwt", "--secret")

	if len(outputs) != 1 || outputs[0]["secret"] != true {
		t.Fatalf("the added output should be secret, got %v", outputs)
	}
}

func TestOutputAddWithoutSecretFlagLeavesTheOutputPlain(t *testing.T) {
	outputs := addOutput(t, requestNodeFlow(t, `[]`),
		"--name", "id", "--extractor", "json_path", "--path", "$.id")

	if _, present := outputs[0]["secret"]; present {
		t.Fatalf("an output added without --secret should not carry the flag, got %v", outputs)
	}
}

// The CLI rewrites the whole definition on every node edit, so a flag its
// client did not know would be dropped from the outputs already there.
func TestOutputAddKeepsAnExistingSecretOutput(t *testing.T) {
	flow := requestNodeFlow(t, `[{"name":"jwt","secret":true,"extractor":{"type":"json_path","path":"$.jwt"}}]`)

	outputs := addOutput(t, flow, "--name", "session", "--extractor", "json_path", "--path", "$.sid")

	encoded, _ := json.Marshal(outputs)
	if len(outputs) != 2 || outputs[0]["secret"] != true {
		t.Fatalf("the existing secret output should keep its flag, got %s", encoded)
	}
	if strings.Count(string(encoded), `"secret"`) != 1 {
		t.Fatalf("only the existing output should be secret, got %s", encoded)
	}
}
