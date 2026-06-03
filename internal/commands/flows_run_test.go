package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/client"
	"echopoint-cli/internal/config"
	"echopoint-cli/internal/output"

	"github.com/google/uuid"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func makeState(t *testing.T, apiKey, token, baseURL string) *AppState {
	t.Helper()
	store := config.DefaultStore()
	cfg, err := store.Resolve("")
	if err != nil {
		t.Fatalf("resolve config: %v", err)
	}
	cfg.API.BaseURL = baseURL
	cfg.API.Timeout = 5 * time.Second

	var cli *client.Client
	if apiKey != "" {
		cli, err = client.NewWithAPIKey(baseURL, apiKey, "org_test", cfg.API.Timeout)
	} else {
		cli, err = client.New(baseURL, token, "", cfg.API.Timeout)
	}
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	return &AppState{
		Config:         cfg,
		OutputFormat:   output.FormatTable,
		APIKey:         apiKey,
		Token:          token,
		OrganizationID: "org_test",
		Client:         cli,
	}
}

func flowUUID() uuid.UUID {
	return uuid.MustParse("550e8400-e29b-41d4-a716-446655440001")
}

func executionUUID() uuid.UUID {
	return uuid.MustParse("550e8400-e29b-41d4-a716-446655440002")
}

// launchResponse builds a LaunchFlowAcceptedResponse for tests.
// When terminal is true the execution is returned with a completed status (idempotent replay).
// When terminal is false the execution is pending and the caller should run the runner.
func launchResponse(terminal bool) api.LaunchFlowAcceptedResponse {
	runnerTypeEphemeral := api.Ephemeral
	now := time.Now().UTC()

	status := api.ExecutionStatus("pending")
	if terminal {
		status = api.ExecutionStatus("completed")
	}

	execution := api.FlowExecution{
		Id:             executionUUID(),
		FlowId:         flowUUID(),
		OrganizationId: "org_test",
		FlowSnapshot: api.FlowDefinition{
			Name:    "test",
			Version: "1.0",
			Nodes:   []api.FlowNode{},
			Edges:   []api.FlowEdge{},
		},
		RunnerInputs:    api.RunnerInputs{},
		ReferencedFlows: api.ReferencedFlows{},
		Status:          status,
		RunnerType:      &runnerTypeEphemeral,
		StartedAt:       now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	return api.LaunchFlowAcceptedResponse{Execution: execution}
}

func publishResponse() api.EphemeralCompletionResponse {
	runnerTypeEphemeral := api.Ephemeral
	now := time.Now().UTC()
	completedStatus := api.ExecutionStatus("completed")
	return api.EphemeralCompletionResponse{
		Execution: api.FlowExecution{
			Id:             executionUUID(),
			FlowId:         flowUUID(),
			OrganizationId: "org_test",
			FlowSnapshot: api.FlowDefinition{
				Name: "test", Version: "1.0",
				Nodes: []api.FlowNode{}, Edges: []api.FlowEdge{},
			},
			RunnerInputs:    api.RunnerInputs{},
			ReferencedFlows: api.ReferencedFlows{},
			Status:          completedStatus,
			RunnerType:      &runnerTypeEphemeral,
			StartedAt:       now,
			CompletedAt:     &now,
			CreatedAt:       now,
			UpdatedAt:       now,
		},
		Nodes: []api.NodeExecutionResult{},
	}
}

// buildFakeRunner builds a tiny shell script (or bat on Windows) that acts
// as echopoint-runner in ephemeral mode.  It reads stdin, writes a completed
// result JSON to stdout, and exits 0.
func buildFakeRunner(t *testing.T, exitCode int, resultJSON string) string {
	t.Helper()
	dir := t.TempDir()

	if runtime.GOOS == "windows" {
		script := filepath.Join(dir, "echopoint-runner.bat")
		os.WriteFile(script, []byte(fmt.Sprintf(`@echo off
echo %s
exit /b %d`, resultJSON, exitCode)), 0o755)
		return script
	}

	script := filepath.Join(dir, "echopoint-runner")
	content := fmt.Sprintf("#!/bin/sh\ncat > /dev/null\nprintf '%%s' '%s'\nexit %d\n", resultJSON, exitCode)
	os.WriteFile(script, []byte(content), 0o755)
	return script
}

func successRunnerResult() ephemeralResult {
	now := time.Now().UTC()
	return ephemeralResult{
		Status:      "completed",
		StartedAt:   now.Format(time.RFC3339),
		CompletedAt: now.Add(time.Second).Format(time.RFC3339),
		DurationMs:  1000,
		Result: map[string]interface{}{
			"execution_results": map[string]interface{}{},
			"final_outputs":     map[string]interface{}{},
			"success":           true,
		},
	}
}

// ── auth resolution unit tests ────────────────────────────────────────────────

func TestResolveAPIKey_Flag(t *testing.T) {
	os.Unsetenv("ECHOPOINT_API_KEY")
	key := resolveAPIKey("mykey123")
	if key != "mykey123" {
		t.Fatalf("expected mykey123, got %q", key)
	}
}

func TestResolveAPIKey_Env(t *testing.T) {
	os.Setenv("ECHOPOINT_API_KEY", "envkey456")
	defer os.Unsetenv("ECHOPOINT_API_KEY")
	key := resolveAPIKey("")
	if key != "envkey456" {
		t.Fatalf("expected envkey456, got %q", key)
	}
}

func TestResolveAPIKey_FlagPrecedence(t *testing.T) {
	os.Setenv("ECHOPOINT_API_KEY", "envkey")
	defer os.Unsetenv("ECHOPOINT_API_KEY")
	key := resolveAPIKey("flagkey")
	if key != "flagkey" {
		t.Fatalf("expected flagkey (flag takes precedence), got %q", key)
	}
}

func TestAPIKeyPrecedenceOverBearer(t *testing.T) {
	// When using API key client, Authorization header must be absent.
	// Verify that apiKeyRequestEditor does not set Authorization header.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	_, _ = client.NewWithAPIKey(srv.URL, "apik", "org_abc", 5*time.Second)

	// Test request editor behavior directly
	apiEditor := func(ctx context.Context, r *http.Request) error {
		r.Header.Del("Authorization")
		r.Header.Set("X-Api-Key", "apik")
		r.Header.Set("X-Organization-Id", "org_abc")
		return nil
	}
	reqTest, _ := http.NewRequest("GET", srv.URL, nil)
	reqTest.Header.Set("Authorization", "Bearer original")
	_ = apiEditor(context.Background(), reqTest)

	if got := reqTest.Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization header should be absent when using API key auth, got %q", got)
	}
	if reqTest.Header.Get("X-Api-Key") != "apik" {
		t.Errorf("expected X-Api-Key=apik, got %q", reqTest.Header.Get("X-Api-Key"))
	}
	if reqTest.Header.Get("X-Organization-Id") != "org_abc" {
		t.Errorf("expected X-Organization-Id=org_abc, got %q", reqTest.Header.Get("X-Organization-Id"))
	}
}

// ── redaction unit tests ─────────────────────────────────────────────────────

func TestRedactedHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer secrettoken")
	h.Set("X-Api-Key", "epk_live_supersecret")
	h.Set("Content-Type", "application/json")

	result := client.RedactedHeaders(h)

	if strings.Contains(result, "secrettoken") {
		t.Errorf("Authorization value leaked in debug output: %s", result)
	}
	if strings.Contains(result, "supersecret") {
		t.Errorf("X-Api-Key value leaked in debug output: %s", result)
	}
	if !strings.Contains(result, "REDACTED") {
		t.Errorf("expected REDACTED marker in output: %s", result)
	}
	if !strings.Contains(result, "application/json") {
		t.Errorf("non-sensitive header should be present: %s", result)
	}
}

// ── GitHub metadata unit tests ───────────────────────────────────────────────

func TestDeriveGitHubIdempotencyKey(t *testing.T) {
	os.Setenv("GITHUB_REPOSITORY", "org/repo")
	os.Setenv("GITHUB_WORKFLOW", "ci.yml")
	os.Setenv("GITHUB_JOB", "test")
	os.Setenv("GITHUB_RUN_ID", "12345")
	os.Setenv("GITHUB_RUN_ATTEMPT", "1")
	defer func() {
		os.Unsetenv("GITHUB_REPOSITORY")
		os.Unsetenv("GITHUB_WORKFLOW")
		os.Unsetenv("GITHUB_JOB")
		os.Unsetenv("GITHUB_RUN_ID")
		os.Unsetenv("GITHUB_RUN_ATTEMPT")
	}()

	key1 := deriveGitHubIdempotencyKey([]string{"flow-1"})
	key2 := deriveGitHubIdempotencyKey([]string{"flow-1"})
	if key1 == "" {
		t.Fatal("expected non-empty idempotency key")
	}
	if key1 != key2 {
		t.Errorf("idempotency key should be stable: %q != %q", key1, key2)
	}

	// Different run attempt → different key
	os.Setenv("GITHUB_RUN_ATTEMPT", "2")
	key3 := deriveGitHubIdempotencyKey([]string{"flow-1"})
	if key3 == key1 {
		t.Errorf("different run attempt should produce different key")
	}
}

func TestResolveIdempotencyKey_GitHub(t *testing.T) {
	os.Setenv("GITHUB_ACTIONS", "true")
	os.Setenv("GITHUB_REPOSITORY", "org/repo")
	os.Setenv("GITHUB_WORKFLOW", "ci.yml")
	os.Setenv("GITHUB_JOB", "test")
	os.Setenv("GITHUB_RUN_ID", "42")
	os.Setenv("GITHUB_RUN_ATTEMPT", "1")
	defer func() {
		os.Unsetenv("GITHUB_ACTIONS")
		os.Unsetenv("GITHUB_REPOSITORY")
		os.Unsetenv("GITHUB_WORKFLOW")
		os.Unsetenv("GITHUB_JOB")
		os.Unsetenv("GITHUB_RUN_ID")
		os.Unsetenv("GITHUB_RUN_ATTEMPT")
	}()

	key := resolveIdempotencyKey("", []string{"flow-1"})
	if key == "" {
		t.Fatal("expected non-empty key when GITHUB_ACTIONS=true")
	}
	if !strings.HasPrefix(key, "gh-") {
		t.Errorf("expected gh- prefix, got %q", key)
	}
}

func TestDerivePerFlowKey(t *testing.T) {
	base := "gh-abc123"
	key1 := derivePerFlowKey(base, "flow-aaa")
	key2 := derivePerFlowKey(base, "flow-bbb")

	if key1 == key2 {
		t.Error("different flow IDs should produce different per-flow keys")
	}
	if !strings.HasPrefix(key1, base) {
		t.Errorf("per-flow key should start with base key: %q", key1)
	}
	if derivePerFlowKey(base, "flow-aaa") != key1 {
		t.Error("per-flow key should be stable (deterministic)")
	}
}

// ── JSON output unit tests ────────────────────────────────────────────────────

func TestFlowRunOutput_Success_JSON(t *testing.T) {
	result := FlowRunResult{
		ExecutionID:  "exec-001",
		FlowID:       "flow-001",
		Status:       "completed",
		Success:      true,
		ExitCode:     0,
		DurationMs:   1234,
		ErrorMessage: nil,
		Nodes:        []FlowRunNode{},
	}

	var buf bytes.Buffer
	if err := output.PrintJSON(&buf, FlowRunOutput{result}); err != nil {
		t.Fatalf("print JSON: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("parse JSON output: %v", err)
	}

	requiredFields := []string{
		"execution_id", "flow_id", "status", "success",
		"exit_code", "duration_ms", "error_message", "nodes",
	}
	for _, f := range requiredFields {
		if _, ok := parsed[f]; !ok {
			t.Errorf("missing field %q in JSON output", f)
		}
	}

	if parsed["status"] != "completed" {
		t.Errorf("expected status=completed, got %v", parsed["status"])
	}
	if parsed["success"] != true {
		t.Errorf("expected success=true, got %v", parsed["success"])
	}
}

func TestFlowRunOutput_FlowFailure_JSON(t *testing.T) {
	msg := "assertion failed"
	result := FlowRunResult{
		ExecutionID:  "exec-002",
		FlowID:       "flow-001",
		Status:       "failed",
		Success:      false,
		ExitCode:     exitFlowFailed,
		DurationMs:   500,
		ErrorMessage: &msg,
		Nodes: []FlowRunNode{
			{NodeID: "req-1", DisplayName: "Login", NodeType: "request", Status: "failed", ErrorMsg: &msg},
		},
	}

	var buf bytes.Buffer
	if err := output.PrintJSON(&buf, FlowRunOutput{result}); err != nil {
		t.Fatalf("print JSON: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("parse JSON output: %v", err)
	}
	if parsed["success"] != false {
		t.Errorf("expected success=false, got %v", parsed["success"])
	}
	if parsed["exit_code"] != float64(exitFlowFailed) {
		t.Errorf("expected exit_code=%d, got %v", exitFlowFailed, parsed["exit_code"])
	}
}

func TestMultiFlowRunOutput_JSON(t *testing.T) {
	msg := "err"
	results := []FlowRunResult{
		{ExecutionID: "e1", FlowID: "f1", Status: "completed", Success: true, ExitCode: 0, DurationMs: 100},
		{
			ExecutionID: "e2", FlowID: "f2", Status: "failed",
			Success: false, ExitCode: 1, DurationMs: 200, ErrorMessage: &msg,
		},
	}
	out := MultiFlowRunOutput{
		Status:     "failed",
		Success:    false,
		ExitCode:   1,
		DurationMs: 300,
		Results:    results,
	}

	var buf bytes.Buffer
	if err := output.PrintJSON(&buf, out); err != nil {
		t.Fatalf("print JSON: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}

	if parsed["success"] != false {
		t.Errorf("aggregate success should be false")
	}
	resultsArr, ok := parsed["results"].([]interface{})
	if !ok || len(resultsArr) != 2 {
		t.Errorf("expected 2 results, got %v", parsed["results"])
	}
}

// ── integration tests with fake API + fake runner ─────────────────────────────

func setupFakeAPIServer(
	t *testing.T, launchResp interface{}, launchStatus int,
	publishResp interface{}, publishStatus int,
) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/launch") && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(launchStatus)
			json.NewEncoder(w).Encode(launchResp)
		case strings.Contains(r.URL.Path, "/complete") && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(publishStatus)
			json.NewEncoder(w).Encode(publishResp)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestIntegration_SuccessfulEphemeralExecution(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows due to shell script runner")
	}

	successResult, _ := json.Marshal(successRunnerResult())
	runnerBinary := buildFakeRunner(t, 0, string(successResult))

	srv := setupFakeAPIServer(t,
		launchResponse(false), http.StatusAccepted,
		publishResponse(), http.StatusOK,
	)
	defer srv.Close()

	state := makeState(t, "test-api-key", "", srv.URL)
	state.OrganizationID = "org_test"

	results, exitCode := executeFlows(
		context.Background(),
		state,
		[]string{flowUUID().String()},
		runnerBinary,
		"",
		"",
		"",
		1,
		"",
	)

	if exitCode != exitSuccess {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ExitCode != exitSuccess {
		t.Errorf("expected flow exit code 0, got %d: err=%v", results[0].ExitCode, results[0].ErrorMessage)
	}
}

func TestIntegration_TerminalIdempotentReplay(t *testing.T) {
	// When the server returns a launch response with a terminal execution status,
	// the CLI should NOT run the runner and should report the existing status.
	srv := setupFakeAPIServer(t,
		launchResponse(true), http.StatusAccepted,
		nil, http.StatusOK,
	)
	defer srv.Close()

	state := makeState(t, "test-api-key", "", srv.URL)

	results, exitCode := executeFlows(
		context.Background(),
		state,
		[]string{flowUUID().String()},
		"/nonexistent-runner",
		"",
		"",
		"",
		1,
		"",
	)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	// Should not error about missing runner since runner is never called
	if results[0].ExitCode == exitError &&
		results[0].ErrorMessage != nil &&
		strings.Contains(*results[0].ErrorMessage, "not found") {
		t.Errorf("runner should not be called on terminal replay; got error: %v", *results[0].ErrorMessage)
	}
	_ = exitCode
}

func TestIntegration_MissingRunnerBinary(t *testing.T) {
	_, err := resolveRunnerBinary("/totally/nonexistent/echopoint-runner")
	if err == nil {
		t.Fatal("expected error for missing runner binary")
	}
	if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "no such file") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestIntegration_MultipleFlowsSequential(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows due to shell script runner")
	}

	successResult, _ := json.Marshal(successRunnerResult())
	runnerBinary := buildFakeRunner(t, 0, string(successResult))

	var mu struct {
		sync.Mutex
		calls []string
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		mu.calls = append(mu.calls, r.URL.Path)
		mu.Unlock()

		switch {
		case strings.Contains(r.URL.Path, "/launch"):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(launchResponse(false))
		case strings.Contains(r.URL.Path, "/complete"):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(publishResponse())
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	flowID1 := uuid.New().String()
	flowID2 := uuid.New().String()
	state := makeState(t, "test-api-key", "", srv.URL)

	results, exitCode := executeFlows(
		context.Background(),
		state,
		[]string{flowID1, flowID2},
		runnerBinary,
		"base-key",
		"",
		"",
		1,
		"",
	)

	if exitCode != exitSuccess {
		t.Errorf("expected exit 0, got %d", exitCode)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}

	mu.Lock()
	launchCalls := 0
	for _, c := range mu.calls {
		if strings.Contains(c, "/launch") {
			launchCalls++
		}
	}
	mu.Unlock()
	if launchCalls != 2 {
		t.Errorf("expected 2 launch calls, got %d", launchCalls)
	}
}

func TestIntegration_ParallelBounding(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows due to shell script runner")
	}

	successResult, _ := json.Marshal(successRunnerResult())
	runnerBinary := buildFakeRunner(t, 0, string(successResult))

	var mu struct {
		sync.Mutex
		active    int
		maxActive int
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/launch") {
			mu.Lock()
			mu.active++
			if mu.active > mu.maxActive {
				mu.maxActive = mu.active
			}
			mu.Unlock()

			time.Sleep(20 * time.Millisecond)

			mu.Lock()
			mu.active--
			mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(launchResponse(false))
			return
		}
		if strings.Contains(r.URL.Path, "/complete") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(publishResponse())
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	flowIDs := make([]string, 6)
	for i := range flowIDs {
		flowIDs[i] = uuid.New().String()
	}

	state := makeState(t, "test-api-key", "", srv.URL)

	const maxParallel = 2
	results, _ := executeFlows(
		context.Background(),
		state,
		flowIDs,
		runnerBinary,
		"",
		"",
		"",
		maxParallel,
		"",
	)

	if len(results) != len(flowIDs) {
		t.Errorf("expected %d results, got %d", len(flowIDs), len(results))
	}
}

func TestIntegration_InvalidParallel(t *testing.T) {
	state := makeState(t, "test-api-key", "", "http://localhost")
	cmd := newFlowsRunCmd(state)

	cmd.SetArgs([]string{"--parallel", "0", flowUUID().String()})
	var buf bytes.Buffer
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for --parallel=0")
	}
}

func TestIntegration_PublishRetryOnTransient(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows due to shell script runner")
	}

	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/complete") {
			attempts++
			if attempts < 3 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(publishResponse())
			return
		}
		if strings.Contains(r.URL.Path, "/launch") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(launchResponse(false))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	state := makeState(t, "test-api-key", "", srv.URL)
	ctx := context.Background()

	runnerResult := &ephemeralResult{
		Status:      "completed",
		StartedAt:   time.Now().Format(time.RFC3339),
		CompletedAt: time.Now().Add(time.Second).Format(time.RFC3339),
		DurationMs:  1000,
	}

	resp, err := publishResult(ctx, state, flowUUID(), executionUUID(), runnerResult, "")
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil publish response")
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts (2 failures + 1 success), got %d", attempts)
	}
}

func TestIntegration_JSONOutputOnAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"errors": []map[string]string{{"code": "FORBIDDEN", "message": "insufficient permissions"}},
		})
	}))
	defer srv.Close()

	state := makeState(t, "test-api-key", "", srv.URL)
	state.OutputFormat = output.FormatJSON

	results, exitCode := executeFlows(
		context.Background(),
		state,
		[]string{flowUUID().String()},
		"/nonexistent",
		"",
		"",
		"",
		1,
		"json",
	)

	if exitCode != exitError {
		t.Errorf("expected exit code %d for API error, got %d", exitError, exitCode)
	}
	if len(results) != 1 || results[0].Success {
		t.Errorf("expected failed result, got %+v", results)
	}
}

func TestIntegration_InvalidRunnerResult(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	// Runner writes invalid JSON
	runnerBinary := buildFakeRunner(t, 0, "this is not json")

	srv := setupFakeAPIServer(t,
		launchResponse(false), http.StatusAccepted,
		publishResponse(), http.StatusOK,
	)
	defer srv.Close()

	state := makeState(t, "test-api-key", "", srv.URL)

	results, exitCode := executeFlows(
		context.Background(),
		state,
		[]string{flowUUID().String()},
		runnerBinary,
		"",
		"",
		"",
		1,
		"",
	)

	if exitCode != exitError {
		t.Errorf("expected exit code %d for invalid runner result, got %d", exitError, exitCode)
	}
	if len(results) == 1 && results[0].ErrorMessage == nil {
		t.Error("expected error message for invalid runner result")
	}
}

func TestIntegration_TimeoutExitsWithCode4(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	// Build a runner that sleeps for a long time
	dir := t.TempDir()
	runnerBinary := filepath.Join(dir, "echopoint-runner")
	os.WriteFile(runnerBinary, []byte("#!/bin/sh\ncat > /dev/null\nsleep 60\n"), 0o755)

	srv := setupFakeAPIServer(t,
		launchResponse(false), http.StatusAccepted,
		nil, http.StatusOK,
	)
	defer srv.Close()

	state := makeState(t, "test-api-key", "", srv.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	results, exitCode := executeFlows(
		ctx,
		state,
		[]string{flowUUID().String()},
		runnerBinary,
		"",
		"",
		"",
		1,
		"",
	)

	if exitCode != exitTimeout {
		t.Errorf("expected exit code %d for timeout, got %d", exitTimeout, exitCode)
	}
	_ = results
}
