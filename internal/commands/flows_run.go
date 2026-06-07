package commands

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/output"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// Exit codes for `echopoint flows run`.
const (
	exitSuccess    = 0
	exitFlowFailed = 1
	exitCancelled  = 2
	exitError      = 3
	exitTimeout    = 4
)

const (
	statusCompleted        = "completed"
	statusFailed           = "failed"
	statusCancelled        = "cancelled"
	statusError            = "error"
	outputFormatJSON       = "json"
	githubActionsTrueValue = "true"
)

// maxTagResolvedFlows caps how many flows a single --tag selection may launch, guarding
// against an over-broad tag filter accidentally launching a huge batch from CI.
const maxTagResolvedFlows = 50

// statusForExit maps a CLI exit code to the stable status string surfaced in JSON output and
// to the GitHub Action. Flow failures are "failed"; launch/runner/publish/timeout problems are
// "error" so callers can distinguish a failing flow from broken infrastructure.
func statusForExit(code int) string {
	switch code {
	case exitSuccess:
		return statusCompleted
	case exitFlowFailed:
		return statusFailed
	case exitCancelled:
		return statusCancelled
	default: // exitError, exitTimeout
		return statusError
	}
}

// FlowRunResult holds the per-flow result for JSON output.
type FlowRunResult struct {
	ExecutionID  string        `json:"execution_id"`
	FlowID       string        `json:"flow_id"`
	FlowURL      string        `json:"flow_url,omitempty"`
	Status       string        `json:"status"`
	Success      bool          `json:"success"`
	ExitCode     int           `json:"exit_code"`
	DurationMs   int64         `json:"duration_ms"`
	ErrorMessage *string       `json:"error_message"`
	Nodes        []FlowRunNode `json:"nodes"`
}

// FlowRunNode holds a node summary for JSON output.
type FlowRunNode struct {
	NodeID      string             `json:"node_id"`
	DisplayName string             `json:"display_name"`
	NodeType    string             `json:"node_type"`
	Status      string             `json:"status"`
	DurationMs  *int               `json:"duration_ms"`
	ErrorMsg    *string            `json:"error_message"`
	Assertions  []AssertionSummary `json:"assertions,omitempty"`
}

// AssertionSummary mirrors a runner AssertionResult so `flows run` can show what
// each assertion compared (expected vs actual), not just whether the node failed.
type AssertionSummary struct {
	Index     int    `json:"index"`
	Extractor string `json:"extractor"`
	Operator  string `json:"operator"`
	Expected  any    `json:"expected"`
	Actual    any    `json:"actual"`
	Passed    bool   `json:"passed"`
	Error     string `json:"error,omitempty"`
}

// FlowRunOutput is the single-flow stdout JSON shape.
type FlowRunOutput struct {
	FlowRunResult
}

// MultiFlowRunOutput is the multi-flow stdout JSON shape.
type MultiFlowRunOutput struct {
	Status     string          `json:"status"`
	Success    bool            `json:"success"`
	ExitCode   int             `json:"exit_code"`
	DurationMs int64           `json:"duration_ms"`
	Results    []FlowRunResult `json:"results"`
}

// ephemeralPackage is the JSON the runner reads from stdin.
type ephemeralPackage struct {
	ExecutionID     string                 `json:"execution_id"`
	FlowID          string                 `json:"flow_id"`
	FlowDefinition  map[string]interface{} `json:"flow_definition"`
	Inputs          map[string]interface{} `json:"inputs"`
	ReferencedFlows map[string]interface{} `json:"referenced_flows"`
}

// ephemeralResult is the JSON the runner writes to stdout.
type ephemeralResult struct {
	Status       string                 `json:"status"`
	StartedAt    string                 `json:"started_at"`
	CompletedAt  string                 `json:"completed_at"`
	DurationMs   int64                  `json:"duration_ms"`
	Result       map[string]interface{} `json:"result"`
	ErrorMessage *string                `json:"error_message"`
	ErrorCode    *string                `json:"error_code"`
}

func newFlowsRunCmd(state *AppState) *cobra.Command {
	var (
		flagEnvironment    string
		flagVersionID      string
		flagRunnerBinary   string
		flagIdempotencyKey string
		flagPollTimeout    time.Duration
		flagParallel       int
		flagOutput         string
		flagVerbose        bool
		flagTags           []string
		flagMatchMode      string
	)

	cmd := &cobra.Command{
		Use:   "run [<flow-id>...] [--tag <tag>...]",
		Short: "Run one or more flows using an ephemeral runner",
		Long: `Run one or more flows locally using the ephemeral runner mode.

The CLI launches each flow on the server with runner_type=ephemeral, receives
an execution package, runs it using echopoint-runner, and publishes the result.

Authentication: a logged-in session (echopoint auth login) or an organization
API key (--api-key / ECHOPOINT_API_KEY). An organization ID is always required
(--organization-id / ECHOPOINT_ORGANIZATION_ID, or the profile default).

Exit codes:
  0  all flows succeeded
  1  one or more flows failed (node assertions or runner failure)
  2  cancelled
  3  API / runner / contract error
  4  timeout`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Ephemeral execution needs an org-scoped credential for both launch
			// (flows:execute) and result publication. The completion endpoint accepts
			// ApiKeys:[runner:complete] or a session bearer carrying flows:execute, so
			// either a stored login or an API key works; an org ID is always required.
			if state.APIKey == "" && state.Token == "" {
				return runError(
					cmd,
					state,
					flagOutput,
					nil,
					exitError,
					"ephemeral execution requires authentication: log in (echopoint auth login) or set an API key (--api-key or ECHOPOINT_API_KEY)",
				)
			}
			if state.OrganizationID == "" {
				return runError(cmd, state, flagOutput, nil, exitError,
					"ephemeral execution requires an organization ID (--organization-id or ECHOPOINT_ORGANIZATION_ID)")
			}

			if flagParallel < 1 {
				return runError(cmd, state, flagOutput, nil, exitError, "--parallel must be >= 1")
			}

			// --tag selects flows by tag and is mutually exclusive with positional flow IDs.
			// Require exactly one selection mode.
			if len(flagTags) > 0 && len(args) > 0 {
				return runError(cmd, state, flagOutput, nil, exitError,
					"--tag cannot be combined with positional flow IDs; use one or the other")
			}
			if len(flagTags) == 0 && len(args) == 0 {
				return runError(cmd, state, flagOutput, nil, exitError,
					"provide at least one flow ID, or use --tag to select flows by tag")
			}
			if flagMatchMode != string(api.Any) && flagMatchMode != string(api.All) {
				return runError(cmd, state, flagOutput, nil, exitError,
					fmt.Sprintf("invalid --match-mode %q; must be %q or %q", flagMatchMode, api.Any, api.All))
			}

			if flagPollTimeout == 0 {
				flagPollTimeout = 30 * time.Minute
			}

			runnerBinary, err := resolveRunnerBinary(flagRunnerBinary)
			if err != nil {
				return runError(cmd, state, flagOutput, nil, exitError, err.Error())
			}

			ctx, cancel := context.WithTimeout(context.Background(), flagPollTimeout)
			defer cancel()

			flowIDs := args
			if len(flagTags) > 0 {
				resolved, resolveErr := resolveFlowIDsByTags(ctx, state, flagTags, flagMatchMode)
				if resolveErr != nil {
					return runError(cmd, state, flagOutput, nil, exitError, resolveErr.Error())
				}
				flowIDs = resolved
			}

			baseKey := resolveIdempotencyKey(flagIdempotencyKey, flowIDs)

			results, exitCode := executeFlows(
				ctx, state, flowIDs, runnerBinary, baseKey,
				flagEnvironment, flagVersionID, flagParallel, flagOutput,
			)

			return emitOutput(cmd, state, flagOutput, flagVerbose, results, exitCode, len(flowIDs))
		},
	}

	cmd.Flags().BoolVar(&flagVerbose, "verbose", false,
		"Print each node's status (name, status, duration) as the flow runs")
	cmd.Flags().StringVar(&flagEnvironment, "environment", "", "Named environment key to overlay on flow inputs")
	cmd.Flags().StringVar(&flagVersionID, "version-id", "",
		"Flow version ID to execute (default: current flow definition)")
	cmd.Flags().StringVar(&flagRunnerBinary, "runner-binary", "",
		"Path to echopoint-runner binary (default: echopoint-runner on PATH)")
	cmd.Flags().StringVar(&flagIdempotencyKey, "idempotency-key", "",
		"Stable key for idempotent CI retries (auto-derived from GitHub env when GITHUB_ACTIONS=true)")
	cmd.Flags().DurationVar(&flagPollTimeout, "poll-timeout", 30*time.Minute,
		"Maximum time to wait for each flow execution")
	cmd.Flags().IntVar(&flagParallel, "parallel", 1, "Maximum number of flows to run concurrently (>= 1)")
	cmd.Flags().StringVarP(&flagOutput, "output", "o", "", "Output format: json (or empty for human)")
	cmd.Flags().StringArrayVar(&flagTags, "tag", nil,
		"Select flows by tag instead of by ID (repeatable). Mutually exclusive with positional flow IDs.")
	cmd.Flags().StringVar(&flagMatchMode, "match-mode", string(api.Any),
		`Tag match mode when using --tag: "any" (default, OR) or "all" (AND)`)

	return cmd
}

// resolveFlowIDsByTags resolves flows matching the given tags into flow IDs via
// POST /flows/search, enforcing the CLI safety cap. The resolved IDs feed the same
// execution path used for positional flow IDs.
func resolveFlowIDsByTags(
	ctx context.Context,
	state *AppState,
	tags []string,
	matchMode string,
) ([]string, error) {
	limit := int32(maxTagResolvedFlows)
	mode := api.TagMatchMode(matchMode)
	body := api.FlowSearchRequest{
		Tags:         &tags,
		TagMatchMode: &mode,
		Pagination:   &api.PaginationRequest{Limit: &limit},
	}
	params := &api.SearchFlowsParams{XOrganizationID: state.OrganizationID}

	resp, err := state.Client.API().SearchFlowsWithResponse(ctx, params, body)
	if err != nil {
		return nil, fmt.Errorf("search flows by tag: %w", err)
	}
	if resp.JSON200 == nil {
		return nil, fmt.Errorf(
			"search flows by tag: unexpected status %d: %s", resp.StatusCode(), string(resp.Body),
		)
	}

	result := resp.JSON200
	if result.Total > int64(maxTagResolvedFlows) {
		return nil, fmt.Errorf(
			"tag search matched %d flows, exceeding the CLI safety cap of %d; "+
				"narrow the tags (or use --match-mode all) before launching",
			result.Total, maxTagResolvedFlows,
		)
	}
	if len(result.Items) == 0 {
		return nil, fmt.Errorf("no flows matched the given tags")
	}

	ids := make([]string, 0, len(result.Items))
	for _, item := range result.Items {
		ids = append(ids, item.Id.String())
	}
	return ids, nil
}

func resolveRunnerBinary(flagValue string) (string, error) {
	if flagValue != "" {
		info, err := os.Stat(flagValue)
		if err != nil {
			return "", fmt.Errorf("runner binary not found at %q: %w", flagValue, err)
		}
		if info.IsDir() {
			return "", fmt.Errorf("runner binary path %q is a directory", flagValue)
		}
		if info.Mode()&0o111 == 0 {
			return "", fmt.Errorf("runner binary %q is not executable", flagValue)
		}
		return flagValue, nil
	}

	path, err := exec.LookPath("echopoint-runner")
	if err != nil {
		return "", fmt.Errorf("echopoint-runner not found on PATH; use --runner-binary to specify the path")
	}
	return path, nil
}

func resolveIdempotencyKey(flagValue string, flowIDs []string) string {
	if flagValue != "" {
		return flagValue
	}
	if os.Getenv("ECHOPOINT_IDEMPOTENCY_KEY") != "" {
		return os.Getenv("ECHOPOINT_IDEMPOTENCY_KEY")
	}
	if os.Getenv("GITHUB_ACTIONS") == githubActionsTrueValue {
		return deriveGitHubIdempotencyKey(flowIDs)
	}
	return ""
}

func deriveGitHubIdempotencyKey(flowIDs []string) string {
	parts := []string{
		os.Getenv("GITHUB_REPOSITORY"),
		os.Getenv("GITHUB_WORKFLOW"),
		os.Getenv("GITHUB_JOB"),
		os.Getenv("GITHUB_RUN_ID"),
		os.Getenv("GITHUB_RUN_ATTEMPT"),
	}
	base := strings.Join(parts, "/")
	if base == "////" {
		return ""
	}
	h := sha256.Sum256([]byte(base))
	return fmt.Sprintf("gh-%x", h[:8])
}

func derivePerFlowKey(baseKey, flowID string) string {
	if baseKey == "" {
		return ""
	}
	h := sha256.Sum256([]byte(baseKey + ":" + flowID))
	return fmt.Sprintf("%s-%x", baseKey, h[:8])
}

func buildGitHubTriggerMetadata() *api.GitTriggerMetadata {
	if os.Getenv("GITHUB_ACTIONS") != githubActionsTrueValue {
		return nil
	}
	source := "github_actions"
	meta := api.GitTriggerMetadata{Source: &source}
	if v := os.Getenv("GITHUB_REPOSITORY"); v != "" {
		meta.Repository = &v
	}
	if v := os.Getenv("GITHUB_WORKFLOW"); v != "" {
		meta.Workflow = &v
	}
	if v := os.Getenv("GITHUB_JOB"); v != "" {
		meta.Job = &v
	}
	if v := os.Getenv("GITHUB_RUN_ID"); v != "" {
		meta.RunId = &v
	}
	if v := os.Getenv("GITHUB_RUN_ATTEMPT"); v != "" {
		meta.RunAttempt = &v
	}
	if v := os.Getenv("GITHUB_SHA"); v != "" {
		meta.Sha = &v
	}
	if v := os.Getenv("GITHUB_REF"); v != "" {
		meta.Ref = &v
	}
	if v := os.Getenv("GITHUB_ACTOR"); v != "" {
		meta.Actor = &v
	}
	return &meta
}

func executeFlows(
	ctx context.Context,
	state *AppState,
	flowIDs []string,
	runnerBinary string,
	baseKey string,
	environment string,
	versionID string,
	parallel int,
	outputFormat string,
) ([]FlowRunResult, int) {
	results := make([]FlowRunResult, len(flowIDs))
	for i := range results {
		results[i] = FlowRunResult{FlowID: flowIDs[i]}
	}

	if parallel == 1 || len(flowIDs) == 1 {
		for i, flowID := range flowIDs {
			key := derivePerFlowKey(baseKey, flowID)
			if len(flowIDs) == 1 && baseKey != "" {
				key = baseKey
			}
			results[i] = runSingleFlow(ctx, state, flowID, runnerBinary, key, environment, versionID, outputFormat)
		}
	} else {
		sem := make(chan struct{}, parallel)
		var mu sync.Mutex
		var wg sync.WaitGroup

		for i, flowID := range flowIDs {
			wg.Add(1)
			go func(idx int, fid string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				key := derivePerFlowKey(baseKey, fid)
				result := runSingleFlow(ctx, state, fid, runnerBinary, key, environment, versionID, outputFormat)
				mu.Lock()
				results[idx] = result
				mu.Unlock()
			}(i, flowID)
		}
		wg.Wait()
	}

	// Stamp the web URL for each flow so JSON consumers (and the GitHub summary)
	// can link straight to the flow in the app instead of showing a bare id.
	for i := range results {
		results[i].FlowURL = flowWebURL(state.Config.FrontendURL, results[i].FlowID)
	}

	return results, aggregateExitCode(results)
}

// flowWebURL builds the app link for a flow, e.g.
// https://app.echopoint.dev/flows/<id>. Returns "" when either part is missing.
func flowWebURL(frontendURL, flowID string) string {
	frontendURL = strings.TrimRight(strings.TrimSpace(frontendURL), "/")
	if frontendURL == "" || flowID == "" {
		return ""
	}
	return frontendURL + "/flows/" + flowID
}

func runSingleFlow(
	ctx context.Context,
	state *AppState,
	flowID string,
	runnerBinary string,
	idempotencyKey string,
	environment string,
	versionID string,
	outputFormat string,
) FlowRunResult {
	flowUUID, err := uuid.Parse(flowID)
	if err != nil {
		return errorResult(flowID, "", exitError, fmt.Sprintf("invalid flow id %q: %v", flowID, err))
	}

	pkg, executionID, terminalResult, launchErr := launchEphemeral(
		ctx, state, flowUUID, idempotencyKey, environment, versionID, outputFormat,
	)
	if launchErr != nil {
		return errorResult(flowID, "", exitCodeForError(launchErr), launchErr.Error())
	}

	if terminalResult != nil {
		return *terminalResult
	}

	runnerResult, runErr := runEphemeralRunner(ctx, runnerBinary, pkg)
	if runErr != nil {
		badResult := &ephemeralResult{
			Status:       statusFailed,
			StartedAt:    time.Now().UTC().Format(time.RFC3339),
			CompletedAt:  time.Now().UTC().Format(time.RFC3339),
			DurationMs:   0,
			ErrorMessage: ptrString(runErr.Error()),
			ErrorCode:    ptrString("RUNNER_ERROR"),
		}
		// Best-effort failure publication; ignore its error so the original failure
		// (including a timeout) determines the exit code.
		_, _ = publishResult(ctx, state, flowUUID, executionID, badResult, outputFormat)
		return errorResult(flowID, executionID.String(), exitCodeForError(runErr), runErr.Error())
	}

	publishResp, pubErr := publishResult(ctx, state, flowUUID, executionID, runnerResult, outputFormat)
	if pubErr != nil {
		return errorResult(flowID, executionID.String(), exitCodeForError(pubErr), pubErr.Error())
	}

	return buildRunResult(flowID, executionID.String(), publishResp, runnerResult)
}

// exitCodeForError classifies an operational error into a stable CI exit code:
// a deadline (--poll-timeout / runtime timeout) is 4, an explicit cancellation is 2,
// and everything else is an API/runner/contract error (3).
func exitCodeForError(err error) int {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return exitTimeout
	case errors.Is(err, context.Canceled):
		return exitCancelled
	default:
		return exitError
	}
}

func launchEphemeral(
	ctx context.Context,
	state *AppState,
	flowUUID uuid.UUID,
	idempotencyKey string,
	environment string,
	versionID string,
	outputFormat string,
) (*ephemeralPackage, uuid.UUID, *FlowRunResult, error) {
	runnerTypEphemeral := api.Ephemeral
	req := api.LaunchFlowRequest{
		RunnerType: &runnerTypEphemeral,
	}
	if environment != "" {
		req.EnvironmentKey = ptrString(environment)
	}
	if versionID != "" {
		vid, err := uuid.Parse(versionID)
		if err != nil {
			return nil, uuid.UUID{}, nil, fmt.Errorf("invalid version-id %q: %w", versionID, err)
		}
		req.VersionId = &vid
	}

	if os.Getenv("GITHUB_ACTIONS") == githubActionsTrueValue {
		triggerGit := api.TriggerTypeGit
		req.TriggerType = &triggerGit
		if git := buildGitHubTriggerMetadata(); git != nil {
			var tm api.TriggerMetadata
			if err := tm.FromGitTriggerMetadata(*git); err != nil {
				return nil, uuid.UUID{}, nil, fmt.Errorf("build trigger metadata: %w", err)
			}
			req.TriggerMetadata = &tm
		}
	}

	var params *api.LaunchFlowParams
	if idempotencyKey != "" {
		params = &api.LaunchFlowParams{IdempotencyKey: &idempotencyKey}
	}

	progressf(outputFormat, "Launching flow %s (ephemeral)...\n", flowUUID)

	resp, err := state.Client.API().LaunchFlowWithResponse(ctx, flowUUID, params, req)
	if err != nil {
		return nil, uuid.UUID{}, nil, fmt.Errorf("launch flow: %w", err)
	}
	if resp.JSON202 == nil {
		return nil, uuid.UUID{}, nil, fmt.Errorf(
			"launch flow: unexpected status %d: %s", resp.StatusCode(), string(resp.Body),
		)
	}

	execution := resp.JSON202.Execution
	executionID := execution.Id

	// Idempotent terminal replay: the server returns the existing execution when a duplicate
	// launch is detected and the execution has already reached a terminal state. The caller
	// must not run the runner in this case — just surface the existing status.
	if isTerminalStatus(string(execution.Status)) {
		result := buildRunResultFromExecution(execution.FlowId.String(), executionID.String(), execution)
		return nil, executionID, &result, nil
	}

	epkg := executionToEphemeralPackage(execution)
	return &epkg, executionID, nil, nil
}

func isTerminalStatus(status string) bool {
	return status == statusCompleted || status == statusFailed || status == statusCancelled
}

func executionToEphemeralPackage(execution api.FlowExecution) ephemeralPackage {
	var flowDef map[string]interface{}
	flowDefBytes, _ := json.Marshal(execution.FlowSnapshot)
	_ = json.Unmarshal(flowDefBytes, &flowDef)

	inputs := map[string]interface{}{}
	for k, v := range execution.RunnerInputs {
		inputs[k] = v
	}

	referenced := map[string]interface{}{}
	for k, v := range execution.ReferencedFlows {
		refBytes, _ := json.Marshal(v)
		var refObj interface{}
		_ = json.Unmarshal(refBytes, &refObj)
		referenced[k] = refObj
	}

	return ephemeralPackage{
		ExecutionID:     execution.Id.String(),
		FlowID:          execution.FlowId.String(),
		FlowDefinition:  flowDef,
		Inputs:          inputs,
		ReferencedFlows: referenced,
	}
}

// filteredRunnerEnv returns the current environment minus credentials the ephemeral runner
// must never receive (it does not authenticate to the control plane).
func filteredRunnerEnv() []string {
	blocked := map[string]bool{
		"ECHOPOINT_API_KEY":         true,
		"ECHOPOINT_ORGANIZATION_ID": true,
		"ECHOPOINT_TOKEN":           true,
	}
	env := os.Environ()
	filtered := make([]string, 0, len(env))
	for _, kv := range env {
		key := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			key = kv[:i]
		}
		if blocked[key] {
			continue
		}
		filtered = append(filtered, kv)
	}
	return filtered
}

// extractRunnerError pulls a concise failure reason out of the runner's stderr,
// which is line-delimited JSON logs. It returns the most specific error_message
// or error field from the last JSON line that has one, falling back to the last
// non-empty line.
func extractRunnerError(stderr string) string {
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var entry map[string]any
		if json.Unmarshal([]byte(line), &entry) != nil {
			continue
		}
		for _, key := range []string{"error_message", "error"} {
			if v, ok := entry[key].(string); ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return ""
}

func runEphemeralRunner(ctx context.Context, runnerBinary string, pkg *ephemeralPackage) (*ephemeralResult, error) {
	pkgBytes, err := json.Marshal(pkg)
	if err != nil {
		return nil, fmt.Errorf("marshal package: %w", err)
	}

	runnerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	runnerCmd := exec.CommandContext(runnerCtx, runnerBinary, "ephemeral", "--input", "-", "--output", "-")
	runnerCmd.Stdin = bytes.NewReader(pkgBytes)
	// Least privilege: the ephemeral runner executes flow logic locally and needs no API
	// credentials, so strip them from the child environment to avoid exposing the org API
	// key to the runner subprocess.
	runnerCmd.Env = filteredRunnerEnv()

	var stdout, stderr bytes.Buffer
	runnerCmd.Stdout = &stdout
	runnerCmd.Stderr = &stderr

	if err := runnerCmd.Start(); err != nil {
		return nil, fmt.Errorf("start runner: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- runnerCmd.Wait() }()

	select {
	case <-ctx.Done():
		_ = runnerCmd.Process.Kill()
		return nil, fmt.Errorf("runner timeout/cancelled: %w", ctx.Err())
	case err := <-done:
		if err != nil {
			stderrStr := strings.TrimSpace(stderr.String())
			if stderrStr != "" {
				fmt.Fprintf(os.Stderr, "[runner stderr] %s\n", stderrStr)
			}
			// Surface the runner's actual failure reason (parse/validation/node
			// error) instead of just the opaque "exit status N", so it flows into
			// the result, JSON output, and the CI summary.
			if reason := extractRunnerError(stderrStr); reason != "" {
				return nil, fmt.Errorf("runner failed: %s", reason)
			}
			return nil, fmt.Errorf("runner exited with error: %w", err)
		}
	}

	stderrStr := strings.TrimSpace(stderr.String())
	if stderrStr != "" {
		fmt.Fprintf(os.Stderr, "[runner] %s\n", stderrStr)
	}

	var result ephemeralResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		// Do not include the raw runner stdout in the error: it is the result payload and
		// may contain resolved request/response data. Report only the byte length.
		return nil, fmt.Errorf("parse runner result (%d bytes): %w", stdout.Len(), err)
	}

	return &result, nil
}

func publishResult(
	ctx context.Context,
	state *AppState,
	_ uuid.UUID,
	executionID uuid.UUID,
	result *ephemeralResult,
	outputFormat string,
) (*api.EphemeralCompletionResponse, error) {
	status := api.RunnerJobTerminalStatus(result.Status)
	req := api.EphemeralCompletionRequest{
		Status:       status,
		DurationMs:   result.DurationMs,
		ErrorMessage: result.ErrorMessage,
		ErrorCode:    result.ErrorCode,
	}

	startedAt, _ := time.Parse(time.RFC3339, result.StartedAt)
	completedAt, _ := time.Parse(time.RFC3339, result.CompletedAt)
	req.StartedAt = startedAt
	req.CompletedAt = completedAt

	if result.Result != nil {
		req.Result = &result.Result
	}

	progressf(outputFormat, "Publishing result for execution %s...\n", executionID)

	params := &api.CompleteEphemeralExecutionParams{
		XOrganizationID: state.OrganizationID,
	}

	var lastErr error
	for attempt := range 3 {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				// Wrap the context error so exitCodeForError classifies a timeout (4) or
				// cancellation (2) rather than a generic contract error (3).
				return nil, fmt.Errorf("publish cancelled: %w", ctx.Err())
			case <-time.After(time.Duration(attempt) * 2 * time.Second):
			}
		}

		resp, err := state.Client.API().CompleteEphemeralExecutionWithResponse(ctx, executionID, params, req)
		if err != nil {
			lastErr = fmt.Errorf("publish result: %w", err)
			continue
		}

		if resp.StatusCode() == 200 && resp.JSON200 != nil {
			return resp.JSON200, nil
		}

		if resp.StatusCode() >= 500 {
			lastErr = fmt.Errorf("publish result: server error %d: %s", resp.StatusCode(), string(resp.Body))
			continue
		}

		return nil, fmt.Errorf(
			"publish result: unexpected status %d: %s", resp.StatusCode(), string(resp.Body),
		)
	}

	return nil, lastErr
}

func buildRunResult(
	flowID, executionID string,
	resp *api.EphemeralCompletionResponse,
	runnerResult *ephemeralResult,
) FlowRunResult {
	execution := resp.Execution
	nodes := buildNodeList(resp.Nodes)
	attachAssertions(nodes, runnerResult)

	statusStr := string(execution.Status)
	success := execution.Status == statusCompleted
	exitCode := exitSuccess
	if !success {
		exitCode = exitFlowFailed
	}

	var durationMs int64
	if execution.StartedAt != (time.Time{}) && execution.CompletedAt != nil {
		durationMs = execution.CompletedAt.Sub(execution.StartedAt).Milliseconds()
	}

	return FlowRunResult{
		ExecutionID:  executionID,
		FlowID:       flowID,
		Status:       statusStr,
		Success:      success,
		ExitCode:     exitCode,
		DurationMs:   durationMs,
		ErrorMessage: execution.ErrorMessage,
		Nodes:        nodes,
	}
}

func buildRunResultFromExecution(flowID, executionID string, execution api.FlowExecution) FlowRunResult {
	statusStr := string(execution.Status)
	success := execution.Status == statusCompleted
	exitCode := exitSuccess
	if !success {
		exitCode = exitFlowFailed
	}
	if execution.Status == "cancelled" {
		exitCode = exitCancelled
	}

	var durationMs int64
	if execution.CompletedAt != nil {
		durationMs = execution.CompletedAt.Sub(execution.StartedAt).Milliseconds()
	}

	return FlowRunResult{
		ExecutionID:  executionID,
		FlowID:       flowID,
		Status:       statusStr,
		Success:      success,
		ExitCode:     exitCode,
		DurationMs:   durationMs,
		ErrorMessage: execution.ErrorMessage,
		Nodes:        nil,
	}
}

// attachAssertions enriches the control-plane node list with the per-assertion
// outcomes the runner recorded locally (expected/actual/passed). The completion
// response does not carry these, but the raw runner payload does.
func attachAssertions(nodes []FlowRunNode, runnerResult *ephemeralResult) {
	if runnerResult == nil || runnerResult.Result == nil {
		return
	}
	byNode := extractAssertionsByNode(runnerResult.Result)
	for i := range nodes {
		if assertions, ok := byNode[nodes[i].NodeID]; ok {
			nodes[i].Assertions = assertions
		}
	}
}

func extractAssertionsByNode(result map[string]interface{}) map[string][]AssertionSummary {
	executionResults, ok := result["execution_results"].(map[string]interface{})
	if !ok {
		return nil
	}
	out := make(map[string][]AssertionSummary, len(executionResults))
	for nodeID, raw := range executionResults {
		nodeMap, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		rawAssertions, ok := nodeMap["assertion_results"]
		if !ok {
			continue
		}
		encoded, err := json.Marshal(rawAssertions)
		if err != nil {
			continue
		}
		var summaries []AssertionSummary
		if json.Unmarshal(encoded, &summaries) == nil && len(summaries) > 0 {
			out[nodeID] = summaries
		}
	}
	return out
}

func buildNodeList(nodes []api.NodeExecutionResult) []FlowRunNode {
	result := make([]FlowRunNode, 0, len(nodes))
	for _, n := range nodes {
		var durationMs *int
		if n.DurationMs != nil {
			v := *n.DurationMs
			durationMs = &v
		}
		result = append(result, FlowRunNode{
			NodeID:      n.NodeId,
			DisplayName: n.DisplayName,
			NodeType:    string(n.NodeType),
			Status:      string(n.Status),
			DurationMs:  durationMs,
			ErrorMsg:    n.ErrorMessage,
		})
	}
	return result
}

func aggregateExitCode(results []FlowRunResult) int {
	code := exitSuccess
	for _, r := range results {
		if r.ExitCode > code {
			code = r.ExitCode
		}
	}
	return code
}

func errorResult(flowID, executionID string, exitCode int, msg string) FlowRunResult {
	return FlowRunResult{
		ExecutionID:  executionID,
		FlowID:       flowID,
		Status:       statusForExit(exitCode),
		Success:      false,
		ExitCode:     exitCode,
		ErrorMessage: ptrString(msg),
		Nodes:        nil,
	}
}

func emitOutput(
	_ *cobra.Command, _ *AppState, flagOutput string, verbose bool,
	results []FlowRunResult, exitCode int, numFlows int,
) error {
	writeSummary(results, exitCode, verbose)

	if summaryPath := os.Getenv("GITHUB_STEP_SUMMARY"); summaryPath != "" {
		_ = writeGitHubStepSummary(summaryPath, results)
	}

	if strings.ToLower(strings.TrimSpace(flagOutput)) == outputFormatJSON {
		if err := output.PrintJSON(os.Stdout, buildJSONOutput(results, exitCode, numFlows)); err != nil {
			return err
		}
	}

	return &exitCodeError{code: exitCode}
}

func buildJSONOutput(results []FlowRunResult, exitCode int, numFlows int) interface{} {
	if numFlows == 1 {
		return FlowRunOutput{results[0]}
	}

	var durationMs int64
	success := true
	for _, r := range results {
		durationMs += r.DurationMs
		if !r.Success {
			success = false
		}
	}
	return MultiFlowRunOutput{
		Status:     statusForExit(exitCode),
		Success:    success,
		ExitCode:   exitCode,
		DurationMs: durationMs,
		Results:    results,
	}
}

func runError(
	_ *cobra.Command, _ *AppState, flagOutput string,
	results []FlowRunResult, exitCode int, msg string,
) error {
	fmt.Fprintf(os.Stderr, "error: %s\n", msg)

	if strings.ToLower(strings.TrimSpace(flagOutput)) == outputFormatJSON {
		out := buildErrorJSON(results, exitCode, msg)
		_ = output.PrintJSON(os.Stdout, out)
	}

	return &exitCodeError{code: exitCode}
}

func buildErrorJSON(results []FlowRunResult, exitCode int, msg string) interface{} {
	if results != nil {
		return MultiFlowRunOutput{
			Status:   statusForExit(exitCode),
			Success:  false,
			ExitCode: exitCode,
			Results:  results,
		}
	}
	return FlowRunOutput{FlowRunResult{
		Status:       statusForExit(exitCode),
		Success:      false,
		ExitCode:     exitCode,
		ErrorMessage: ptrString(msg),
	}}
}

// exitCodeError is an error that carries an exit code so main can call os.Exit.
type exitCodeError struct {
	code int
}

func (e *exitCodeError) Error() string {
	if e.code == exitSuccess {
		return ""
	}
	return fmt.Sprintf("exit code %d", e.code)
}

func (e *exitCodeError) ExitCode() int {
	return e.code
}

// writeAssertions prints each recorded assertion under its node in verbose mode,
// showing what was compared so a pass or failure is self-explanatory.
func writeAssertions(assertions []AssertionSummary) {
	for _, a := range assertions {
		icon := "✓"
		if !a.Passed {
			icon = "✗"
		}
		fmt.Fprintf(
			os.Stderr, "      %s %s %s expected=%v actual=%v",
			icon, a.Extractor, a.Operator, a.Expected, a.Actual,
		)
		if a.Error != "" {
			fmt.Fprintf(os.Stderr, " — %s", a.Error)
		}
		fmt.Fprintln(os.Stderr)
	}
}

func writeSummary(results []FlowRunResult, exitCode int, verbose bool) {
	isGitHub := os.Getenv("GITHUB_ACTIONS") == githubActionsTrueValue

	if isGitHub {
		fmt.Fprintf(os.Stderr, "::group::Echopoint Flow Results\n")
	}

	for _, r := range results {
		icon := "✓"
		if !r.Success {
			icon = "✗"
		}
		fmt.Fprintf(os.Stderr, "%s Flow %s: %s", icon, r.FlowID, r.Status)
		if r.DurationMs > 0 {
			fmt.Fprintf(os.Stderr, " (%dms)", r.DurationMs)
		}
		fmt.Fprintln(os.Stderr)

		if r.ErrorMessage != nil && *r.ErrorMessage != "" {
			if isGitHub {
				fmt.Fprintf(os.Stderr, "::error::Flow %s failed: %s\n", r.FlowID, *r.ErrorMessage)
			} else {
				fmt.Fprintf(os.Stderr, "  Error: %s\n", *r.ErrorMessage)
			}
		}

		for _, n := range r.Nodes {
			switch {
			case verbose:
				icon := "✓"
				if n.Status == statusFailed {
					icon = "✗"
				}
				fmt.Fprintf(os.Stderr, "  %s %s (%s) %s", icon, n.DisplayName, n.NodeID, n.Status)
				if n.DurationMs != nil {
					fmt.Fprintf(os.Stderr, " (%dms)", *n.DurationMs)
				}
				if n.Status == statusFailed && n.ErrorMsg != nil {
					fmt.Fprintf(os.Stderr, " — %s", *n.ErrorMsg)
				}
				fmt.Fprintln(os.Stderr)
				writeAssertions(n.Assertions)
			case n.Status == statusFailed:
				nodeMsg := ""
				if n.ErrorMsg != nil {
					nodeMsg = ": " + *n.ErrorMsg
				}
				fmt.Fprintf(os.Stderr, "  Node %s (%s) failed%s\n", n.DisplayName, n.NodeID, nodeMsg)
			}
		}
	}

	if isGitHub {
		fmt.Fprintf(os.Stderr, "::endgroup::\n")
	}
}

func writeGitHubStepSummary(path string, results []FlowRunResult) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	fmt.Fprintln(f, "## Echopoint Flow Results")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "| Flow ID | Status | Duration | Error |")
	fmt.Fprintln(f, "|---------|--------|----------|-------|")

	for _, r := range results {
		errStr := ""
		if r.ErrorMessage != nil {
			errStr = *r.ErrorMessage
		}
		flowCell := fmt.Sprintf("`%s`", r.FlowID)
		if r.FlowURL != "" {
			flowCell = fmt.Sprintf("[`%s`](%s)", r.FlowID, r.FlowURL)
		}
		fmt.Fprintf(f, "| %s | %s | %dms | %s |\n", flowCell, r.Status, r.DurationMs, errStr)
	}

	return nil
}

func progressf(outputFormat, format string, args ...interface{}) {
	if strings.ToLower(strings.TrimSpace(outputFormat)) != outputFormatJSON {
		fmt.Fprintf(os.Stderr, format, args...)
	}
}

func ptrString(s string) *string {
	return &s
}
