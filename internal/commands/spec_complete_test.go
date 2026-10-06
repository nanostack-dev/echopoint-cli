package commands

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"echopoint-cli/internal/api"
)

func completionServer(t *testing.T) (*specServer, string) {
	t.Helper()
	fake, server := newSpecServer(t)
	users := specOf("users-api", "Users\tAPI\nv2", "2.0.0")
	fake.responses["GET /specs"] = api.SpecListResponse{
		Count: 2, Total: 2, Items: []api.Spec{specOf("pets-api", "Pets", "1.4.0"), users},
	}
	fake.responses["GET /specs/pets-api/versions"] = api.SpecVersionListResponse{
		Count: 2, Total: 2, Items: []api.SpecVersionSummary{
			versionOf(
				"1.4.0",
				"minor",
				api.SpecChangeCounts{},
				nil,
			),
			versionOf("1.3.0", "patch", api.SpecChangeCounts{}, nil),
		},
	}
	return fake, server.URL
}

func TestCompleteSpecSlugsListsTheSpecsWithTheirLiveVersionAndTitle(t *testing.T) {
	fake, url := completionServer(t)
	state := makeState(t, "test-api-key", "", url)

	names, directive := completeSpecSlugs(state)(&cobra.Command{}, nil, "")

	want := []string{"pets-api\tLive 1.4.0 · Pets", "users-api\tLive 2.0.0 · Users API v2"}
	if !slices.Equal(names, want) || directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("names %q, directive %v", names, directive)
	}
	if len(fake.requests) != 1 || !strings.HasPrefix(fake.requests[0], "GET /specs?") {
		t.Errorf("requests = %v", fake.requests)
	}
}

func TestCompleteSpecSlugsFiltersByWhatIsTyped(t *testing.T) {
	_, url := completionServer(t)
	state := makeState(t, "test-api-key", "", url)

	names, _ := completeSpecSlugs(state)(&cobra.Command{}, nil, "us")

	if len(names) != 1 || !strings.HasPrefix(names[0], "users-api\t") {
		t.Errorf("names = %q", names)
	}
}

func TestCompleteSpecSlugsOnlyCompletesTheFirstArgument(t *testing.T) {
	fake, url := completionServer(t)
	state := makeState(t, "test-api-key", "", url)

	names, directive := completeSpecSlugs(state)(&cobra.Command{}, []string{"pets-api"}, "")

	if len(names) != 0 || directive != cobra.ShellCompDirectiveNoFileComp || len(fake.requests) != 0 {
		t.Errorf("names %q, directive %v, requests %v", names, directive, fake.requests)
	}
}

func TestCompleteSpecSlugsCompletesNothingOnAnyFailure(t *testing.T) {
	_, url := completionServer(t)
	failing, failingServer := newSpecServer(t)
	failing.responses["GET /specs"] = apiError("INTERNAL", "boom")
	failing.statuses["GET /specs"] = http.StatusInternalServerError
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	noCredentials := makeState(t, "test-api-key", "", url)
	noCredentials.APIKey, noCredentials.Token = "", ""
	brokenConfigure := makeState(t, "test-api-key", "", url)
	brokenConfigure.Configure = func(*cobra.Command) error { return http.ErrAbortHandler }

	for name, state := range map[string]*AppState{
		"server error":    makeState(t, "test-api-key", "", failingServer.URL),
		"unreachable":     makeState(t, "test-api-key", "", closed.URL),
		"no credentials":  noCredentials,
		"configure fails": brokenConfigure,
		"no client":       {APIKey: "k"},
	} {
		names, directive := completeSpecSlugs(state)(&cobra.Command{}, nil, "")
		if len(names) != 0 || directive != cobra.ShellCompDirectiveNoFileComp {
			t.Errorf("%s: names %q, directive %v", name, names, directive)
		}
	}
}

func TestCompleteSpecSlugsGivesUpAfterTheTimeout(t *testing.T) {
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer slow.Close()
	defer close(release)
	previous := completionTimeout
	completionTimeout = 50 * time.Millisecond
	t.Cleanup(func() { completionTimeout = previous })
	state := makeState(t, "test-api-key", "", slow.URL)

	started := time.Now()
	names, _ := completeSpecSlugs(state)(&cobra.Command{}, nil, "")

	if len(names) != 0 || time.Since(started) > 2*time.Second {
		t.Errorf("names %q after %v", names, time.Since(started))
	}
}

func TestCompleteSpecVersionsListsTheVersionsOfTheSpecAlreadyTyped(t *testing.T) {
	fake, url := completionServer(t)
	state := makeState(t, "test-api-key", "", url)

	versions, directive := completeSpecVersions(state)(&cobra.Command{}, []string{"pets-api"}, "1.")

	want := []string{"1.4.0\tLive · minor · 2026-10-03 12:00", "1.3.0\tpatch · 2026-10-03 12:00"}
	if !slices.Equal(versions, want) || directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("versions %q, directive %v", versions, directive)
	}
	if len(fake.requests) != 1 || !strings.HasPrefix(fake.requests[0], "GET /specs/pets-api/versions?") {
		t.Errorf("requests = %v", fake.requests)
	}
}

func TestCompleteSpecVersionsNeedsTheSpecNameFirst(t *testing.T) {
	fake, url := completionServer(t)
	state := makeState(t, "test-api-key", "", url)

	versions, _ := completeSpecVersions(state)(&cobra.Command{}, nil, "")

	if len(versions) != 0 || len(fake.requests) != 0 {
		t.Errorf("versions %q, requests %v", versions, fake.requests)
	}
}

func TestEverySpecCommandCompletesTheNameAsItsFirstArgument(t *testing.T) {
	_, url := completionServer(t)
	state := makeState(t, "test-api-key", "", url)
	spec := newSpecCmd(state)

	for _, leaf := range specLeaves {
		cmd, _, err := spec.Find(leaf.command)
		if err != nil {
			t.Fatal(err)
		}
		if cmd.ValidArgsFunction == nil {
			t.Errorf("%v has no completion", leaf.command)
			continue
		}
		names, directive := cmd.ValidArgsFunction(cmd, nil, "")
		if len(names) != 2 || directive != cobra.ShellCompDirectiveNoFileComp {
			t.Errorf("%v: names %q, directive %v", leaf.command, names, directive)
		}
		if more, _ := cmd.ValidArgsFunction(cmd, []string{"pets-api"}, ""); len(more) != 0 {
			t.Errorf("%v completes a second argument: %q", leaf.command, more)
		}
	}
}

func TestFlagsThatTakeAFileCompleteYAMLAndJSONFilenames(t *testing.T) {
	spec := newSpecCmd(&AppState{})
	for _, leaf := range specLeaves {
		if !leaf.takesF {
			continue
		}
		cmd, _, err := spec.Find(leaf.command)
		if err != nil {
			t.Fatal(err)
		}
		extensions, ok := cmd.Flags().Lookup("file").Annotations[cobra.BashCompFilenameExt]
		if !ok || !slices.Equal(extensions, []string{"yaml", "yml", "json"}) {
			t.Errorf("%v: file extensions = %v", leaf.command, extensions)
		}
	}
}

func TestVersionFlagsCompleteFromTheSpecTyped(t *testing.T) {
	_, url := completionServer(t)
	spec := newSpecCmd(makeState(t, "test-api-key", "", url))

	for command, flag := range map[string]string{"pull": "version", "lint": "version", "diff": "from"} {
		cmd, _, err := spec.Find([]string{command})
		if err != nil {
			t.Fatal(err)
		}
		complete, ok := cmd.GetFlagCompletionFunc(flag)
		if !ok {
			t.Fatalf("%s --%s has no completion", command, flag)
		}
		versions, _ := complete(cmd, []string{"pets-api"}, "")
		if len(versions) != 2 {
			t.Errorf("%s --%s: versions = %q", command, flag, versions)
		}
	}
	diff, _, _ := spec.Find([]string{"diff"})
	if _, ok := diff.GetFlagCompletionFunc("to"); !ok {
		t.Error("diff --to has no completion")
	}
}

func completeThroughTheRoot(t *testing.T, args ...string) (string, string) {
	t.Helper()
	root := NewRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{cobra.ShellCompRequestCmd}, args...))
	if err := root.Execute(); err != nil {
		t.Fatalf("__complete %v: %v", args, err)
	}
	return stdout.String(), stderr.String()
}

func isolateConfig(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ECHOPOINT_CONFIG", home+"/config.yaml")
	for _, name := range []string{
		"ECHOPOINT_API_URL", "ECHOPOINT_API_KEY", "ECHOPOINT_TOKEN", "ECHOPOINT_ORGANIZATION_ID", "ECHOPOINT_PROFILE",
	} {
		t.Setenv(name, "")
	}
}

func TestCompletionThroughTheRootUsesTheNormalAuth(t *testing.T) {
	_, url := completionServer(t)
	isolateConfig(t)
	t.Setenv("ECHOPOINT_API_URL", url)
	t.Setenv("ECHOPOINT_API_KEY", "key")
	t.Setenv("ECHOPOINT_ORGANIZATION_ID", "org")

	stdout, _ := completeThroughTheRoot(t, "spec", "view", "")

	if want := "pets-api\tLive 1.4.0 · Pets\nusers-api\tLive 2.0.0 · Users API v2\n:4\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestCompletionThroughTheRootHonorsTheFlagsTypedSoFar(t *testing.T) {
	_, url := completionServer(t)
	isolateConfig(t)

	stdout, _ := completeThroughTheRoot(t, "--api-url", url, "--api-key", "key", "--organization-id", "org",
		"spec", "versions", "pe")

	if want := "pets-api\tLive 1.4.0 · Pets\n:4\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestCompletionThroughTheRootOfAVersionFlag(t *testing.T) {
	_, url := completionServer(t)
	isolateConfig(t)
	t.Setenv("ECHOPOINT_API_URL", url)
	t.Setenv("ECHOPOINT_API_KEY", "key")
	t.Setenv("ECHOPOINT_ORGANIZATION_ID", "org")

	stdout, _ := completeThroughTheRoot(t, "spec", "pull", "pets-api", "--version", "")

	if !strings.Contains(stdout, "1.4.0\tLive · minor") || !strings.Contains(stdout, "1.3.0\tpatch") {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestCompletionThroughTheRootOfTheFileFlagFiltersByExtension(t *testing.T) {
	isolateConfig(t)

	stdout, _ := completeThroughTheRoot(t, "spec", "push", "pets-api", "-f", "")

	if want := "yaml\nyml\njson\n:8\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestCompletionWithoutCredentialsCompletesNothingAndPrintsNothing(t *testing.T) {
	isolateConfig(t)

	stdout, stderr := completeThroughTheRoot(t, "spec", "lint", "")

	if stdout != ":4\n" {
		t.Errorf("stdout = %q", stdout)
	}
	if strings.Contains(stderr, "Error") || strings.Contains(stderr, "authentication") {
		t.Errorf("stderr = %q", stderr)
	}
}
