package commands

import "testing"

func TestWorkspaceFlagOverridesEnvironment(t *testing.T) {
	t.Setenv("ECHOPOINT_WORKSPACE_ID", "ws_environment")
	if got := resolveWorkspaceID(" ws_flag "); got != "ws_flag" {
		t.Fatalf("flag selection = %q", got)
	}
	if got := resolveWorkspaceID(""); got != "ws_environment" {
		t.Fatalf("environment selection = %q", got)
	}
	t.Setenv("ECHOPOINT_WORKSPACE_ID", "")
	if got := resolveWorkspaceID(""); got != "" {
		t.Fatalf("unexpected implicit selection = %q", got)
	}
}
