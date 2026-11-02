package commands

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFlowCompilePrintsTheCreateRequestAndChecksIt(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "login.flow.yaml")
	body := []byte(strings.TrimSpace(`
name: "anchor: login (reusable)"
role: module
nodes:
  request:
    request:
      method: POST
      url: "{{base}}/login"
    expect: "status: 200"
`) + "\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := runRoot(t, "flow", "compile", "-f", path)
	if err != nil {
		t.Fatalf("err %v stderr %s", err, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr: %s", stderr)
	}
	if !strings.Contains(stdout, `"auto_layout": true`) || !strings.Contains(stdout, `"flow_definition"`) {
		t.Fatalf("stdout:\n%s", stdout)
	}

	checked, checkErr, err := runRoot(t, "flow", "compile", "-f", path, "--check")
	if err != nil || checked != "" || checkErr != "" {
		t.Fatalf("check stdout %q stderr %q err %v", checked, checkErr, err)
	}

	yamlOut, _, err := runRoot(t, "flow", "compile", "-f", path, "-o", "yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(yamlOut, "flow_definition:") || !strings.Contains(yamlOut, "auto_layout:") {
		t.Fatalf("yaml:\n%s", yamlOut)
	}
	if strings.Contains(yamlOut, "flowdefinition:") || strings.Contains(yamlOut, "autolayout:") {
		t.Fatalf("yaml used Go names:\n%s", yamlOut)
	}
}

func TestFlowCreateCompilesAFlowFileAndSendsTheJSON(t *testing.T) {
	file := writeTemp(t, "checkout.flow.yaml", `
name: Checkout
nodes:
  create:
    request:
      method: POST
      url: "{{base}}/orders"
    expect: "status: 201"
    outputs:
      orderId: $.id
`)
	stub := newAPIStub(t)
	stub.on(http.MethodPost, "/flows", http.StatusCreated, sampleFlow(idFor("1"), "Checkout", nil))

	_, stderr, err := execute(t, newFlowCmd(stub.tableState(t)), "", "create", "-f", file)
	if err != nil {
		t.Fatalf("err %v stderr %s", err, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr: %s", stderr)
	}
	sent := bodyOf(t, stub.requestsTo(http.MethodPost, "/flows")[0])
	definition, _ := sent["flow_definition"].(map[string]any)
	nodes, _ := definition["nodes"].([]any)
	if sent["name"] != "Checkout" || len(nodes) != 1 {
		t.Fatalf("sent %v", sent)
	}
}

func TestFlowCreateSendsAPIJSONOnlyWithTheFlag(t *testing.T) {
	file := writeTemp(t, "flow.json", `{"name":"Checkout","flow_definition":{"nodes":[],"edges":[]}}`)
	stub := newAPIStub(t)
	stub.on(http.MethodPost, "/flows", http.StatusCreated, sampleFlow(idFor("1"), "Checkout", nil))

	_, stderr, err := execute(t, newFlowCmd(stub.state(t)), "", "create", "-f", file)
	if err == nil || !strings.Contains(err.Error(), "--json") || stub.requestCount() != 0 {
		t.Fatalf("err %v stderr %q requests %d", err, stderr, stub.requestCount())
	}

	_, stderr, err = execute(t, newFlowCmd(stub.tableState(t)), "", "create", "-f", file, "--json")
	if err != nil {
		t.Fatalf("json err %v stderr %s", err, stderr)
	}
	sent := bodyOf(t, stub.requestsTo(http.MethodPost, "/flows")[0])
	if sent["name"] != "Checkout" || sent["auto_layout"] != nil {
		t.Fatalf("sent %v", sent)
	}
}

func TestFlowCompileFailsOnAProblemAndOnAMissingFileFlag(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "bad.flow.yaml")
	if err := os.WriteFile(path, []byte("name: bad\ncolour: blue\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := runRoot(t, "flow", "compile", "-f", path, "--check")
	if err == nil || err.Error() != "exit code 1" {
		t.Fatalf("err %v", err)
	}
	if stdout != "" || !strings.Contains(stderr, "colour") {
		t.Fatalf("stdout %q stderr %q", stdout, stderr)
	}

	_, _, err = runRoot(t, "flow", "compile")
	if err == nil || err.Error() != "pass -f with a flow file or a directory" {
		t.Fatalf("missing -f: %v", err)
	}
	_, _, err = runRoot(t, "flow", "compile", path)
	if err == nil || err.Error() != "pass the file with -f" {
		t.Fatalf("positional: %v", err)
	}
}
