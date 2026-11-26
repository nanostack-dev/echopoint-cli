package commands

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"echopoint-cli/internal/api"
)

// workspaceEnvStub serves every workspace variable and environment route the
// workspace env commands call.
func workspaceEnvStub(t *testing.T) *apiStub {
	t.Helper()
	stub := newAPIStub(t)
	set := map[string]any{
		"id": idFor("50"), "organization_id": "org_test", "workspace_id": "ws_payments",
		"created_at": "2026-11-01T00:00:00Z", "updated_at": "2026-11-01T00:00:00Z",
		"base": map[string]any{
			"BASE_URL": map[string]any{"value": "https://api.example.com", "secret": false},
			"API_KEY":  map[string]any{"secret": true},
		},
		"environments": map[string]any{"dev": map[string]any{}, "sandbox": map[string]any{}},
	}
	stub.on(http.MethodGet, "/workspace/variables", http.StatusOK, set)
	stub.on(http.MethodPut, "/workspace/variables/API_KEY", http.StatusOK, set)
	stub.on(http.MethodPut, "/workspace/environments/sandbox/variables/BASE_URL", http.StatusOK, set)
	stub.on(http.MethodDelete, "/workspace/variables/API_KEY", http.StatusNoContent, nil)
	stub.on(http.MethodDelete, "/workspace/environments/dev/variables/API_KEY", http.StatusNoContent, nil)
	stub.on(http.MethodPost, "/workspace/environments", http.StatusCreated, map[string]any{
		"id": idFor("42"), "name": "sandbox", "scope": "workspace",
		"created_at": "2026-11-01T00:00:00Z", "updated_at": "2026-11-01T00:00:00Z",
	})
	stub.onSearch("/workspace/environments/search",
		api.Environment{Id: idFor("42"), Name: "sandbox", Scope: api.EnvironmentScopeWorkspace},
		api.Environment{Id: idFor("40"), Name: "dev", Scope: api.EnvironmentScopeOrganization})
	isolateCLIEnvironment(t, stub.server.URL)
	return stub
}

// onlyRequestTo returns the one request sent to a route.
func onlyRequestTo(t *testing.T, stub *apiStub, method, path string) stubRequest {
	t.Helper()
	requests := stub.requestsTo(method, path)
	if len(requests) != 1 {
		t.Fatalf("sent %d %s %s, want 1", len(requests), method, path)
	}
	return requests[0]
}

func TestWorkspaceEnvWritesReachTheSelectedWorkspaceOrTheDefaultOne(t *testing.T) {
	stub := workspaceEnvStub(t)

	if _, stderr, code := runCLI(t, "workspace", "env", "set", "--workspace", "ws_payments",
		"--secret", "--var", "API_KEY=sk-live"); code != 0 {
		t.Fatalf("set exited %d: %s", code, stderr)
	}
	base := onlyRequestTo(t, stub, http.MethodPut, "/workspace/variables/API_KEY")
	if got := base.header.Get("X-Workspace-Id"); got != "ws_payments" {
		t.Errorf("X-Workspace-Id = %q, want ws_payments", got)
	}
	var body api.SetVariableRequest
	if err := json.Unmarshal(
		base.body,
		&body,
	); err != nil || body.Value != "sk-live" || body.Secret == nil ||
		!*body.Secret {
		t.Errorf("body = %s, want the value as a secret", base.body)
	}

	// without --workspace the header is absent, so the API uses the oldest workspace
	if _, stderr, code := runCLI(t, "workspace", "env", "set", "-e", "sandbox",
		"--var", "BASE_URL=https://sandbox.example.com"); code != 0 {
		t.Fatalf("set -e exited %d: %s", code, stderr)
	}
	environment := onlyRequestTo(t, stub, http.MethodPut, "/workspace/environments/sandbox/variables/BASE_URL")
	if _, sent := environment.header["X-Workspace-Id"]; sent {
		t.Errorf("X-Workspace-Id sent as %q without --workspace", environment.header.Get("X-Workspace-Id"))
	}
	var plain api.SetVariableRequest
	if err := json.Unmarshal(environment.body, &plain); err != nil || plain.Secret != nil {
		t.Errorf("body = %s, want a plain value", environment.body)
	}
}

func TestWorkspaceEnvUnsetRemovesFromTheBaseLayerOrAnEnvironment(t *testing.T) {
	stub := workspaceEnvStub(t)

	for _, args := range [][]string{
		{"workspace", "env", "unset", "API_KEY"},
		{"workspace", "env", "unset", "-e", "dev", "API_KEY"},
	} {
		if _, stderr, code := runCLI(t, args...); code != 0 {
			t.Fatalf("%v exited %d: %s", args, code, stderr)
		}
	}
	onlyRequestTo(t, stub, http.MethodDelete, "/workspace/variables/API_KEY")
	onlyRequestTo(t, stub, http.MethodDelete, "/workspace/environments/dev/variables/API_KEY")
}

func TestWorkspaceEnvViewHidesValuesAndNamesTheEnvironments(t *testing.T) {
	workspaceEnvStub(t)

	stdout, stderr, code := runCLI(t, "workspace", "env", "view", "-o", "json")
	if code != 0 {
		t.Fatalf("view exited %d: %s", code, stderr)
	}
	var got struct {
		Base         []string `json:"base"`
		Environments []string `json:"environments"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("view printed %q: %v", stdout, err)
	}
	if strings.Join(got.Base, ",") != "API_KEY,BASE_URL" || strings.Join(got.Environments, ",") != "dev,sandbox" {
		t.Errorf("view = %+v, want names only", got)
	}
	if strings.Contains(stdout, "api.example.com") {
		t.Errorf("view printed a value without --show-values: %s", stdout)
	}
}

func TestWorkspaceEnvironmentsListGivesEachEnvironmentItsScope(t *testing.T) {
	workspaceEnvStub(t)

	stdout, stderr, code := runCLI(t, "workspace", "env", "environments", "list", "-o", "json")
	if code != 0 {
		t.Fatalf("list exited %d: %s", code, stderr)
	}
	var rows []environmentRow
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
		t.Fatalf("list printed %q: %v", stdout, err)
	}
	want := []environmentRow{{Name: "dev", Scope: "organization"}, {Name: "sandbox", Scope: "workspace"}}
	if len(rows) != 2 || rows[0] != want[0] || rows[1] != want[1] {
		t.Errorf("rows = %+v, want %+v", rows, want)
	}

	table, _, _ := runCLI(t, "workspace", "env", "environments", "list")
	if !strings.Contains(table, "shared") || !strings.Contains(table, "workspace") {
		t.Errorf("table does not name the scopes:\n%s", table)
	}
}

func TestWorkspaceEnvironmentsCreateSendsTheName(t *testing.T) {
	stub := workspaceEnvStub(t)

	stdout, stderr, code := runCLI(t, "workspace", "env", "environments", "create", "sandbox")
	if code != 0 {
		t.Fatalf("create exited %d: %s", code, stderr)
	}
	request := onlyRequestTo(t, stub, http.MethodPost, "/workspace/environments")
	if strings.TrimSpace(string(request.body)) != `{"name":"sandbox"}` {
		t.Errorf("body = %s", request.body)
	}
	if !strings.Contains(stdout, `Created environment "sandbox"`) {
		t.Errorf("stdout = %q", stdout)
	}
}
