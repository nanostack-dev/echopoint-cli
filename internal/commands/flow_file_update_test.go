package commands

import (
	"net/http"
	"strings"
	"testing"
)

const pingNodes = `
nodes:
  ping:
    request:
      method: GET
      url: "{{base}}/health"
    expect: "status: 200"
`

// updateFromFlowFile runs `flow update <id> -f file` on a compiled flow file
// and returns the raw body it sent.
func updateFromFlowFile(t *testing.T, header string) (string, map[string]any) {
	t.Helper()
	flow := idFor("1")
	file := writeTemp(t, "ping.flow.yaml", header+pingNodes)
	stub := newAPIStub(t)
	stub.on(http.MethodPut, "/flows/"+flow.String(), http.StatusOK, sampleFlow(flow, "Ping", nil))

	_, stderr, err := execute(t, newFlowCmd(stub.tableState(t)), "", "update", flow.String(), "-f", file)
	if err != nil {
		t.Fatalf("err %v stderr %s", err, stderr)
	}
	sent := stub.requestsTo(http.MethodPut, "/flows/"+flow.String())
	if len(sent) != 1 {
		t.Fatalf("sent %d updates", len(sent))
	}
	return string(sent[0].body), bodyOf(t, sent[0])
}

// The API keeps the stored tags, description and role when an update leaves
// them out, and clears them when it sends an empty value (echopoint#893).
func TestFlowUpdateFromAFlowFileClearsOnlyWhatTheFileEmpties(t *testing.T) {
	raw, _ := updateFromFlowFile(t, "name: Ping\ntags: []\ndescription: \"\"\n")
	if !strings.Contains(raw, `"tags":[]`) || !strings.Contains(raw, `"description":""`) {
		t.Errorf("an explicit empty tags list and description must reach the API as clears, sent %s", raw)
	}

	raw, sent := updateFromFlowFile(t, "name: Ping\n")
	for _, key := range []string{"tags", "description", "role"} {
		if _, has := sent[key]; has {
			t.Errorf("a file without %s must leave it out so the API keeps it, sent %s", key, raw)
		}
	}

	raw, sent = updateFromFlowFile(t, "name: Ping\ntags: [smoke]\nrole: function\n")
	tags, _ := sent["tags"].([]any)
	if len(tags) != 1 || tags[0] != "smoke" || sent["role"] != "function" {
		t.Errorf("sent %s", raw)
	}
}

func TestFlowFileSendsAFormBody(t *testing.T) {
	file := writeTemp(t, "token.flow.yaml", `
name: Token
nodes:
  token:
    request:
      method: POST
      url: "{{base}}/oauth/token"
      body_encoding: form
      body:
        grant_type: client_credentials
    expect: "status: 200"
`)
	stub := newAPIStub(t)
	stub.on(http.MethodPost, "/flows", http.StatusCreated, sampleFlow(idFor("1"), "Token", nil))

	_, stderr, err := execute(t, newFlowCmd(stub.tableState(t)), "", "create", "-f", file)
	if err != nil {
		t.Fatalf("err %v stderr %s", err, stderr)
	}
	raw := string(stub.requestsTo(http.MethodPost, "/flows")[0].body)
	if !strings.Contains(raw, `"body_encoding":"form"`) {
		t.Errorf("the request node lost body_encoding: %s", raw)
	}
}

func TestFlowFileRefusesWhatASaveWouldRefuse(t *testing.T) {
	cycle := `
name: Loop
nodes:
  first:
    request:
      method: GET
      url: "{{base}}/a"
    next: second
  second:
    request:
      method: GET
      url: "{{base}}/b"
    next: first
`
	for name, tc := range map[string]struct {
		body string
		want string
	}{
		"invalid tag":    {"name: Ping\ntags: [\"has space\"]\n" + pingNodes, `tag "has space" is invalid`},
		"too many tags":  {"name: Ping\ntags: [" + manyTags(33) + "]\n" + pingNodes, "at most 32 tags"},
		"cycle":          {cycle, "makes a cycle"},
		"two documents":  {"name: Ping\n" + pingNodes + "---\nname: Other\n" + pingNodes, "more than one YAML document"},
		"unknown encode": {strings.Replace("name: Ping\n"+pingNodes, "    expect:", "      body_encoding: xml\n    expect:", 1), "body_encoding must be form"},
	} {
		t.Run(name, func(t *testing.T) {
			flow := idFor("1")
			file := writeTemp(t, "bad.flow.yaml", tc.body)
			stub := newAPIStub(t)

			_, stderr, err := execute(t, newFlowCmd(stub.state(t)), "", "update", flow.String(), "-f", file)

			if err == nil || !strings.Contains(stderr+err.Error(), tc.want) || stub.requestCount() != 0 {
				t.Errorf("err %v stderr %q requests %d", err, stderr, stub.requestCount())
			}
		})
	}
}

func manyTags(n int) string {
	tags := make([]string, n)
	for i := range tags {
		tags[i] = "t" + strings.Repeat("x", i%5) + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	return strings.Join(tags, ",")
}
