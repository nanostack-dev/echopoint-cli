package commands

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"echopoint-cli/internal/api"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
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
	if len(stub.requestsTo(http.MethodGet, "/flows")) != 1 || stub.requestCount() != 1 {
		t.Fatal("ordinary flow list did not make exactly its expected request")
	}
}

func TestMCPProcessExcludesAdministrationAndServesOrdinaryTools(t *testing.T) {
	stub := newAPIStub(t).onFlows()
	isolateCLIEnvironment(t, stub.server.URL)
	command := exec.Command(echopointBinary(t), "mcp", "--profile", "default")
	command.Stderr = io.Discard
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "administration-exclusion-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcpsdk.CommandTransport{Command: command}, nil)
	if err != nil {
		t.Fatalf("connect to CLI stdio MCP process: %v", err)
	}
	defer session.Close()

	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	excluded := map[string]bool{
		"get_cloud_fleet": true, "update_cloud_fleet": true,
		"resync_resource_search": true, "run_flow_schedule_now": true,
	}
	ordinaryToolFound := false
	for _, tool := range listed.Tools {
		if excluded[tool.Name] {
			t.Errorf("administration tool %q was listed", tool.Name)
		}
		ordinaryToolFound = ordinaryToolFound || tool.Name == "list_flows"
	}
	if !ordinaryToolFound {
		t.Fatal("ordinary list_flows tool is missing")
	}
	for name := range excluded {
		_, err := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: name, Arguments: map[string]any{}})
		if err == nil || !strings.Contains(err.Error(), `unknown tool "`+name+`"`) {
			t.Errorf("%s: expected unknown-tool rejection, got %v", name, err)
		}
	}
	if stub.requestCount() != 0 {
		t.Fatalf("MCP discovery/admin calls sent %d HTTP requests", stub.requestCount())
	}

	result, err := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: "list_flows", Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatalf("ordinary list_flows call failed: result=%v, error=%v", result, err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected one ordinary response content block, got %d", len(result.Content))
	}
	text, ok := result.Content[0].(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("expected JSON text, got %T", result.Content[0])
	}
	var response api.FlowListResponse
	if err := json.Unmarshal([]byte(text.Text), &response); err != nil {
		t.Fatalf("ordinary response is not a flow list: %v", err)
	}
	requests := stub.requestsTo(http.MethodGet, "/flows")
	if len(requests) != 1 || stub.requestCount() != 1 || requests[0].header.Get("X-Api-Key") != "golden-key" {
		t.Fatal("ordinary MCP tool did not preserve the authenticated API request")
	}
}
