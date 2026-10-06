package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"echopoint-cli/internal/api"
)

const (
	goldenReplayFlow    = "550e8400-e29b-41d4-a716-4466554400a2"
	goldenForbiddenFlow = "550e8400-e29b-41d4-a716-4466554400a3"
	goldenFailedFlow    = "550e8400-e29b-41d4-a716-4466554400a4"
	goldenCancelledFlow = "550e8400-e29b-41d4-a716-4466554400a5"
	goldenSlowFlow      = "550e8400-e29b-41d4-a716-4466554400a6"
	goldenVersionID     = "550e8400-e29b-41d4-a716-4466554400b1"
	goldenDirectory     = "testdata/flow_run"
	updateGoldenEnv     = "UPDATE_GOLDEN"
)

var (
	durationJSONPattern  = regexp.MustCompile(`"duration_ms": \d+`)
	durationHumanPattern = regexp.MustCompile(`\(\d+ms\)`)
	durationTablePattern = regexp.MustCompile(`\| \d+ms \|`)
	loopbackPortPattern  = regexp.MustCompile(`127\.0\.0\.1:\d+`)
)

type goldenLaunch struct {
	line string
}

type goldenFlowServer struct {
	mu       sync.Mutex
	launches []goldenLaunch
	server   *httptest.Server
}

func newGoldenFlowServer(t *testing.T, tagMatches []string) *goldenFlowServer {
	t.Helper()
	fake := &goldenFlowServer{}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/flows/search") && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(searchListResponse(tagMatches, int64(len(tagMatches))))
		case strings.HasSuffix(r.URL.Path, "/launch") && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			var compact bytes.Buffer
			if err := json.Compact(&compact, body); err != nil {
				compact.Write(body)
			}
			fake.mu.Lock()
			fake.launches = append(fake.launches, goldenLaunch{line: fmt.Sprintf(
				"POST %s idempotency-key=%q %s",
				r.URL.RequestURI(), r.Header.Get("Idempotency-Key"), compact.String(),
			)})
			fake.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			switch {
			case strings.Contains(r.URL.Path, goldenForbiddenFlow):
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"errors": []map[string]string{{"code": "FORBIDDEN", "message": "insufficient permissions"}},
				})
			case strings.Contains(r.URL.Path, goldenReplayFlow):
				w.WriteHeader(http.StatusAccepted)
				_ = json.NewEncoder(w).Encode(launchResponse(true))
			case strings.Contains(r.URL.Path, goldenFailedFlow):
				w.WriteHeader(http.StatusAccepted)
				_ = json.NewEncoder(w).Encode(launchResponseWithStatus("failed"))
			case strings.Contains(r.URL.Path, goldenCancelledFlow):
				w.WriteHeader(http.StatusAccepted)
				_ = json.NewEncoder(w).Encode(launchResponseWithStatus("cancelled"))
			case strings.Contains(r.URL.Path, goldenSlowFlow):
				select {
				case <-r.Context().Done():
				case <-time.After(5 * time.Second):
				}
			default:
				w.WriteHeader(http.StatusAccepted)
				_ = json.NewEncoder(w).Encode(launchResponse(false))
			}
		default:
			if !serveFakeJobRequest(w, r) {
				w.WriteHeader(http.StatusNotFound)
			}
		}
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

func (fake *goldenFlowServer) launchLines() string {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	lines := make([]string, 0, len(fake.launches))
	for _, launch := range fake.launches {
		lines = append(lines, launch.line)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func isolateCLIEnvironment(t *testing.T, apiURL string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("ECHOPOINT_CONFIG", filepath.Join(home, "config.yaml"))
	t.Setenv("ECHOPOINT_API_URL", apiURL)
	t.Setenv("ECHOPOINT_API_KEY", "golden-key")
	t.Setenv("ECHOPOINT_ORGANIZATION_ID", "org_test")
	for _, name := range []string{
		"ECHOPOINT_OUTPUT_FORMAT", "ECHOPOINT_TOKEN", "ECHOPOINT_PROFILE", "ECHOPOINT_IDEMPOTENCY_KEY",
		"GITHUB_ACTIONS", "GITHUB_STEP_SUMMARY", "GITHUB_REPOSITORY", "GITHUB_WORKFLOW", "GITHUB_JOB",
		"GITHUB_RUN_ID", "GITHUB_RUN_ATTEMPT", "GITHUB_SHA", "GITHUB_REF", "GITHUB_ACTOR",
	} {
		t.Setenv(name, "")
	}
}

func setGitHubActionsEnvironment(t *testing.T, summaryPath string) {
	t.Helper()
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_STEP_SUMMARY", summaryPath)
	t.Setenv("GITHUB_REPOSITORY", "acme/api")
	t.Setenv("GITHUB_WORKFLOW", "ci")
	t.Setenv("GITHUB_JOB", "flows")
	t.Setenv("GITHUB_RUN_ID", "4242")
	t.Setenv("GITHUB_RUN_ATTEMPT", "2")
	t.Setenv("GITHUB_SHA", "abc123")
	t.Setenv("GITHUB_REF", "refs/heads/main")
	t.Setenv("GITHUB_ACTOR", "octocat")
}

func normalizeDurations(text string) string {
	text = durationJSONPattern.ReplaceAllString(text, `"duration_ms": 0`)
	text = durationHumanPattern.ReplaceAllString(text, "(Nms)")
	text = durationTablePattern.ReplaceAllString(text, "| Nms |")
	return loopbackPortPattern.ReplaceAllString(text, "127.0.0.1:PORT")
}

func compareWithGolden(t *testing.T, name, actual string) {
	t.Helper()
	path := filepath.Join(goldenDirectory, name+".golden")
	if os.Getenv(updateGoldenEnv) != "" {
		if err := os.MkdirAll(goldenDirectory, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(actual), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (record it with %s=1)", err, updateGoldenEnv)
	}
	if string(want) != actual {
		t.Errorf("%s differs from the recorded behaviour\n--- want ---\n%s\n--- got ---\n%s", name, want, actual)
	}
}

type goldenRun struct {
	name       string
	args       []string
	tagMatches []string
	github     bool
}

func goldenRuns() []goldenRun {
	flow1 := flowUUID().String()
	return []goldenRun{
		{name: "single-completed-json", args: []string{flow1, "-o", "json"}},
		{name: "single-replay-json", args: []string{goldenReplayFlow, "-o", "json"}},
		{name: "single-uppercase-format", args: []string{goldenReplayFlow, "-o", "JSON"}},
		{name: "single-api-error-json", args: []string{goldenForbiddenFlow, "-o", "json"}},
		{name: "multi-json", args: []string{flow1, goldenReplayFlow, goldenForbiddenFlow, "-o", "json"}},
		{
			name: "multi-parallel-json",
			args: []string{flow1, goldenReplayFlow, goldenForbiddenFlow, "-o", "json", "--parallel", "3"},
		},
		{
			name: "action-flags-json",
			args: []string{
				flow1, "-o", "json", "--environment", "dev", "--version-id", goldenVersionID,
				"--poll-timeout", "300s", "--parallel", "1",
			},
		},
		{
			name: "tags-json",
			args: []string{"--tag", "smoke", "--tag", "api", "--match-mode", "all", "-o", "json"},
			tagMatches: []string{
				flow1, goldenReplayFlow,
			},
		},
		{name: "invalid-id-json", args: []string{"not-a-uuid", "-o", "json"}},
		{name: "no-selection-json", args: []string{"-o", "json"}},
		{name: "bad-parallel-json", args: []string{flow1, "-o", "json", "--parallel", "0"}},
		{name: "bad-match-mode-json", args: []string{"--tag", "x", "--match-mode", "bogus", "-o", "json"}},
		{name: "tag-and-id-json", args: []string{flow1, "--tag", "x", "-o", "json"}},
		{name: "exit-1-failed-json", args: []string{goldenFailedFlow, "-o", "json"}},
		{name: "exit-1-failed-human", args: []string{goldenFailedFlow}},
		{name: "exit-2-cancelled-json", args: []string{goldenCancelledFlow, "-o", "json"}},
		{name: "exit-2-cancelled-human", args: []string{goldenCancelledFlow}},
		{name: "exit-4-timeout-json", args: []string{goldenSlowFlow, "-o", "json", "--poll-timeout", "300ms"}},
		{name: "exit-4-timeout-human", args: []string{goldenSlowFlow, "--poll-timeout", "300ms"}},
		{
			name: "exit-codes-multi-json",
			args: []string{flow1, goldenFailedFlow, goldenCancelledFlow, goldenForbiddenFlow, "-o", "json"},
		},
		{name: "human-single", args: []string{flow1}},
		{name: "human-single-verbose", args: []string{flow1, "--verbose"}},
		{name: "human-multi", args: []string{flow1, goldenReplayFlow, goldenForbiddenFlow}},
		{name: "human-invalid-id", args: []string{"not-a-uuid"}},
		{
			name:   "github-actions-json",
			args:   []string{flow1, goldenReplayFlow, "-o", "json", "--environment", "dev"},
			github: true,
		},
		{name: "github-actions-failure-json", args: []string{goldenForbiddenFlow, "-o", "json"}, github: true},
	}
}

// The GitHub Action runs `echopoint flows run <ids> -o json ...` and parses stdout,
// reads the exit code, and reports on stderr and the step summary. Whatever the
// command is named or wired to, those must not move.
func TestFlowRunKeepsTheBehaviourTheGitHubActionDependsOn(t *testing.T) {
	for _, group := range runGroupNames() {
		for _, tc := range goldenRuns() {
			t.Run(group+"/"+tc.name, func(t *testing.T) {
				fake := newGoldenFlowServer(t, tc.tagMatches)
				isolateCLIEnvironment(t, fake.server.URL)
				summaryPath := filepath.Join(t.TempDir(), "summary.md")
				if tc.github {
					setGitHubActionsEnvironment(t, summaryPath)
				}

				args := append([]string{group, "run"}, tc.args...)
				stdout, stderr, code := runCLI(t, args...)

				summary, _ := os.ReadFile(summaryPath)
				recorded := fmt.Sprintf(
					"exit: %d\n--- stdout ---\n%s--- stderr ---\n%s--- launches ---\n%s\n--- step summary ---\n%s",
					code, normalizeDurations(stdout), normalizeDurations(stderr), fake.launchLines(),
					normalizeDurations(string(summary)),
				)
				compareWithGolden(t, tc.name, recorded)
			})
		}
	}
}

func runGroupNames() []string { return []string{"flows", "flow"} }

func launchResponseWithStatus(status string) api.LaunchFlowAcceptedResponse {
	response := launchResponse(true)
	response.Execution.Status = api.ExecutionStatus(status)
	return response
}
