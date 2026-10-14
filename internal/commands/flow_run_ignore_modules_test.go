package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/output"

	"github.com/google/uuid"
)

// roleFlow is one flow served by the paginating search stub.
type roleFlow struct {
	id   uuid.UUID
	role api.FlowRole
}

// makeRoleFlows builds modules module flows followed by standalones standalone flows with
// distinct ids.
func makeRoleFlows(modules, standalones int) []roleFlow {
	flows := make([]roleFlow, 0, modules+standalones)
	for i := range modules {
		flows = append(flows, roleFlow{
			id:   uuid.MustParse(fmt.Sprintf("550e8400-e29b-41d4-a716-4466554%05d", i)),
			role: api.FlowRoleModule,
		})
	}
	for i := range standalones {
		flows = append(flows, roleFlow{
			id:   uuid.MustParse(fmt.Sprintf("650e8400-e29b-41d4-a716-4466554%05d", i)),
			role: api.FlowRoleStandalone,
		})
	}
	return flows
}

// searchStub serves POST /flows/search from flows, honouring pagination.limit and
// pagination.offset like the server does, and counts launches per flow id.
type searchStub struct {
	*httptest.Server
	mu       sync.Mutex
	launched map[string]int
	pages    int
}

func newSearchStub(t *testing.T, flows []roleFlow) *searchStub {
	t.Helper()
	stub := &searchStub{launched: map[string]int{}}
	stub.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/flows/search") && r.Method == http.MethodPost:
			var req api.FlowSearchRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			limit, offset := 20, 0
			if req.Pagination != nil {
				if req.Pagination.Limit != nil {
					limit = int(*req.Pagination.Limit)
				}
				if req.Pagination.Offset != nil {
					offset = int(*req.Pagination.Offset)
				}
			}
			end := min(offset+limit, len(flows))
			items := []api.FlowSearchItem{}
			if offset < len(flows) {
				for _, f := range flows[offset:end] {
					items = append(items, api.FlowSearchItem{Id: f.id, Role: f.role, Tags: []string{}})
				}
			}
			stub.mu.Lock()
			stub.pages++
			stub.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(api.FlowSearchResponse{
				Count: len(items), Items: items, Total: int64(len(flows)),
			})
		case strings.Contains(r.URL.Path, "/launch") && r.Method == http.MethodPost:
			stub.mu.Lock()
			for _, f := range flows {
				if strings.Contains(r.URL.Path, f.id.String()) {
					stub.launched[f.id.String()]++
				}
			}
			stub.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(launchResponse(false))
		default:
			if !serveFakeJobRequest(w, r) {
				w.WriteHeader(http.StatusNotFound)
			}
		}
	}))
	t.Cleanup(stub.Close)
	return stub
}

func runTagCommand(t *testing.T, srvURL string, args ...string) (int, string, string) {
	t.Helper()
	state := makeState(t, "test-api-key", "", srvURL)
	state.OutputFormat = output.FormatJSON
	cmd := newFlowRunCmd(state)
	cmd.SetArgs(args)
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	code := exitCodeOf(t, cmd.Execute())
	return code, out.String(), errBuf.String()
}

func TestFlowRun_IgnoreModulesSkipsModuleFlows(t *testing.T) {
	flows := makeRoleFlows(2, 2)
	stub := newSearchStub(t, flows)

	code, _, _ := runTagCommand(t, stub.URL, "--tag", "smoke", "--ignore-modules")
	if code != exitSuccess {
		t.Fatalf("exit code = %d, want %d", code, exitSuccess)
	}
	stub.mu.Lock()
	defer stub.mu.Unlock()
	for _, f := range flows {
		got := stub.launched[f.id.String()]
		want := 0
		if f.role == api.FlowRoleStandalone {
			want = 1
		}
		if got != want {
			t.Errorf("flow %s (%s) launched %d times, want %d", f.id, f.role, got, want)
		}
	}
}

func TestFlowRun_TagRunsModuleFlowsWithoutIgnoreModules(t *testing.T) {
	flows := makeRoleFlows(2, 2)
	stub := newSearchStub(t, flows)

	code, _, stderr := runTagCommand(t, stub.URL, "--tag", "smoke")
	if code != exitSuccess {
		t.Fatalf("exit code = %d, want %d", code, exitSuccess)
	}
	stub.mu.Lock()
	defer stub.mu.Unlock()
	for _, f := range flows {
		if stub.launched[f.id.String()] != 1 {
			t.Errorf("flow %s (%s) launched %d times, want 1", f.id, f.role, stub.launched[f.id.String()])
		}
	}
	if strings.Contains(stderr, "Skipped") {
		t.Errorf("stderr mentions skipped modules without the flag: %q", stderr)
	}
}

func TestFlowRun_IgnoreModulesWithPositionalIDRejected(t *testing.T) {
	stub := newSearchStub(t, makeRoleFlows(0, 1))

	code, _, _ := runTagCommand(t, stub.URL, flowUUID().String(), "--ignore-modules")
	if code != exitError {
		t.Errorf("exit code = %d, want %d", code, exitError)
	}
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if len(stub.launched) != 0 || stub.pages != 0 {
		t.Errorf("rejected call reached the API: launches=%v search pages=%d", stub.launched, stub.pages)
	}
}

func TestFlowRun_IgnoreModulesAllModulesMatchedIsAnError(t *testing.T) {
	stub := newSearchStub(t, makeRoleFlows(3, 0))

	code, _, _ := runTagCommand(t, stub.URL, "--tag", "smoke", "--ignore-modules")
	if code != exitError {
		t.Errorf("exit code = %d, want %d (same as no flows matched)", code, exitError)
	}

	state := makeState(t, "test-api-key", "", stub.URL)
	_, _, err := resolveStandaloneFlowIDsByTags(context.Background(), state, []string{"smoke"}, "any")
	if err == nil || !strings.Contains(err.Error(), "no flows matched the given tags (3 module flows skipped)") {
		t.Errorf("error = %v, want no-match message naming 3 skipped modules", err)
	}
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if len(stub.launched) != 0 {
		t.Errorf("launched flows after an all-modules match: %v", stub.launched)
	}
}

func TestResolveStandaloneFlowIDsByTags_NoMatchesKeepsPlainMessage(t *testing.T) {
	stub := newSearchStub(t, nil)
	state := makeState(t, "test-api-key", "", stub.URL)

	_, _, err := resolveStandaloneFlowIDsByTags(context.Background(), state, []string{"none"}, "any")
	if err == nil || err.Error() != "no flows matched the given tags" {
		t.Errorf("error = %v, want the plain no-match message", err)
	}
}

func TestResolveStandaloneFlowIDsByTags_CapCountsOnlyLaunchedFlows(t *testing.T) {
	// 130 matches span two search pages and exceed the cap, but only 40 are standalone, and
	// the last 40 sit on the second page.
	stub := newSearchStub(t, makeRoleFlows(90, 40))
	state := makeState(t, "test-api-key", "", stub.URL)

	ids, skipped, err := resolveStandaloneFlowIDsByTags(context.Background(), state, []string{"smoke"}, "any")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 40 || skipped != 90 {
		t.Errorf("got %d ids and %d skipped, want 40 and 90", len(ids), skipped)
	}
	if stub.pages != 2 {
		t.Errorf("search pages = %d, want 2", stub.pages)
	}
}

func TestResolveStandaloneFlowIDsByTags_CapStillRejectsTooManyStandaloneFlows(t *testing.T) {
	stub := newSearchStub(t, makeRoleFlows(10, maxTagResolvedFlows+1))
	state := makeState(t, "test-api-key", "", stub.URL)

	_, _, err := resolveStandaloneFlowIDsByTags(context.Background(), state, []string{"smoke"}, "any")
	if err == nil || !strings.Contains(err.Error(), "safety cap") {
		t.Errorf("error = %v, want the safety cap error", err)
	}
}

func TestResolveStandaloneFlowIDsByTags_RefusesToScanTooManyMatches(t *testing.T) {
	stub := newSearchStub(t, makeRoleFlows(maxTagScannedFlows+1, 0))
	state := makeState(t, "test-api-key", "", stub.URL)

	_, _, err := resolveStandaloneFlowIDsByTags(context.Background(), state, []string{"smoke"}, "any")
	if err == nil || !strings.Contains(err.Error(), "too many to filter out modules") {
		t.Errorf("error = %v, want the scan bound error", err)
	}
}

func TestFlowRun_IgnoreModulesReportsSkippedOnStderrOnly(t *testing.T) {
	stub := newSearchStub(t, makeRoleFlows(2, 1))

	code, stdout, stderr := runTagCommand(t, stub.URL, "--tag", "smoke", "--ignore-modules")
	if code != exitSuccess {
		t.Fatalf("exit code = %d, want %d", code, exitSuccess)
	}
	if !strings.Contains(stderr, "Skipped 2 module flow(s) matched by the tags.") {
		t.Errorf("stderr = %q, want the skipped-modules notice", stderr)
	}
	if strings.Contains(stdout, "Skipped") || strings.Contains(stdout, "module") {
		t.Errorf("stdout carries the notice: %q", stdout)
	}
}
