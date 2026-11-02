package flowdsl_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/flowdsl"
)

const loginYAML = `
name: "anchor: login (reusable)"
role: module
id: eac266b3-2f3f-41b4-b061-f63b92b7b80e
folder: Anchor/Modules
inputs: [email, password]
nodes:
  request:
    request:
      method: post
      url: "{{nanostackBaseUrl}}/v1/auth/login"
      body:
        email: "{{email}}"
        password: "{{password}}"
    outputs:
      accessToken:
        path: $.accessToken
        secret: true
      refreshToken: $.refreshToken
    expect:
      status: 200
`

const memberYAML = `
name: "echopoint: organization member role"
role: standalone
tags: [echopoint]
folder: 11111111-1111-4111-8111-111111111111
nodes:
  login:
    module: anchor/login
    inputs:
      email: "{{email}}"
      password: "{{password}}"
    next: [member, stranger]
  member:
    request:
      method: GET
      url: "{{echopointUrl}}/organizations/{{echopointOrgId}}/members"
      headers:
        Authorization: "Bearer {{login.accessToken}}"
    expect: "status: 200"
  stranger:
    request:
      method: GET
      url: "{{echopointUrl}}/organizations/{{otherOrgId}}/members"
      headers:
        Authorization: "Bearer {{login.refreshToken}}"
    expect: "status: 404"
`

func TestCompileRewritesTheLoginModuleAndItsCaller(t *testing.T) {
	root := t.TempDir()
	writeFlow(t, root, "anchor/login.flow.yaml", loginYAML)
	writeFlow(t, root, "echopoint/member-role.flow.yaml", memberYAML)

	out, err := flowdsl.CompileDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Errors()) != 0 {
		t.Fatalf("errors: %v", out.Errors())
	}
	if len(out.Warnings()) != 1 || !strings.Contains(out.Warnings()[0].Message, "Anchor/Modules") {
		t.Fatalf("warnings: %v", out.Warnings())
	}

	assertCompiledLogin(t, flowByPath(t, out, "anchor/login"))
	caller := flowByPath(t, out, "echopoint/member-role")
	if caller.AutoLayout == nil || !*caller.AutoLayout {
		t.Fatal("auto_layout")
	}
	if caller.Version == nil || *caller.Version != "1.0" || caller.FlowDefinition.Version != "1.0" {
		t.Fatalf("version: %#v / %s", caller.Version, caller.FlowDefinition.Version)
	}
	if caller.FolderId == nil || caller.FolderId.String() != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("folder: %v", caller.FolderId)
	}
	if caller.Tags == nil || len(*caller.Tags) != 1 || (*caller.Tags)[0] != "echopoint" {
		t.Fatalf("tags: %v", caller.Tags)
	}
	loginNode := nodeByID(t, caller, "login")
	bindings := object(t, object(t, loginNode, "data"), "output_bindings")
	if bindings["accessToken"] != "request.accessToken" || bindings["refreshToken"] != "request.refreshToken" {
		t.Fatalf("bindings: %#v", bindings)
	}
	if object(t, loginNode, "data")["flow_id"] != "eac266b3-2f3f-41b4-b061-f63b92b7b80e" {
		t.Fatalf("flow_id: %#v", object(t, loginNode, "data")["flow_id"])
	}
	inputs := object(t, object(t, loginNode, "data"), "input_bindings")
	if inputs["email"] != "{{email}}" || inputs["password"] != "{{password}}" {
		t.Fatalf("inputs: %#v", inputs)
	}
	assertStatus(t, nodeByID(t, caller, "member"), "200")
	assertStatus(t, nodeByID(t, caller, "stranger"), "404")
	edges := edgeIDs(t, caller)
	for _, id := range []string{"login-member", "login-stranger"} {
		if !edges[id] {
			t.Fatalf("missing edge %s in %v", id, edges)
		}
	}
}

func assertCompiledLogin(t *testing.T, login api.CreateFlowRequest) {
	t.Helper()
	if login.FolderId != nil {
		t.Fatalf("folder path was sent: %v", login.FolderId)
	}
	if login.Role == nil || *login.Role != api.FlowRoleModule {
		t.Fatalf("role: %v", login.Role)
	}
	request := nodeByID(t, login, "request")
	if request["type"] != "request" || request["display_name"] != "request" || request["run_when"] != "on_success" {
		t.Fatalf("request node: %#v", request)
	}
	data := object(t, request, "data")
	if data["method"] != "POST" {
		t.Fatalf("method: %#v", data["method"])
	}
	outputs := array(t, request, "outputs")
	if len(outputs) != 2 || outputs[0]["name"] != "accessToken" || outputs[0]["secret"] != true {
		t.Fatalf("outputs: %#v", outputs)
	}
	assertStatus(t, request, "200")
}

func TestModuleFileWithoutAnIDStillCompilesOnItsOwn(t *testing.T) {
	root := t.TempDir()
	writeFlow(t, root, "anchor/login.flow.yaml", `
name: "anchor: login (reusable)"
role: module
nodes:
  request:
    request:
      method: GET
      url: "{{base}}/login"
`)
	writeFlow(t, root, "echopoint/caller.flow.yaml", `
name: caller
nodes:
  login:
    module: anchor/login
`)

	alone, err := flowdsl.CompileFile(filepath.Join(root, "anchor/login.flow.yaml"), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(alone.Errors()) != 0 || len(alone.Flows) != 1 {
		t.Fatalf("alone errors %v flows %d", alone.Errors(), len(alone.Flows))
	}

	both, err := flowdsl.CompileDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(both.Errors()) == 0 || !strings.Contains(both.Errors()[0].String(), "has no id") {
		t.Fatalf("dir errors: %v", both.Errors())
	}
}

func TestCompileReportsUnknownOutputAndAPollBodyParentReference(t *testing.T) {
	root := t.TempDir()
	writeFlow(t, root, "bad.flow.yaml", `
name: bad
nodes:
  login:
    request:
      method: GET
      url: "{{base}}/login"
    outputs:
      accessToken: $.accessToken
    next: wait
  wait:
    poll:
      max_attempts: 5
      interval_ms: 1000
      timeout_ms: 20000
      body:
        nodes:
          probe:
            request:
              method: GET
              url: "{{base}}/licenses/{{login.accessToken}}"
    expect: '$["probe.state"] equals ready'
  other:
    request:
      method: GET
      url: "{{missing.token}}/x"
`)

	out, err := flowdsl.CompileDir(root)
	if err != nil {
		t.Fatal(err)
	}
	text := joinDiags(out.Errors())
	if !strings.Contains(text, "poll or loop body only sees its own nodes") || !strings.Contains(text, "missing") {
		t.Fatalf("errors:\n%s", text)
	}
	if len(out.Flows) != 0 {
		t.Fatalf("compiled %d flows", len(out.Flows))
	}
}

func TestBranchPollAndStepsLowerToTheRunnerGraph(t *testing.T) {
	root := t.TempDir()
	writeFlow(t, root, "suite.flow.yaml", `
name: "echopoint: license ready"
tags: [echopoint]
nodes:
  list:
    request:
      method: GET
      url: "{{base}}/licenses/{{licenseId}}"
    outputs:
      state: $.state
    next: route
    on_failure: cleanup
  route:
    branch:
      target: "{{list.state}}"
      cases:
        - when: "body equals ready"
          target: done
      default: wait
  done:
    set:
      ok: true
  wait:
    poll:
      max_attempts: 5
      interval_ms: 1000
      timeout_ms: 20000
      body:
        steps:
          - id: probe
            request:
              method: GET
              url: "{{base}}/licenses/{{licenseId}}"
            outputs:
              state: $.state
    expect: '$["probe.state"] equals ready'
  cleanup:
    run_when: always
    set:
      failed: true
`)

	out, err := flowdsl.CompileDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Errors()) != 0 {
		t.Fatalf("errors: %v", out.Errors())
	}
	flow := out.Flows[0].Request
	edges := edgeIDs(t, flow)
	for _, id := range []string{"list-route", "route-done", "route-wait", "list-cleanup-failure"} {
		if !edges[id] {
			t.Fatalf("missing %s in %v", id, edges)
		}
	}
	wait := nodeByID(t, flow, "wait")
	body := object(t, object(t, wait, "data"), "body")
	probe := findByID(t, array(t, body, "nodes"), "probe")
	if probe["id"] != "probe" {
		t.Fatalf("probe: %#v", probe)
	}
	assertCheck(t, wait, `$["probe.state"]`, "equals", "ready")
	if nodeByID(t, flow, "cleanup")["run_when"] != "always" {
		t.Fatal("cleanup run_when")
	}
}

func TestFailureEdgeWithoutAlwaysWarns(t *testing.T) {
	root := t.TempDir()
	writeFlow(t, root, "fail.flow.yaml", `
name: fail
nodes:
  call:
    request:
      method: GET
      url: "{{base}}/x"
    on_failure: cleanup
  cleanup:
    set:
      failed: true
`)
	out, err := flowdsl.CompileDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Errors()) != 0 || len(out.Warnings()) != 1 ||
		!strings.Contains(out.Warnings()[0].Message, "run_when: always") {
		t.Fatalf("errors %v warnings %v", out.Errors(), out.Warnings())
	}
	if len(out.Flows) != 1 {
		t.Fatalf("flows %d", len(out.Flows))
	}
}

func TestCompileFileIgnoresAnUnrelatedBrokenFile(t *testing.T) {
	root := t.TempDir()
	writeFlow(t, root, "good.flow.yaml", `
name: good
nodes:
  call:
    request:
      method: GET
      url: "{{base}}/x"
`)
	writeFlow(t, root, "broken.flow.yaml", "name: [\n")

	out, err := flowdsl.CompileFile(filepath.Join(root, "good.flow.yaml"), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Errors()) != 0 || len(out.Flows) != 1 || out.Flows[0].Path != "good" {
		t.Fatalf("file compile: errors %v flows %#v", out.Errors(), out.Flows)
	}
	dir, err := flowdsl.CompileDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(dir.Errors()) == 0 {
		t.Fatal("directory compile accepted the broken file")
	}
}

func TestUnknownFieldIsAnError(t *testing.T) {
	root := t.TempDir()
	writeFlow(t, root, "odd.flow.yaml", `
name: odd
colour: blue
nodes:
  call:
    request:
      method: GET
      url: "{{base}}/x"
`)
	out, err := flowdsl.CompileDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Errors()) != 1 || !strings.Contains(out.Errors()[0].Message, "colour") {
		t.Fatalf("errors: %v", out.Errors())
	}
}

func TestRepeatedNamesAndPathsCompile(t *testing.T) {
	root := t.TempDir()
	writeFlow(t, root, "page.flow.yaml", `
name: pages
nodes:
  list:
    request:
      method: GET
      url: "{{base}}/products"
    outputs:
      items: $.items
      id: $.id
    next: [page, read]
  page:
    request:
      method: GET
      url: "{{base}}/products/{{list.items.0.id}}"
    outputs:
      id: $.id
  read:
    request:
      method: GET
      url: "{{base}}/products/{{page.id}}"
    expect: "\"$.items[?(@.name == 'a b')].id\" equals 1"
  stream:
    sse:
      url: "{{base}}/events"
      method: GET
    next: done
  done:
    request:
      method: GET
      url: "{{base}}/events/{{stream.last.id}}"
  hook:
    webhook_wait:
      timeout_ms: 1000
    next: use
  use:
    request:
      method: GET
      url: "{{base}}/hooks/{{hook.body.token}}"
`)
	out, err := flowdsl.CompileFile(filepath.Join(root, "page.flow.yaml"), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Errors()) != 0 || len(out.Flows) != 1 {
		t.Fatalf("errors %v flows %d", out.Errors(), len(out.Flows))
	}
	raw, err := json.Marshal(out.Flows[0].Request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `$.items[?(@.name == 'a b')].id`) {
		t.Fatal("filter path was not kept")
	}
}

func TestModuleWarnsOnARepeatedPublicOutput(t *testing.T) {
	root := t.TempDir()
	writeFlow(t, root, "mod.flow.yaml", `
name: mod
role: module
nodes:
  first:
    request:
      method: GET
      url: "{{base}}/a"
    outputs:
      token: $.token
  second:
    request:
      method: GET
      url: "{{base}}/b"
    outputs:
      token: $.token
`)
	out, err := flowdsl.CompileFile(filepath.Join(root, "mod.flow.yaml"), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Errors()) != 0 || len(out.Flows) != 1 || len(out.Warnings()) != 1 ||
		!strings.Contains(out.Warnings()[0].Message, "declared more than once") {
		t.Fatalf("errors %v warnings %v flows %d", out.Errors(), out.Warnings(), len(out.Flows))
	}
}

func writeFlow(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimSpace(body)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func flowByPath(t *testing.T, out flowdsl.Outcome, path string) api.CreateFlowRequest {
	t.Helper()
	for _, flow := range out.Flows {
		if flow.Path == path {
			return flow.Request
		}
	}
	t.Fatalf("no flow %s", path)
	return api.CreateFlowRequest{}
}

func nodeByID(t *testing.T, req api.CreateFlowRequest, id string) map[string]any {
	t.Helper()
	doc := asObject(t, req)
	def := object(t, doc, "flow_definition")
	return findByID(t, array(t, def, "nodes"), id)
}

func edgeIDs(t *testing.T, req api.CreateFlowRequest) map[string]bool {
	t.Helper()
	def := object(t, asObject(t, req), "flow_definition")
	ids := map[string]bool{}
	for _, item := range array(t, def, "edges") {
		ids[item["id"].(string)] = true
		if item["type"] == "failure" && !strings.HasSuffix(item["id"].(string), "-failure") {
			t.Fatalf("failure edge id: %#v", item)
		}
	}
	return ids
}

func assertStatus(t *testing.T, node map[string]any, code string) {
	t.Helper()
	assertCheck(t, node, "", "equals", code)
	checks := array(t, node, "assertions")
	if checks[0]["extractor_type"] != "status_code" {
		t.Fatalf("extractor: %#v", checks[0])
	}
}

func assertCheck(t *testing.T, node map[string]any, path, operator, value string) {
	t.Helper()
	checks := array(t, node, "assertions")
	if len(checks) != 1 {
		t.Fatalf("assertions: %#v", checks)
	}
	op := object(t, checks[0], "operator_data")
	if checks[0]["operator_type"] != operator || op["value"] != value {
		t.Fatalf("operator: %#v", checks[0])
	}
	if _, isString := op["value"].(string); !isString {
		t.Fatalf("operator value is %T, want a string", op["value"])
	}
	if path != "" {
		extractor := object(t, checks[0], "extractor_data")
		if extractor["path"] != path {
			t.Fatalf("path: %#v", extractor)
		}
	}
}

func findByID(t *testing.T, nodes []map[string]any, id string) map[string]any {
	t.Helper()
	for _, node := range nodes {
		if node["id"] == id {
			return node
		}
	}
	t.Fatalf("no node %s", id)
	return nil
}

func asObject(t *testing.T, value any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func object(t *testing.T, doc map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := doc[key].(map[string]any)
	if !ok {
		t.Fatalf("%s: %#v", key, doc[key])
	}
	return value
}

func array(t *testing.T, doc map[string]any, key string) []map[string]any {
	t.Helper()
	raw, ok := doc[key].([]any)
	if !ok {
		t.Fatalf("%s: %#v", key, doc[key])
	}
	out := make([]map[string]any, len(raw))
	for i, item := range raw {
		one, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("%s[%d]: %#v", key, i, item)
		}
		out[i] = one
	}
	return out
}

func joinDiags(diags []flowdsl.Diagnostic) string {
	parts := make([]string, len(diags))
	for i, diag := range diags {
		parts[i] = diag.String()
	}
	return strings.Join(parts, "\n")
}
