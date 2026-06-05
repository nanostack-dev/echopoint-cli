package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"

	"echopoint-cli/internal/api"

	"github.com/google/uuid"
)

// searchListResponse builds a minimal FlowSearchResponse for /flows/search mocks.
// Only flow IDs and the total matter for tag resolution.
func searchListResponse(flowIDs []string, total int64) api.FlowSearchResponse {
	items := make([]api.FlowSearchItem, 0, len(flowIDs))
	for _, id := range flowIDs {
		items = append(items, api.FlowSearchItem{Id: uuid.MustParse(id), Tags: []string{}})
	}
	return api.FlowSearchResponse{Count: len(items), Items: items, Total: total}
}

// fakeSearchServer serves POST /flows/search with the given response and status.
func fakeSearchServer(t *testing.T, resp api.FlowSearchResponse, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/flows/search") && r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

func TestResolveFlowIDsByTags_Success(t *testing.T) {
	ids := []string{
		"550e8400-e29b-41d4-a716-446655440010",
		"550e8400-e29b-41d4-a716-446655440011",
	}
	srv := fakeSearchServer(t, searchListResponse(ids, int64(len(ids))), http.StatusOK)
	defer srv.Close()

	state := makeState(t, "test-api-key", "", srv.URL)

	got, err := resolveFlowIDsByTags(context.Background(), state, []string{"prod"}, "any")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 resolved IDs, got %d: %v", len(got), got)
	}
	for i, want := range ids {
		if got[i] != want {
			t.Errorf("resolved ID %d = %q, want %q", i, got[i], want)
		}
	}
}

func TestResolveFlowIDsByTags_NoMatches(t *testing.T) {
	srv := fakeSearchServer(t, searchListResponse(nil, 0), http.StatusOK)
	defer srv.Close()

	state := makeState(t, "test-api-key", "", srv.URL)

	_, err := resolveFlowIDsByTags(context.Background(), state, []string{"nonexistent"}, "any")
	if err == nil {
		t.Fatal("expected error for zero matches")
	}
	if !strings.Contains(err.Error(), "no flows matched") {
		t.Errorf("error = %q, want it to mention no matches", err.Error())
	}
}

func TestResolveFlowIDsByTags_TooManyMatches(t *testing.T) {
	// Total exceeds the safety cap; items can be empty since the cap check uses Total.
	srv := fakeSearchServer(t, searchListResponse(nil, int64(maxTagResolvedFlows+1)), http.StatusOK)
	defer srv.Close()

	state := makeState(t, "test-api-key", "", srv.URL)

	_, err := resolveFlowIDsByTags(context.Background(), state, []string{"prod"}, "any")
	if err == nil {
		t.Fatal("expected error when matches exceed the safety cap")
	}
	if !strings.Contains(err.Error(), "safety cap") {
		t.Errorf("error = %q, want it to mention the safety cap", err.Error())
	}
}

// exitCodeOf extracts the exit code from the *exitCodeError that flows-run commands
// always return (emitOutput/runError carry the code via this sentinel; the human
// message is written to os.Stderr, not the error).
func exitCodeOf(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		return exitSuccess
	}
	var ec *exitCodeError
	if !errors.As(err, &ec) {
		t.Fatalf("expected *exitCodeError, got %T: %v", err, err)
	}
	return ec.code
}

func TestFlowsRun_TagAndPositionalRejected(t *testing.T) {
	// A 404-everything server is fine: the mutual-exclusion check fires before any API call.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	state := makeState(t, "test-api-key", "", srv.URL)
	cmd := newFlowsRunCmd(state)
	cmd.SetArgs([]string{flowUUID().String(), "--tag", "prod"})
	var buf bytes.Buffer
	cmd.SetErr(&buf)
	cmd.SetOut(&buf)

	if code := exitCodeOf(t, cmd.Execute()); code != exitError {
		t.Errorf("expected exit code %d (rejected before any API call), got %d", exitError, code)
	}
}

func TestFlowsRun_InvalidMatchMode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	state := makeState(t, "test-api-key", "", srv.URL)
	cmd := newFlowsRunCmd(state)
	cmd.SetArgs([]string{"--tag", "prod", "--match-mode", "bogus"})
	var buf bytes.Buffer
	cmd.SetErr(&buf)
	cmd.SetOut(&buf)

	if code := exitCodeOf(t, cmd.Execute()); code != exitError {
		t.Errorf("expected exit code %d for invalid --match-mode, got %d", exitError, code)
	}
}

// TestFlowsRun_TagResolutionRunsResolvedFlows exercises the full path: --tag resolves
// flow IDs via /flows/search, then the existing launch/run path executes each resolved
// flow. Uses --parallel 2 to confirm parallelism is preserved for tag-resolved runs.
func TestFlowsRun_TagResolutionRunsResolvedFlows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows due to shell script runner")
	}

	ids := []string{
		"550e8400-e29b-41d4-a716-446655440020",
		"550e8400-e29b-41d4-a716-446655440021",
		"550e8400-e29b-41d4-a716-446655440022",
	}

	var mu sync.Mutex
	launchCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/flows/search") && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(searchListResponse(ids, int64(len(ids))))
		case strings.Contains(r.URL.Path, "/launch") && r.Method == http.MethodPost:
			mu.Lock()
			launchCount++
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(launchResponse(false))
		case strings.Contains(r.URL.Path, "/complete") && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(publishResponse())
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	resultJSON, _ := json.Marshal(successRunnerResult())
	runnerBinary := buildFakeRunner(t, 0, string(resultJSON))

	state := makeState(t, "test-api-key", "", srv.URL)
	cmd := newFlowsRunCmd(state)
	cmd.SetArgs([]string{
		"--tag", "production",
		"--match-mode", "any",
		"--parallel", "2",
		"--runner-binary", runnerBinary,
		"-o", "json",
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if code := exitCodeOf(t, cmd.Execute()); code != exitSuccess {
		t.Fatalf("expected exit success running tag-resolved flows, got code %d\noutput: %s", code, out.String())
	}

	mu.Lock()
	defer mu.Unlock()
	if launchCount != len(ids) {
		t.Errorf("expected %d launches (one per resolved flow), got %d", len(ids), launchCount)
	}
}
