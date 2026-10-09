//go:build integration

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/client"
)

// runCLIWithContext permits cleanup commands after t.Context is canceled.
// Callers must supply a bounded context; the binary still builds once per suite.
func runCLIWithContext(t *testing.T, ctx context.Context, args ...string) (string, string, int) {
	t.Helper()
	stdout, stderr, code := runCLIWithLogsContext(t, ctx, args...)
	return stdout, withoutLibraryLogs(stderr), code
}

// TestProbeAPIConfigurationWorkflow runs the built CLI against an owned local
// API, not a stub. Supply a disposable organization without an existing page.
func TestProbeAPIConfigurationWorkflow(t *testing.T) {
	targetURL, cli := newProbeE2ECLI(t)
	suffix := fmt.Sprint(time.Now().UnixNano())
	environment := "probe-cli-" + suffix
	cli("org", "env", "environments", "create", environment)
	t.Cleanup(func() { cli("org", "env", "environments", "delete", environment, "--yes") })
	tag := "probe-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:14]
	flows := make([]api.Flow, 0, 2)
	for i := range 2 {
		flowFile := writeTemp(t, fmt.Sprintf("flow-%d.json", i), fmt.Sprintf(`{
  "name":"CLI probe E2E %s %d", "tags":[%q],
  "flow_definition":{"name":"Service availability","version":"1.0","nodes":[{
    "id":"health","display_name":"Availability HTTP request","type":"request",
    "data":{"method":"GET","url":%q,"timeout":5000},
    "assertions":[{"extractor_type":"status_code","extractor_data":{},"operator_type":"equals","operator_data":{"value":200}}]
  }],"edges":[]}
}`, suffix, i, tag, targetURL))
		var flow api.Flow
		decodeProbeE2E(t, cli("flow", "create", "-f", flowFile), &flow)
		flows = append(flows, flow)
		t.Cleanup(func() { cli("flow", "delete", flow.Id.String(), "--yes") })
	}
	flowIDs := []uuid.UUID{flows[0].Id, flows[1].Id}
	// No publication or version selection is necessary for a monitor-backed probe.
	var estimate api.ProbeEstimate
	decodeProbeE2E(
		t,
		cli(
			"probe",
			"estimate",
			"--flow-id",
			flowIDs[0].String(),
			"--flow-id",
			flowIDs[1].String(),
			"--environment",
			environment,
		),
		&estimate,
	)
	assertProbeE2EForecast(t, estimate)
	config := api.ProbeConfig{
		Name:             "CLI availability " + suffix,
		FlowIds:          &flowIDs,
		EnvironmentKey:   environment,
		RunnerType:       api.ProbeRunnerCloud,
		Enabled:          false,
		IntervalSeconds:  60,
		TimeoutSeconds:   30,
		ConfirmationRuns: 2,
		RecoveryRuns:     2,
		FreshnessSeconds: 180,
	}
	createFile := probeE2EFile(t, "probe.json", api.CreateProbeRequest{Config: config})
	cli("probe", "validate", "-f", createFile)
	var probe api.Probe
	decodeProbeE2E(
		t,
		cli(
			"probe",
			"create",
			"--name",
			config.Name,
			"--flow-id",
			flowIDs[0].String(),
			"--flow-id",
			flowIDs[1].String(),
			"--environment",
			environment,
			"--paused",
		),
		&probe,
	)
	t.Cleanup(func() {
		var current api.Probe
		decodeProbeE2E(t, cli("probe", "view", probe.Id), &current)
		cli("probe", "delete", probe.Id, "--expected-revision", fmt.Sprint(current.Revision), "--yes")
	})
	var reloaded api.Probe
	decodeProbeE2E(t, cli("probe", "view", probe.Id), &reloaded)
	if reloaded.Config.FlowIds == nil || !sameProbeE2EFlowIDs(*reloaded.Config.FlowIds, flowIDs) ||
		reloaded.Config.EnvironmentKey != environment ||
		reloaded.Revision != probe.Revision ||
		reloaded.ScheduleId == uuid.Nil {
		t.Fatal("durable probe source did not round trip")
	}
	assertProbeE2EMonitor(t, probe, flowIDs)
	assertProbeE2EDiagnostic(t, cli, probe, flowIDs, environment)
	config.Name += " updated"
	config.FlowIds = nil
	tags := []string{tag}
	mode := api.All
	config.Tags, config.TagMatchMode = &tags, &mode
	decodeProbeE2E(
		t,
		cli("probe", "estimate", "--tag", tag, "--match-mode", "all", "--environment", environment),
		&estimate,
	)
	assertProbeE2EForecast(t, estimate)
	updateFile := probeE2EFile(
		t,
		"update.json",
		api.UpdateProbeRequest{ExpectedRevision: reloaded.Revision, Config: config},
	)
	cli("probe", "validate", "--update", "-f", updateFile)
	decodeProbeE2E(t, cli("probe", "update", probe.Id, "-f", updateFile), &probe)
	if probe.Config.Name != config.Name || probe.Revision <= reloaded.Revision || probe.Config.FlowIds != nil ||
		probe.Config.Tags == nil ||
		!slices.Equal(*probe.Config.Tags, tags) {
		t.Fatal("update did not persist a new revision")
	}
	decodeProbeE2E(t, cli("probe", "resume", probe.Id, "--expected-revision", fmt.Sprint(probe.Revision)), &probe)
	if !probe.Config.Enabled {
		t.Fatal("resume did not enable scheduling")
	}
	pageBytes, err := os.ReadFile("testdata/status-page.json")
	if err != nil {
		t.Fatal(err)
	}
	var pageRequest api.SaveStatusPageRequest
	decodeProbeE2E(t, string(pageBytes), &pageRequest)
	pageRequest.Slug = "probe-cli-" + suffix
	pageRequest.Config.BrandName = "Disposable CLI verification"
	operational, outage, unknown := "Checkout is available", "Checkout is unavailable", "We are checking checkout"
	pageRequest.Config.Services[0].Messages = &api.StatusPageServiceMessages{
		Operational: &operational,
		Outage:      &outage,
		Unknown:     &unknown,
	}
	pageRequest.ProbeBindings = &[]api.StatusPageProbeBinding{
		{ServiceId: "api", ProbeId: probe.Id},
	}
	pageFile := probeE2EFile(t, "page.json", pageRequest)
	cli("status-page", "validate", "-f", pageFile)
	var page api.StatusPageEditor
	decodeProbeE2E(t, cli("status-page", "save", "-f", pageFile), &page)
	decodeProbeE2E(
		t,
		cli(
			"status-page",
			"publish",
			"--expected-draft-version",
			fmt.Sprint(page.DraftVersion),
			"--expected-intent-version",
			fmt.Sprint(page.IntentVersion),
		),
		&page,
	)
	t.Cleanup(func() {
		var latest api.StatusPageEditor
		decodeProbeE2E(t, cli("status-page", "view"), &latest)
		cli("status-page", "unpublish", "--expected-intent-version", fmt.Sprint(latest.IntentVersion), "--yes")
	})
	var pageReadBack api.StatusPageEditor
	decodeProbeE2E(t, cli("status-page", "view"), &pageReadBack)
	if pageReadBack.ProbeBindings == nil || len(*pageReadBack.ProbeBindings) != 1 ||
		(*pageReadBack.ProbeBindings)[0].ProbeId != probe.Id ||
		!reflect.DeepEqual(pageReadBack.Config.Services[0].Messages, pageRequest.Config.Services[0].Messages) {
		t.Fatal("status page mapping did not persist")
	}
	// A draft update must not change the published service's messages.
	pageRequest.ExpectedDraftVersion = pageReadBack.DraftVersion
	pageRequest.Slug = pageReadBack.Slug
	pageRequest.Config.Services[0].Messages = nil
	pageFile = probeE2EFile(t, "default-page.json", pageRequest)
	cli("status-page", "validate", "-f", pageFile)
	decodeProbeE2E(t, cli("status-page", "save", "-f", pageFile), &page)
	awaitProbeE2EPublicHealth(t, cli, page.Slug, pageReadBack.Config.Services[0].Messages)
	var history api.ProbeRunListResponse
	decodeProbeE2E(t, cli("probe", "history", probe.Id), &history)
	var confirmed int
	for _, run := range history.Items {
		if run.Origin == api.ProbeScheduled && run.Revision == probe.Revision &&
			run.Status == api.ProbeCompleted {
			confirmed++
		}
	}
	if confirmed < 2 {
		t.Fatal("public health lacked two distinct scheduled occurrences")
	}
	decodeProbeE2E(t, cli("status-page", "publish", "--expected-draft-version", fmt.Sprint(page.DraftVersion), "--expected-intent-version", fmt.Sprint(page.IntentVersion)), &page)
	awaitProbeE2EPublicHealth(t, cli, page.Slug, nil)
	decodeProbeE2E(t, cli("probe", "pause", probe.Id, "--expected-revision", fmt.Sprint(probe.Revision)), &probe)
	if probe.Config.Enabled {
		t.Fatal("pause did not persist")
	}
	awaitProbeE2EPausedPublicHealth(t, cli, page.Slug)
	t.Log(
		"CLI/API explicit flow set, tag selector, request forecast, shared monitor linkage/type, single health result, diagnostic isolation, scheduled confirmation, direct status bindings, frozen public messages, draft isolation, omitted-message defaults and paused Unknown verified. Cleanup removes probe/flows/environment and withdraws publication; the private draft remains in the disposable organization because no page deletion API exists.",
	)
}

func sameProbeE2EFlowIDs(got, want []uuid.UUID) bool {
	if len(got) != len(want) {
		return false
	}
	for _, id := range want {
		if !slices.Contains(got, id) {
			return false
		}
	}
	return true
}

func assertProbeE2EForecast(t *testing.T, estimate api.ProbeEstimate) {
	t.Helper()
	if estimate.MatchedFlowCount != 2 || len(estimate.Flows) != 2 || estimate.ExecutionsPerOccurrence != 2 ||
		estimate.ExecutionsPerDay != 2880 ||
		estimate.ExecutionsPer30Days != 86400 ||
		!estimate.RequestsExact ||
		estimate.RequestsPerOccurrence == nil ||
		*estimate.RequestsPerOccurrence != 2 ||
		estimate.RequestsPerDay == nil ||
		*estimate.RequestsPerDay != 2880 ||
		estimate.RequestsPer30Days == nil ||
		*estimate.RequestsPer30Days != 86400 {
		t.Fatalf("forecast not based on both flows at 60-second cadence: %+v", estimate)
	}
}

func assertProbeE2EMonitor(t *testing.T, probe api.Probe, flowIDs []uuid.UUID) {
	t.Helper()
	c, err := client.NewWithAPIKey(
		os.Getenv("ECHOPOINT_PROBE_E2E_API_URL"),
		os.Getenv("ECHOPOINT_PROBE_E2E_API_KEY"),
		os.Getenv("ECHOPOINT_PROBE_E2E_ORG_ID"),
		10*time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	response, err := c.API().
		GetFlowScheduleWithResponse(t.Context(), probe.ScheduleId, &api.GetFlowScheduleParams{XOrganizationID: os.Getenv("ECHOPOINT_PROBE_E2E_ORG_ID")})
	if err != nil || response.JSON200 == nil {
		t.Fatalf("monitor read unavailable: %v", err)
	}
	schedule := response.JSON200
	if schedule.Type != api.FlowScheduleTypeProbe || schedule.ProbeId == nil || *schedule.ProbeId != probe.Id ||
		schedule.IntervalSeconds != 60 ||
		!sameProbeE2EFlowIDs(schedule.FlowIds, flowIDs) ||
		schedule.EnvironmentKey != probe.Config.EnvironmentKey ||
		schedule.Enabled {
		t.Fatal("probe was not backed by the configured shared monitor")
	}
}

func newProbeE2ECLI(t *testing.T) (string, func(...string) string) {
	t.Helper()
	apiURL, key, org := os.Getenv(
		"ECHOPOINT_PROBE_E2E_API_URL",
	), os.Getenv(
		"ECHOPOINT_PROBE_E2E_API_KEY",
	), os.Getenv(
		"ECHOPOINT_PROBE_E2E_ORG_ID",
	)
	parsed, err := url.Parse(apiURL)
	if err != nil ||
		(parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost" && parsed.Hostname() != "::1") ||
		key == "" ||
		org == "" {
		t.Fatal(
			"set ECHOPOINT_PROBE_E2E_API_URL to the owned loopback API and provide ECHOPOINT_PROBE_E2E_API_KEY / ECHOPOINT_PROBE_E2E_ORG_ID for a disposable organization",
		)
	}
	t.Setenv("ECHOPOINT_API_KEY", key)
	t.Setenv("ECHOPOINT_API_URL", apiURL)
	t.Setenv("ECHOPOINT_TOKEN", "")
	t.Setenv("ECHOPOINT_OUTPUT_FORMAT", "json")
	targetURL := os.Getenv("ECHOPOINT_PROBE_E2E_TARGET_URL")
	target, err := url.Parse(targetURL)
	if err != nil || target.Scheme != "https" || target.Hostname() == "" || target.User != nil {
		t.Fatal("set ECHOPOINT_PROBE_E2E_TARGET_URL to an explicit public HTTPS target reachable by Cloud jobs")
	}
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cli := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		stdout, stderr, code := runCLIWithContext(
			t,
			ctx,
			append([]string{"--config", configPath, "--org", org}, args...)...)
		if code != 0 {
			t.Fatalf("CLI %s: exit %d: %s", strings.Join(args, " "), code, stderr)
		}
		return stdout
	}
	_, stderr, code := runCLI(t, "--config", configPath, "--org", org, "status-page", "view")
	if code != 1 || !strings.Contains(stderr, "404") {
		t.Fatal("disposable organization must not have an existing status page")
	}
	return targetURL, cli
}

func assertProbeE2EDiagnostic(
	t *testing.T,
	cli func(...string) string,
	probe api.Probe,
	flowIDs []uuid.UUID,
	environment string,
) {
	t.Helper()
	var reloaded api.Probe
	var diagnostic api.ProbeRun
	decodeProbeE2E(t, cli("probe", "run", probe.Id, "--expected-revision", fmt.Sprint(probe.Revision)), &diagnostic)
	diagnostic = awaitProbeE2ERun(t, cli, probe.Id, diagnostic.Id)
	if diagnostic.Origin != api.ProbeDiagnostic || diagnostic.Config.FlowIds == nil ||
		!sameProbeE2EFlowIDs(*diagnostic.Config.FlowIds, flowIDs) ||
		diagnostic.Config.EnvironmentKey != environment ||
		diagnostic.ScheduleRunId == uuid.Nil ||
		len(diagnostic.Executions) != 2 ||
		len(diagnostic.Steps) != 2 {
		logProbeE2ENodeErrors(t, diagnostic)
		t.Fatalf("real diagnostic evidence missing: %+v", diagnostic)
	}
	for _, step := range diagnostic.Steps {
		if !slices.Contains(flowIDs, step.FlowId) || len(step.Assertions) != 1 || step.Assertions[0].Passed == nil ||
			!*step.Assertions[0].Passed {
			t.Fatal("selected flow assertion evidence missing")
		}
	}
	for _, execution := range diagnostic.Executions {
		if !slices.Contains(flowIDs, execution.FlowId) || execution.ExecutionId == uuid.Nil ||
			execution.Status != "completed" {
			t.Fatal("selected flow execution missing")
		}
	}
	decodeProbeE2E(t, cli("probe", "view", probe.Id), &reloaded)
	if reloaded.Health.State != api.ProbeNotMonitored || diagnostic.Observation.State != api.ProbeOperational {
		t.Fatal("diagnostic changed paused policy health")
	}
	var history api.ProbeRunListResponse
	decodeProbeE2E(t, cli("probe", "history", probe.Id), &history)
	if len(history.Items) != 1 || history.Items[0].Id != diagnostic.Id {
		t.Fatal("diagnostic history not durable")
	}
}

// Only transport error codes are logged, never raw node exchanges or headers.
func logProbeE2ENodeErrors(t *testing.T, run api.ProbeRun) {
	t.Helper()
	if run.ExecutionId == nil || len(run.Executions) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := client.NewWithAPIKey(
		os.Getenv("ECHOPOINT_PROBE_E2E_API_URL"),
		os.Getenv("ECHOPOINT_PROBE_E2E_API_KEY"),
		os.Getenv("ECHOPOINT_PROBE_E2E_ORG_ID"),
		10*time.Second,
	)
	if err != nil {
		t.Log("could not initialize node-error lookup")
		return
	}
	response, err := c.API().GetExecutionNodeResultsWithResponse(
		ctx,
		run.Executions[0].FlowId,
		*run.ExecutionId,
		&api.GetExecutionNodeResultsParams{XOrganizationID: os.Getenv("ECHOPOINT_PROBE_E2E_ORG_ID")},
	)
	if err != nil || response.JSON200 == nil {
		t.Log("node-error lookup unavailable")
		return
	}
	for _, node := range *response.JSON200 {
		if node.ErrorCode != nil {
			t.Logf("node %s status=%s error_code=%s", node.NodeId, node.Status, *node.ErrorCode)
		}
	}
}

func decodeProbeE2E[T any](t *testing.T, value string, target *T) {
	t.Helper()
	var decoded T
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		t.Fatalf("decode CLI JSON: %v", err)
	}
	*target = decoded
}

func probeE2EFile(t *testing.T, name string, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return writeTemp(t, name, string(data))
}

func awaitProbeE2ERun(t *testing.T, cli func(...string) string, probe, run string) api.ProbeRun {
	t.Helper()
	deadline := time.Now().Add(75 * time.Second)
	for time.Now().Before(deadline) {
		var value api.ProbeRun
		decodeProbeE2E(t, cli("probe", "execution", "view", probe, run), &value)
		if value.Status == api.ProbeCompleted || value.Status == api.ProbeUnavailable {
			return value
		}
		time.Sleep(time.Second)
	}
	t.Fatal("diagnostic did not finish within 75 seconds")
	return api.ProbeRun{}
}

func awaitProbeE2EPublicHealth(t *testing.T, cli func(...string) string, slug string, messages *api.StatusPageServiceMessages) {
	t.Helper()
	deadline := time.Now().Add(165 * time.Second)
	for time.Now().Before(deadline) {
		result := cli("status-page", "public", slug)
		var value api.PublicStatusView
		decodeProbeE2E(t, result, &value)
		assertProbeE2EPublicPrivacy(t, result)
		if len(value.Services) != 1 || !reflect.DeepEqual(value.Services[0].Messages, messages) {
			t.Fatal("published service messages changed before publication or did not round trip")
		}
		if value.Health.State == api.StatusPageHealthStateOperational && len(value.Services) == 1 &&
			value.Services[0].Health.State == api.StatusPageHealthStateOperational {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatal("scheduled probe evidence did not establish public Operational health within 165 seconds")
}

func awaitProbeE2EPausedPublicHealth(t *testing.T, cli func(...string) string, slug string) {
	t.Helper()
	// Public health is projected by the 30-second status evaluator, not on reads.
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		result := cli("status-page", "public", slug)
		assertProbeE2EPublicPrivacy(t, result)
		var value api.PublicStatusView
		decodeProbeE2E(t, result, &value)
		if len(value.Services) != 1 || value.Services[0].Messages != nil {
			t.Fatal("omitted public service messages did not retain defaults")
		}
		if value.Health.State == api.StatusPageHealthStateUnknown &&
			value.Services[0].Health.State == api.StatusPageHealthStateUnknown {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatal("paused probe public health did not become Unknown within 60 seconds")
}

func assertProbeE2EPublicPrivacy(t *testing.T, result string) {
	t.Helper()
	for _, private := range []string{"probe_id", "flow_id", "flow_ids", "execution_id", "schedule_id", "schedule_run_id", "node_id", "assertion_index", "expected_revision", "extractor_data", "operator_data", "source_fingerprint", "capability_id", "error_message"} {
		if strings.Contains(result, `"`+private+`"`) {
			t.Fatalf("public view leaked private field %s", private)
		}
	}
}
