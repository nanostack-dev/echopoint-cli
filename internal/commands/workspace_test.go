package commands

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeWorkspaceServer serves /me with two organizations, the roles of a
// workspace, and records the role a member is given.
func fakeWorkspaceServer(t *testing.T, gotRole *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/me" && r.URL.Query().Get("include") == "workspace":
			_, _ = io.WriteString(w, `{"membership_status":"active","user":{"id":"u","organizations":[
				{"id":"org_other","name":"Other","role":{"id":"r","name":"admin","permissions":[]},
				 "workspaces":[{"id":"ws_foreign","name":"Foreign","permissions":["workspace.flows:read"],"role":null}]},
				{"id":"org_test","name":"Test","role":{"id":"r","name":"member","permissions":[]},
				 "workspaces":[
				  {"id":"ws_payments","name":"Payments","permissions":["workspace.flows:read"],
				   "role":{"id":"prole_viewer","name":"workspace_viewer"}},
				  {"id":"ws_platform","name":"Platform","permissions":["workspace.flows:read"],"role":null}]}]}}`)
		case r.URL.Path == "/workspaces/ws_payments/roles":
			_, _ = io.WriteString(w, `{"items":[
				{"id":"prole_editor","name":"workspace_editor","permissions":["workspace.flows:update"]},
				{"id":"prole_viewer","name":"workspace_viewer","permissions":["workspace.flows:read"]}]}`)
		case r.URL.Path == "/workspaces/ws_payments/members/pusr_bob" && r.Method == http.MethodPut:
			var body struct {
				RoleID string `json:"role_id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			*gotRole = body.RoleID
			_, _ = io.WriteString(w, `{"user_id":"pusr_bob","email":"bob@example.com",
				"role":{"id":"prole_editor","name":"workspace_editor"},"joined_at":"2026-11-01T00:00:00Z"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestWorkspaceListShowsOnlyTheCurrentOrganization(t *testing.T) {
	srv := fakeWorkspaceServer(t, new(string))
	defer srv.Close()
	state := makeState(t, "", "session-token", srv.URL)

	got, err := fetchWorkspaces(t.Context(), state)
	if err != nil {
		t.Fatalf("fetchWorkspaces: %v", err)
	}
	if len(got) != 2 || got[0].ID != "ws_payments" || got[1].ID != "ws_platform" {
		t.Fatalf("workspaces = %+v, want ws_payments and ws_platform", got)
	}
	if got[0].Role == nil || *got[0].Role != "workspace_viewer" {
		t.Errorf("role on Payments = %v, want workspace_viewer", got[0].Role)
	}
	if got[1].Role != nil {
		t.Errorf("Platform is reached through the organization role, got role %q", *got[1].Role)
	}
}

func TestWorkspaceListNeedsASession(t *testing.T) {
	srv := fakeWorkspaceServer(t, new(string))
	defer srv.Close()
	state := makeState(t, "api-key", "", srv.URL)

	_, err := fetchWorkspaces(t.Context(), state)
	if err == nil || !strings.Contains(err.Error(), "echopoint auth login") {
		t.Fatalf("err = %v, want a hint to sign in", err)
	}
}

func TestWorkspaceMemberSetTakesARoleName(t *testing.T) {
	var gotRole string
	srv := fakeWorkspaceServer(t, &gotRole)
	defer srv.Close()
	state := makeState(t, "", "session-token", srv.URL)
	cmd := newWorkspaceMemberSetCmd(state)
	cmd.SetArgs([]string{"ws_payments", "pusr_bob", "--role", "Workspace_Editor"})
	cmd.SetOut(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("member set: %v", err)
	}
	if gotRole != "prole_editor" {
		t.Errorf("role sent = %q, want prole_editor", gotRole)
	}
}

func TestWorkspaceMemberSetRefusesAnUnknownRole(t *testing.T) {
	srv := fakeWorkspaceServer(t, new(string))
	defer srv.Close()
	state := makeState(t, "", "session-token", srv.URL)

	_, err := resolveWorkspaceRole(t.Context(), state, "ws_payments", "owner")
	if err == nil || !strings.Contains(err.Error(), "workspace_editor, workspace_viewer") {
		t.Fatalf("err = %v, want the role names listed", err)
	}
}
