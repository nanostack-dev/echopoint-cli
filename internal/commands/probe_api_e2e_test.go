//go:build integration

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/client"
)

// TestProbeAPIConfigurationWorkflow runs the built CLI against an owned local
// API, not a stub. Supply a disposable organization without an existing page.
func TestProbeAPIConfigurationWorkflow(t *testing.T) {
	targetURL, cli := newProbeE2ECLI(t)
	suffix := fmt.Sprint(time.Now().UnixNano())
	environment := "probe-cli-" + suffix
	cli("org", "env", "environments", "create", environment)
	t.Cleanup(func() { cli("org", "env", "environments", "delete", environment, "--yes") })
	flowFile := writeTemp(t, "flow.json", fmt.Sprintf(`{
  "name":"CLI probe E2E %s",
  "flow_definition":{"name":"Health capability","version":"1.0","nodes":[{
    "id":"health","display_name":"Capability HTTP check","type":"request",
    "data":{"method":"GET","url":%q,"timeout":5000},
    "assertions":[{"extractor_type":"status_code","extractor_data":{},"operator_type":"equals","operator_data":{"value":200}}]
  }],"edges":[]}
}`, suffix, targetURL))
	var flow api.Flow
	decodeProbeE2E(t, cli("flow", "create", "-f", flowFile), &flow)
	t.Cleanup(func() { cli("flow", "delete", flow.Id.String(), "--yes") })
	var published api.FlowVersion
	decodeProbeE2E(t, cli("flow", "publish", flow.Id.String()), &published)
	var pin api.FlowVersion
	decodeProbeE2E(t, cli("flow", "version", "view", flow.Id.String(), published.Id.String()), &pin)
	if pin.Id != published.Id {
		t.Fatal("published pin did not round trip")
	}
	var source api.ProbeSourceOptions
	decodeProbeE2E(
		t,
		cli(
			"probe",
			"source-options",
			"--flow-id",
			flow.Id.String(),
			"--version-id",
			published.Id.String(),
			"--environment",
			environment,
		),
		&source,
	)
	if source.EnvironmentKey != environment || len(source.Checks) != 1 || source.Checks[0].NodeId != "health" ||
		source.Checks[0].AssertionIndex != 0 {
		t.Fatalf("source = %+v", source)
	}
	config := api.ProbeConfig{
		Name:             "CLI capability " + suffix,
		FlowId:           flow.Id,
		VersionId:        published.Id,
		EnvironmentKey:   environment,
		RunnerType:       api.ProbeRunnerCloud,
		Enabled:          false,
		IntervalSeconds:  60,
		TimeoutSeconds:   30,
		ConfirmationRuns: 2,
		RecoveryRuns:     2,
		FreshnessSeconds: 180,
		Capabilities: []api.ProbeCapability{
			{
				Id:        "api",
				Name:      "API availability",
				Enabled:   true,
				Checks:    []api.ProbeCheck{{NodeId: "health", AssertionIndex: 0}},
				DependsOn: []string{},
			},
		},
	}
	createFile := probeE2EFile(t, "probe.json", api.CreateProbeRequest{Config: config})
	cli("probe", "validate", "-f", createFile)
	var probe api.Probe
	decodeProbeE2E(t, cli("probe", "create", "-f", createFile, "--environment", environment), &probe)
	t.Cleanup(func() {
		var current api.Probe
		decodeProbeE2E(t, cli("probe", "view", probe.Id), &current)
		cli("probe", "delete", probe.Id, "--expected-revision", fmt.Sprint(current.Revision), "--yes")
	})
	var reloaded api.Probe
	decodeProbeE2E(t, cli("probe", "view", probe.Id), &reloaded)
	if reloaded.Config.VersionId != published.Id || reloaded.Config.EnvironmentKey != environment ||
		reloaded.Revision != probe.Revision {
		t.Fatal("durable probe source did not round trip")
	}
	assertProbeE2EDiagnostic(t, cli, probe, published, environment)
	config.Name += " updated"
	updateFile := probeE2EFile(
		t,
		"update.json",
		api.UpdateProbeRequest{ExpectedRevision: reloaded.Revision, Config: config},
	)
	cli("probe", "validate", "--update", "-f", updateFile)
	decodeProbeE2E(t, cli("probe", "update", probe.Id, "-f", updateFile), &probe)
	if probe.Config.Name != config.Name || probe.Revision <= reloaded.Revision {
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
	pageRequest.ProbeBindings = &[]api.StatusPageProbeBinding{
		{ServiceId: "api", ProbeId: probe.Id, CapabilityId: "api"},
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
		(*pageReadBack.ProbeBindings)[0].ProbeId != probe.Id {
		t.Fatal("status page mapping did not persist")
	}
	awaitProbeE2EPublicHealth(t, cli, page.Slug)
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
	decodeProbeE2E(t, cli("probe", "pause", probe.Id, "--expected-revision", fmt.Sprint(probe.Revision)), &probe)
	if probe.Config.Enabled {
		t.Fatal("pause did not persist")
	}
	t.Log(
		"CLI/API configuration, source pin, diagnostic isolation, scheduled confirmation, persistence and lifecycle verified. Cleanup removes probe/flow/environment and withdraws publication; the private draft remains in the disposable organization because no page deletion API exists.",
	)
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
	published api.FlowVersion,
	environment string,
) {
	t.Helper()
	var reloaded api.Probe
	var diagnostic api.ProbeRun
	decodeProbeE2E(t, cli("probe", "run", probe.Id, "--expected-revision", fmt.Sprint(probe.Revision)), &diagnostic)
	diagnostic = awaitProbeE2ERun(t, cli, probe.Id, diagnostic.Id)
	if diagnostic.Origin != api.ProbeDiagnostic || diagnostic.Config.VersionId != published.Id ||
		diagnostic.Config.EnvironmentKey != environment ||
		diagnostic.ExecutionId == nil ||
		len(diagnostic.Steps) != 1 ||
		len(diagnostic.Steps[0].Assertions) != 1 ||
		diagnostic.Steps[0].Assertions[0].Passed == nil ||
		!*diagnostic.Steps[0].Assertions[0].Passed {
		logProbeE2ENodeErrors(t, diagnostic)
		t.Fatalf("real diagnostic evidence missing: %+v", diagnostic)
	}
	decodeProbeE2E(t, cli("probe", "view", probe.Id), &reloaded)
	if len(reloaded.Health) != 1 || reloaded.Health[0].State != api.ProbeNotMonitored {
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
	if run.ExecutionId == nil {
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
		run.Config.FlowId,
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

func decodeProbeE2E(t *testing.T, value string, target any) {
	t.Helper()
	if err := json.Unmarshal([]byte(value), target); err != nil {
		t.Fatalf("decode CLI JSON: %v", err)
	}
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

func awaitProbeE2EPublicHealth(t *testing.T, cli func(...string) string, slug string) {
	t.Helper()
	deadline := time.Now().Add(165 * time.Second)
	for time.Now().Before(deadline) {
		result := cli("status-page", "public", slug)
		var value api.PublicStatusView
		decodeProbeE2E(t, result, &value)
		for _, private := range []string{"probe_id", "flow_id", "execution_id", "node_id", "assertion_index", "expected_revision", "extractor_data", "operator_data", "source_fingerprint"} {
			if strings.Contains(result, `"`+private+`"`) {
				t.Fatalf("public view leaked private field %s", private)
			}
		}
		if value.Health.State == api.StatusPageHealthStateOperational && len(value.Services) == 1 &&
			value.Services[0].Health.State == api.StatusPageHealthStateOperational {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatal("scheduled probe evidence did not establish public Operational health within 165 seconds")
}
