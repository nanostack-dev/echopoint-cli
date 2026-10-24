package commands

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"echopoint-cli/internal/auth"
	"echopoint-cli/internal/config"
)

// signInWithoutOrganization stores a live session on the default profile with no
// organization, as a login whose /me lookup found none leaves it.
func signInWithoutOrganization(t *testing.T, apiURL string) {
	t.Helper()
	isolateCLIEnvironment(t, apiURL)
	t.Setenv("ECHOPOINT_API_KEY", "")
	t.Setenv("ECHOPOINT_ORGANIZATION_ID", "")
	expiresAt := time.Now().Add(time.Hour)
	if _, err := auth.SaveCredentials(config.DefaultProfile, auth.Credentials{
		AccessToken: "session-token", ExpiresAt: &expiresAt,
	}); err != nil {
		t.Fatal(err)
	}
}

func meWith(organizations ...map[string]string) map[string]any {
	return map[string]any{"user": map[string]any{"organizations": organizations}}
}

func storedOrganizationID(t *testing.T) string {
	t.Helper()
	creds, _, err := auth.LoadCredentials(config.DefaultProfile)
	if err != nil || creds == nil {
		t.Fatalf("load credentials: %v", err)
	}
	return creds.OrganizationID
}

func TestASessionStoredWithoutAnOrganizationUsesTheUsersOnlyOneAndSavesIt(t *testing.T) {
	stub := flowListStub(t).on(http.MethodGet, "/me", http.StatusOK,
		meWith(map[string]string{"id": "org_only", "name": "Default Organization"}))
	signInWithoutOrganization(t, stub.server.URL)

	if _, stderr, code := runCLI(t, "flow", "list"); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if _, stderr, code := runCLI(t, "flow", "list"); code != 0 {
		t.Fatalf("second run: exit %d, stderr %q", code, stderr)
	}

	for _, request := range stub.requestsTo(http.MethodPost, flowSearchPath) {
		if got := request.header.Get("X-Organization-Id"); got != "org_only" {
			t.Errorf("flow search sent X-Organization-Id %q, want org_only", got)
		}
	}
	if got := storedOrganizationID(t); got != "org_only" {
		t.Errorf("profile stores organization %q, want org_only", got)
	}
	if calls := len(stub.requestsTo(http.MethodGet, "/me")); calls != 1 {
		t.Errorf("/me called %d times, want once: the saved organization skips the lookup", calls)
	}
}

func TestASessionInSeveralOrganizationsPicksNoneAndNamesThem(t *testing.T) {
	stub := flowListStub(t).on(http.MethodGet, "/me", http.StatusOK, meWith(
		map[string]string{"id": "org_a", "name": "Acme"},
		map[string]string{"id": "org_b", "name": "Globex"},
	))
	signInWithoutOrganization(t, stub.server.URL)

	_, stderr, _ := runCLI(t, "flow", "list")

	if !strings.Contains(stderr, "Acme (org_a)") || !strings.Contains(stderr, "Globex (org_b)") ||
		!strings.Contains(stderr, "--org") {
		t.Errorf("stderr %q does not name both organizations and --org", stderr)
	}
	for _, request := range stub.requestsTo(http.MethodPost, flowSearchPath) {
		if got := request.header.Get("X-Organization-Id"); got != "" {
			t.Errorf("flow search sent X-Organization-Id %q, want none", got)
		}
	}
	if got := storedOrganizationID(t); got != "" {
		t.Errorf("profile stores organization %q, want none", got)
	}
}

func TestAnOrganizationFlagSkipsTheLookup(t *testing.T) {
	stub := flowListStub(t).on(http.MethodGet, "/me", http.StatusOK,
		meWith(map[string]string{"id": "org_only", "name": "Default Organization"}))
	signInWithoutOrganization(t, stub.server.URL)

	if _, stderr, code := runCLI(t, "flow", "list", "--org", "org_flag"); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}

	if calls := len(stub.requestsTo(http.MethodGet, "/me")); calls != 0 {
		t.Errorf("/me called %d times, want none", calls)
	}
	if got := storedOrganizationID(t); got != "" {
		t.Errorf("profile stores organization %q, want none", got)
	}
}
