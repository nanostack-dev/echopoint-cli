package commands

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestAdministrationCommandsAreUnavailableWithoutRequests(t *testing.T) {
	stub := newAPIStub(t).onFlows()
	isolateCLIEnvironment(t, stub.server.URL)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"admin", "cloud-fleet", "view"}, `unknown command "admin"`},
		{[]string{"admin", "cloud-fleet", "update", "--daily-launch-limit", "0", "--global-cap", "1", "--expected-revision", "0"}, `unknown command "admin"`},
		{[]string{"auth", "login", "--admin"}, "unknown flag: --admin"},
		{[]string{"auth", "login", "--admin", "--api-key", "test-key"}, "unknown flag: --admin"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			_, stderr, code := runCLI(t, tc.args...)
			if code == 0 || !strings.Contains(stderr, tc.want) {
				t.Fatalf("exit %d, stderr %q; want rejection %q", code, stderr, tc.want)
			}
		})
	}
	for _, args := range [][]string{{"--help"}, {"auth", "login", "--help"}} {
		stdout, stderr, code := runCLI(t, args...)
		if code != 0 {
			t.Fatalf("help exit %d: %s", code, stderr)
		}
		if strings.Contains(stdout, "--admin") || strings.Contains(stdout, "cloud-fleet") ||
			strings.Contains(stdout, "  admin ") {
			t.Errorf("help still exposes product administration: %s", stdout)
		}
	}
	if stub.requestCount() != 0 {
		t.Fatalf("unsupported commands sent %d HTTP requests", stub.requestCount())
	}

	// An ordinary public command remains usable with the same selected profile.
	stdout, stderr, code := runCLI(t, "flow", "list", "--profile", "default", "-o", "json")
	if code != 0 || !json.Valid([]byte(stdout)) {
		t.Fatalf("ordinary flow list failed: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if len(stub.requestsTo(http.MethodPost, flowSearchPath)) != 1 || stub.requestCount() != 1 {
		t.Fatal("ordinary flow list did not make exactly its expected request")
	}
}
