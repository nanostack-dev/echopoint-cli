package commands

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func deadServerURL(t *testing.T) string {
	t.Helper()
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()
	return dead.URL
}

func flowListStub(t *testing.T) *apiStub {
	t.Helper()
	return newAPIStub(t).onFlows(sampleFlow(idFor("1"), "Checkout smoke", nil))
}

func writeCLIConfig(t *testing.T, content string) {
	t.Helper()
	if err := os.WriteFile(os.Getenv("ECHOPOINT_CONFIG"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAnAPIURLFlagBeatsTheEnvironmentVariable(t *testing.T) {
	stub := flowListStub(t)
	isolateCLIEnvironment(t, deadServerURL(t))

	_, stderr, code := runCLI(t, "flow", "list", "--api-url", stub.server.URL)

	if code != 0 || len(stub.requestsTo(http.MethodGet, "/flows")) != 1 {
		t.Errorf("exit %d, %d requests to the flag's server, stderr %q",
			code, len(stub.requestsTo(http.MethodGet, "/flows")), stderr)
	}
}

func TestAnAPIURLEnvironmentVariableBeatsTheConfigAndTheConfigStillApplies(t *testing.T) {
	stub := flowListStub(t)
	isolateCLIEnvironment(t, stub.server.URL)
	writeCLIConfig(t, "current_profile: qa\nprofiles:\n  qa:\n    api_base_url: "+deadServerURL(t)+"\n")

	if _, stderr, code := runCLI(t, "flow", "list"); code != 0 || len(stub.requestsTo(http.MethodGet, "/flows")) != 1 {
		t.Errorf("the environment variable lost to the config: exit %d, stderr %q", code, stderr)
	}

	t.Setenv("ECHOPOINT_API_URL", "")
	writeCLIConfig(t, "current_profile: qa\nprofiles:\n  qa:\n    api_base_url: "+stub.server.URL+"\n")
	if _, stderr, code := runCLI(t, "flow", "list"); code != 0 || len(stub.requestsTo(http.MethodGet, "/flows")) != 2 {
		t.Errorf("the config alone was not used: exit %d, stderr %q", code, stderr)
	}
}

func TestAnOutputFlagBeatsTheEnvironmentVariableWhichBeatsTheConfig(t *testing.T) {
	stub := flowListStub(t)
	isolateCLIEnvironment(t, stub.server.URL)
	writeCLIConfig(t, "defaults:\n  output_format: json\n")
	isJSON := func(out string) bool { return strings.HasPrefix(strings.TrimSpace(out), "{") }
	isYAML := func(out string) bool { return strings.HasPrefix(strings.TrimSpace(out), "count:") }
	isTable := func(out string) bool { return strings.Contains(out, "UPDATED") }

	for name, tc := range map[string]struct {
		env  string
		args []string
		is   func(string) bool
	}{
		"config alone":       {"", nil, isJSON},
		"env over config":    {"yaml", nil, isYAML},
		"flag over config":   {"", []string{"-o", "table"}, isTable},
		"flag over env":      {"yaml", []string{"-o", "json"}, isJSON},
		"flag over env, too": {"json", []string{"--output", "table"}, isTable},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("ECHOPOINT_OUTPUT_FORMAT", tc.env)

			stdout, stderr, code := runCLI(t, append([]string{"flow", "list"}, tc.args...)...)

			if code != 0 || !tc.is(stdout) {
				t.Errorf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
			}
		})
	}
}

// The Action passes the API URL only through ECHOPOINT_API_URL, and asks for JSON
// with -o json: both keep working, whatever else the environment holds.
func TestTheActionsURLAndOutputStillApply(t *testing.T) {
	fake := newGoldenFlowServer(t, nil)
	isolateCLIEnvironment(t, fake.server.URL)
	t.Setenv("ECHOPOINT_OUTPUT_FORMAT", "yaml")
	writeCLIConfig(t, "defaults:\n  output_format: table\n")

	stdout, _, code := runCLI(t, "flows", "run", goldenReplayFlow, "-o", "json")

	if code != 0 || !strings.HasPrefix(stdout, "{\n  \"execution_id\"") {
		t.Errorf("exit %d, stdout %q", code, stdout)
	}
}
