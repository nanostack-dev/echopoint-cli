package commands

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nanostack-dev/echopoint-runner/pkg/jobrunner"

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

func newFlowsRunCmd(state *AppState) *cobra.Command {
	var (
		flagEnvironment    string
		flagVersionID      string
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

The CLI launches each flow on the server with runner_type=ephemeral, claims its
one-shot Job, and runs it with live progress and completion reporting.

Authentication: a logged-in session (echopoint auth login) or an organization
API key (--api-key / ECHOPOINT_API_KEY). An organization ID is always required
(--organization-id / ECHOPOINT_ORGANIZATION_ID, or the profile default).

Exit codes:
  0  all flows succeeded
  1  one or more flows failed (node assertions or runner failure)
  2  cancelled
  3  API / runner / contract error
  4  timeout`,
		Args:          cobra.ArbitraryArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Launch and one-time claim use flows:execute. Reporting uses the
			// one-Job token returned by the claim.
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
				ctx, state, flowIDs, baseKey,
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
			results[i] = runSingleFlow(ctx, state, flowID, key, environment, versionID, outputFormat)
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
				result := runSingleFlow(ctx, state, fid, key, environment, versionID, outputFormat)
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
	idempotencyKey string,
	environment string,
	versionID string,
	outputFormat string,
) FlowRunResult {
	flowUUID, err := uuid.Parse(flowID)
	if err != nil {
		return errorResult(flowID, "", exitError, fmt.Sprintf("invalid flow id %q: %v", flowID, err))
	}

	executionID, terminalResult, launchErr := launchEphemeral(
		ctx, state, flowUUID, idempotencyKey, environment, versionID, outputFormat,
	)
	if launchErr != nil {
		return errorResult(flowID, "", exitCodeForError(launchErr), launchErr.Error())
	}

	if terminalResult != nil {
		return *terminalResult
	}

	bootID := uuid.Must(uuid.NewV7())
	job, token, claimErr := claimEphemeralJob(ctx, state, executionID, bootID, outputFormat)
	if claimErr != nil {
		return errorResult(flowID, executionID.String(), exitCodeForError(claimErr), claimErr.Error())
	}

	runnerClient, clientErr := jobrunner.NewClient(jobrunner.Config{
		BaseURL:  state.Client.BaseURL(),
		JobToken: token,
		RunnerID: "echopoint-cli",
		BootID:   bootID,
		Timeout:  state.Config.API.Timeout,
	})
	if clientErr != nil {
		return errorResult(flowID, executionID.String(), exitError, clientErr.Error())
	}
	runnerResult, runErr := runnerClient.Run(ctx, job)
	if ctx.Err() != nil {
		return errorResult(flowID, executionID.String(), exitCodeForError(ctx.Err()), ctx.Err().Error())
	}
	if runErr != nil {
		return errorResult(flowID, executionID.String(), exitCodeForError(runErr), runErr.Error())
	}
	return buildRunResultFromJob(flowID, executionID.String(), runnerResult)
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
) (uuid.UUID, *FlowRunResult, error) {
	runnerTypEphemeral := api.Ephemeral
	jobVersion := api.LaunchFlowRequestEphemeralJobVersion(1)
	req := api.LaunchFlowRequest{
		RunnerType:          &runnerTypEphemeral,
		EphemeralJobVersion: &jobVersion,
	}
	if environment != "" {
		req.EnvironmentKey = new(environment)
	}
	if versionID != "" {
		vid, err := uuid.Parse(versionID)
		if err != nil {
			return uuid.UUID{}, nil, fmt.Errorf("invalid version-id %q: %w", versionID, err)
		}
		req.VersionId = &vid
	}

	if os.Getenv("GITHUB_ACTIONS") == githubActionsTrueValue {
		triggerGit := api.TriggerTypeGit
		req.TriggerType = &triggerGit
		if git := buildGitHubTriggerMetadata(); git != nil {
			var tm api.TriggerMetadata
			if err := tm.FromGitTriggerMetadata(*git); err != nil {
				return uuid.UUID{}, nil, fmt.Errorf("build trigger metadata: %w", err)
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
		return uuid.UUID{}, nil, fmt.Errorf("launch flow: %w", err)
	}
	if resp.JSON202 == nil {
		return uuid.UUID{}, nil, fmt.Errorf(
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
		return executionID, &result, nil
	}

	return executionID, nil, nil
}

func isTerminalStatus(status string) bool {
	return status == statusCompleted || status == statusFailed || status == statusCancelled
}

// claimEphemeralJob fetches the Job payload and token once. A lost claim response
// is deliberately not retried: the Job may already be running or claimed.
func claimEphemeralJob(
	ctx context.Context,
	state *AppState,
	executionID uuid.UUID,
	bootID uuid.UUID,
	outputFormat string,
) (jobrunner.Job, string, error) {
	progressf(outputFormat, "Claiming Job for execution %s...\n", executionID)
	params := &api.ClaimEphemeralJobParams{XOrganizationID: state.OrganizationID}
	response, err := state.Client.API().ClaimEphemeralJobWithResponse(
		ctx, executionID, params,
		api.EphemeralJobClaimRequest{RunnerId: "echopoint-cli", BootId: bootID},
	)
	if err != nil {
		return jobrunner.Job{}, "", fmt.Errorf(
			"claim ephemeral Job: %w; inspect execution %s before retrying", err, executionID,
		)
	}
	if response.JSON200 == nil {
		return jobrunner.Job{}, "", fmt.Errorf(
			"claim ephemeral Job: status %d: %s; inspect execution %s before retrying",
			response.StatusCode(), string(response.Body), executionID,
		)
	}
	// The generated API and embedded runner share the same JSON Job contract.
	encoded, err := json.Marshal(response.JSON200.Job)
	if err != nil {
		return jobrunner.Job{}, "", fmt.Errorf("encode claimed Job: %w", err)
	}
	var job jobrunner.Job
	if err := json.Unmarshal(encoded, &job); err != nil {
		return jobrunner.Job{}, "", fmt.Errorf("decode claimed Job: %w", err)
	}
	return job, response.JSON200.JobToken, nil
}

func buildRunResultFromJob(flowID, executionID string, runnerResult jobrunner.Result) FlowRunResult {
	success := runnerResult.Status == statusCompleted &&
		runnerResult.Execution != nil && runnerResult.Execution.Success
	code := exitFlowFailed
	status := statusFailed
	if success {
		code = exitSuccess
		status = statusCompleted
	}
	report := FlowRunResult{
		ExecutionID:  executionID,
		FlowID:       flowID,
		Status:       status,
		Success:      success,
		ExitCode:     code,
		ErrorMessage: runnerResult.ErrorMessage,
		Nodes:        []FlowRunNode{},
	}
	if runnerResult.Execution == nil {
		return report
	}
	report.DurationMs = runnerResult.Execution.DurationMS
	if report.ErrorMessage == nil {
		report.ErrorMessage = runnerResult.Execution.ErrorMsg
	}
	encoded, err := json.Marshal(runnerResult.Execution)
	if err != nil {
		return report
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		return report
	}
	report.Nodes = buildJobNodeList(payload)
	attachAssertions(report.Nodes, payload)
	return report
}

func buildJobNodeList(result map[string]any) []FlowRunNode {
	raw, ok := result["execution_results"].(map[string]any)
	if !ok {
		return []FlowRunNode{}
	}
	nodes := make([]FlowRunNode, 0, len(raw))
	for nodeID, value := range raw {
		node, ok := value.(map[string]any)
		if !ok {
			continue
		}
		status := statusCompleted
		if node["skip_reason"] != nil {
			status = "skipped"
		} else if node["error_message"] != nil {
			status = statusFailed
		}
		var errorMessage *string
		if message, ok := node["error_message"].(string); ok {
			errorMessage = &message
		}
		name, _ := node["display_name"].(string)
		nodeType, _ := node["node_type"].(string)
		var durationMs *int
		if value, ok := node["duration_ms"].(float64); ok {
			duration := int(value)
			durationMs = &duration
		}
		nodes = append(nodes, FlowRunNode{
			NodeID: nodeID, DisplayName: name, NodeType: nodeType,
			Status: status, DurationMs: durationMs, ErrorMsg: errorMessage,
		})
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].NodeID < nodes[j].NodeID })
	return nodes
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

// attachAssertions adds the runner's per-assertion outcomes to local node summaries.
func attachAssertions(nodes []FlowRunNode, result map[string]any) {
	if result == nil {
		return
	}
	byNode := extractAssertionsByNode(result)
	for i := range nodes {
		if assertions, ok := byNode[nodes[i].NodeID]; ok {
			nodes[i].Assertions = assertions
		}
	}
}

func extractAssertionsByNode(result map[string]any) map[string][]AssertionSummary {
	executionResults, ok := result["execution_results"].(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string][]AssertionSummary, len(executionResults))
	for nodeID, raw := range executionResults {
		nodeMap, ok := raw.(map[string]any)
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
		ErrorMessage: new(msg),
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

func buildJSONOutput(results []FlowRunResult, exitCode int, numFlows int) any {
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

func buildErrorJSON(results []FlowRunResult, exitCode int, msg string) any {
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
		ErrorMessage: new(msg),
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

func progressf(outputFormat, format string, args ...any) {
	if strings.ToLower(strings.TrimSpace(outputFormat)) != outputFormatJSON {
		fmt.Fprintf(os.Stderr, format, args...)
	}
}
