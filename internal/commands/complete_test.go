package commands

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/config"
)

const noFileCompletionDirective = ":4"

// completeCLI asks the binary's own completion command what would follow args,
// and returns the candidates, each as "value" or "value\tdescription".
func completeCLI(t *testing.T, args ...string) []string {
	t.Helper()
	stdout, _, code := runCLI(t, append([]string{cobra.ShellCompRequestCmd}, args...)...)
	if code != 0 {
		t.Fatalf("__complete %q exited %d", args, code)
	}
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if lines[len(lines)-1] != noFileCompletionDirective {
		t.Fatalf("__complete %q ended with %q, want the no-file directive %q\n%s",
			args, lines[len(lines)-1], noFileCompletionDirective, stdout)
	}
	// cobra also offers the required flags it is not given yet
	var candidates []string
	for _, line := range lines[:len(lines)-1] {
		if !strings.HasPrefix(line, "-") {
			candidates = append(candidates, line)
		}
	}
	return candidates
}

func completionStub(t *testing.T) *apiStub {
	t.Helper()
	anchor, identity := idFor("10"), idFor("11")
	stub := newAPIStub(t)
	stub.onFlows(
		sampleFlow(idFor("1"), "Checkout", &identity),
		sampleFlow(idFor("2"), "Login\n  page", nil),
	)
	stub.on(
		http.MethodGet,
		"/flows/folders",
		http.StatusOK,
		api.FlowFolderListResponse{Count: 2, Total: 2, Items: []api.FlowFolder{
			sampleFolder(identity, "Identity", &anchor),
			sampleFolder(anchor, "Anchor", nil),
		}},
	)
	started := time.Date(2026, 10, 5, 14, 30, 0, 0, time.UTC)
	stub.on(http.MethodGet, "/flows/"+idFor("1").String()+"/executions", http.StatusOK, api.FlowExecutionListResponse{
		Count: 2, Total: 2, Items: []api.FlowExecution{
			{Id: idFor("20"), FlowId: idFor("1"), Status: "completed", StartedAt: started},
			{Id: idFor("21"), FlowId: idFor("1"), Status: "failed", StartedAt: started.Add(-time.Hour)},
		},
	})
	stub.on(
		http.MethodGet,
		"/collections",
		http.StatusOK,
		api.CollectionListResponse{Count: 1, Total: 1, Items: []api.Collection{
			{Id: idFor("30"), Name: "Payments API"},
		}},
	)
	stub.on(
		http.MethodGet,
		"/organization/environments",
		http.StatusOK,
		api.EnvironmentListResponse{Items: []api.Environment{
			{Id: idFor("40"), Name: "dev"}, {Id: idFor("41"), Name: "prd"},
		}},
	)
	isolateCLIEnvironment(t, stub.server.URL)
	return stub
}

func TestFlowArgumentsCompleteAsIDWithNameAndFolder(t *testing.T) {
	completionStub(t)
	want := []string{
		idFor("1").String() + "\tCheckout · Anchor/Identity",
		idFor("2").String() + "\tLogin page",
	}

	for _, args := range [][]string{
		{"flow", "view", ""},
		{"flows", "view", ""},
		{"flow", "update", ""},
		{"flow", "delete", ""},
		{"flow", "launch", ""},
		{"flow", "validate", ""},
		{"flow", "execution", "list", ""},
		{"flow", "execution", "view", ""},
		{"flow", "node", "add", ""},
		{"flow", "edge", "remove", ""},
		{"flow", "env", "view", ""},
		{"flow", "env", "get", ""},
		{"flow", "env", "set", ""},
		{"flow", "run", ""},
		{"flow", "tag", ""},
		{"flow", "move", ""},
	} {
		if got := completeCLI(t, args...); !slices.Equal(got, want) {
			t.Errorf("%v = %q, want %q", args, got, want)
		}
	}
}

func TestFlowArgumentsCompleteOnlyWhereAFlowGoes(t *testing.T) {
	completionStub(t)
	one := idFor("1").String()

	for _, args := range [][]string{
		{"flow", "view", one, ""},
		{"flow", "node", "add", one, ""},
		{"flow", "node", "remove", one, ""},
		{"flow", "env", "unset", one, ""},
		{"flow", "execution", "list", one, ""},
	} {
		if got := completeCLI(t, args...); len(got) != 0 {
			t.Errorf("%v = %q, want nothing after the flow", args, got)
		}
	}
	for _, args := range [][]string{{"flow", "run", one, ""}, {"flow", "tag", one, ""}, {"flow", "move", one, ""}} {
		if got := completeCLI(t, args...); len(got) != 2 {
			t.Errorf("%v = %q, want every flow again", args, got)
		}
	}
}

func TestFlowArgumentsFilterByWhatIsTyped(t *testing.T) {
	completionStub(t)

	got := completeCLI(t, "flow", "view", idFor("2").String())

	if len(got) != 1 || !strings.HasPrefix(got[0], idFor("2").String()+"\t") {
		t.Errorf("got %q", got)
	}
}

func TestExecutionIDsCompleteFromTheFlowAlreadyTyped(t *testing.T) {
	completionStub(t)

	want := []string{
		idFor("20").String() + "\tcompleted · 2026-10-05 14:30",
		idFor("21").String() + "\tfailed · 2026-10-05 13:30",
	}
	if got := completeCLI(t, "flow", "execution", "view", idFor("1").String(), ""); !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := completeCLI(t, "flow", "execution", "view", "not-a-flow", ""); len(got) != 0 {
		t.Errorf("a flow that does not resolve completed %q", got)
	}
	third := completeCLI(t, "flow", "execution", "view", idFor("1").String(), idFor("20").String(), "")
	if len(third) != 0 {
		t.Errorf("a third argument completed %q", third)
	}
}

func TestCollectionIDsCompleteWithTheirName(t *testing.T) {
	completionStub(t)
	want := []string{idFor("30").String() + "\tPayments API"}

	for _, verb := range []string{"view", "update", "delete"} {
		if got := completeCLI(t, "collection", verb, ""); !slices.Equal(got, want) {
			t.Errorf("collection %s = %q, want %q", verb, got, want)
		}
	}
	if got := completeCLI(t, "collections", "view", ""); !slices.Equal(got, want) {
		t.Errorf("collections view = %q", got)
	}
	if got := completeCLI(t, "collection", "view", idFor("30").String(), ""); len(got) != 0 {
		t.Errorf("a second argument completed %q", got)
	}
}

func TestFoldersCompleteAsPathsAndSomeAcceptAReservedDestination(t *testing.T) {
	completionStub(t)
	paths := []string{"Anchor", "Anchor/Identity"}

	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"flow", "list", "--folder", ""}, paths},
		{[]string{"flow", "folder", "rename", ""}, paths},
		{[]string{"flow", "folder", "delete", ""}, paths},
		{[]string{"flow", "folder", "move", ""}, paths},
		{[]string{"flow", "folder", "create", "--parent", ""}, paths},
		{[]string{"flow", "folder", "move", "Anchor", "--to", ""}, append([]string{"root"}, paths...)},
		{[]string{"flow", "move", "--to", ""}, append([]string{"uncategorized"}, paths...)},
		{[]string{"flow", "move", "--to", "Anc"}, []string{"Anchor", "Anchor/Identity"}},
		{[]string{"flow", "folder", "rename", "Anchor", ""}, nil},
	} {
		if got := completeCLI(t, tc.args...); !slices.Equal(got, tc.want) {
			t.Errorf("%v = %q, want %q", tc.args, got, tc.want)
		}
	}
}

func TestEnvironmentNamesCompleteForTheFlagAndTheDeleteArgument(t *testing.T) {
	completionStub(t)
	want := []string{"dev", "prd"}

	for _, args := range [][]string{
		{"flow", "run", "-e", ""},
		{"flow", "run", "--environment", ""},
		{"flow", "launch", "-e", ""},
		{"org", "env", "view", "-e", ""},
		{"org", "env", "get", "-e", ""},
		{"org", "env", "set", "--environment", ""},
		{"org", "env", "unset", "-e", ""},
		{"org", "env", "import", "-e", ""},
		{"org", "env", "environments", "delete", ""},
	} {
		if got := completeCLI(t, args...); !slices.Equal(got, want) {
			t.Errorf("%v = %q, want %q", args, got, want)
		}
	}
	if got := completeCLI(t, "org", "env", "environments", "delete", "dev", ""); len(got) != 0 {
		t.Errorf("a second argument completed %q", got)
	}
}

func TestProfileNamesCompleteForUseDeleteAndTheProfileFlag(t *testing.T) {
	completionStub(t)
	t.Setenv("ECHOPOINT_CONFIG", "")
	store := config.DefaultStore()
	store.Profiles["staging"] = config.Profile{APIBaseURL: "https://staging.example.com"}
	store.Profiles["local"] = config.Profile{APIBaseURL: "http://localhost:8080"}
	if _, err := config.SaveStore(store); err != nil {
		t.Fatal(err)
	}
	withDefault := []string{
		"default\thttps://api.echopoint.dev", "local\thttp://localhost:8080", "staging\thttps://staging.example.com",
	}

	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"profile", "use", ""}, withDefault},
		{[]string{"profile", "delete", ""}, withDefault[1:]},
		{[]string{"flow", "list", "--profile", ""}, withDefault},
		{[]string{"--profile", ""}, withDefault},
		{[]string{"profile", "use", "st"}, withDefault[2:]},
		{[]string{"profile", "use", "local", ""}, nil},
	} {
		if got := completeCLI(t, tc.args...); !slices.Equal(got, tc.want) {
			t.Errorf("%v = %q, want %q", tc.args, got, tc.want)
		}
	}
}

func TestTheProfileFlagCompletesFromTheConfigAFlagOrTheEnvironmentNames(t *testing.T) {
	completionStub(t)
	elsewhere := writeTemp(t, "config.yaml", "profiles:\n  qa:\n    api_base_url: https://qa.example.com\n")
	t.Setenv("ECHOPOINT_CONFIG", elsewhere)

	want := []string{"default\thttps://api.echopoint.dev", "qa\thttps://qa.example.com"}
	if got := completeCLI(t, "flow", "list", "--profile", ""); !slices.Equal(got, want) {
		t.Errorf("from ECHOPOINT_CONFIG = %q, want %q", got, want)
	}
	t.Setenv("ECHOPOINT_CONFIG", "")
	if got := completeCLI(t, "--config", elsewhere, "flow", "list", "--profile", ""); !slices.Equal(got, want) {
		t.Errorf("from --config = %q, want %q", got, want)
	}
}

func TestCompletionStaysSilentWhenTheAPIFails(t *testing.T) {
	stub := newAPIStub(t)
	stub.on(http.MethodGet, "/flows", http.StatusInternalServerError, map[string]any{
		"errors": []map[string]string{{"code": "INTERNAL", "message": "boom"}},
	})
	isolateCLIEnvironment(t, stub.server.URL)

	stdout, stderr, code := runCLI(t, cobra.ShellCompRequestCmd, "flow", "view", "")

	if code != 0 || strings.TrimSpace(stdout) != noFileCompletionDirective {
		t.Errorf("exit %d, stdout %q", code, stdout)
	}
	if strings.Contains(stdout+stderr, "boom") || strings.Contains(stdout+stderr, "api error") {
		t.Errorf("an error leaked into the prompt: %q %q", stdout, stderr)
	}
}

func TestCompletionStaysSilentWithoutCredentials(t *testing.T) {
	stub := completionStub(t)
	t.Setenv("ECHOPOINT_API_KEY", "")

	if got := completeCLI(t, "flow", "view", ""); len(got) != 0 {
		t.Errorf("completed %q without credentials", got)
	}
	if stub.requestCount() != 0 {
		t.Errorf("%d requests without credentials", stub.requestCount())
	}
}

func TestCompletionGivesUpAfterTheTimeout(t *testing.T) {
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
	names, directive := completeCollectionArgs(state)(&cobra.Command{}, nil, "")

	if len(names) != 0 || directive != cobra.ShellCompDirectiveNoFileComp || time.Since(started) > 2*time.Second {
		t.Errorf("names %q, directive %v after %v", names, directive, time.Since(started))
	}
}

func TestFormatCompletionsKeepsOnlyWhatStartsWithWhatIsTyped(t *testing.T) {
	candidates := []completionCandidate{{"alpha", "first"}, {"beta", ""}, {"alps", "third"}}

	got := formatCompletions(candidates, "al")

	if want := []string{"alpha\tfirst", "alps\tthird"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := formatCompletions(candidates, "be"); !slices.Equal(got, []string{"beta"}) {
		t.Errorf("a candidate without a description = %q", got)
	}
}

func folderCompletionStub(t *testing.T, folders ...api.FlowFolder) {
	t.Helper()
	stub := newAPIStub(t)
	stub.on(http.MethodGet, "/flows/folders", http.StatusOK, api.FlowFolderListResponse{
		Count: len(folders), Total: int64(len(folders)), Items: folders,
	})
	isolateCLIEnvironment(t, stub.server.URL)
}

func TestAFolderWhoseNameHoldsASlashCompletesAsItsIDNotAsAPathToAnotherFolder(t *testing.T) {
	payments, legacy, slashed := idFor("50"), idFor("51"), idFor("52")
	folderCompletionStub(t,
		sampleFolder(payments, "Payments", nil),
		sampleFolder(legacy, "Legacy", &payments),
		sampleFolder(slashed, "Payments/Legacy", nil),
	)
	want := []string{"Payments", "Payments/Legacy", slashed.String() + "\tPayments/Legacy"}

	got := completeCLI(t, "flow", "list", "--folder", "")

	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSiblingsOfOneNameCompleteAsIDsBecauseTheirPathIsAmbiguous(t *testing.T) {
	first, second, other := idFor("60"), idFor("61"), idFor("62")
	folderCompletionStub(t,
		sampleFolder(first, "Dup", nil),
		sampleFolder(second, "Dup", nil),
		sampleFolder(other, "Other", nil),
	)

	got := completeCLI(t, "flow", "folder", "rename", "")

	slices.Sort(got)
	want := []string{"Other", first.String() + "\tDup", second.String() + "\tDup"}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAFolderNamedLikeAReservedDestinationCompletesAsItsID(t *testing.T) {
	folder := idFor("70")
	folderCompletionStub(t, sampleFolder(folder, "Root", nil))

	withReserved := completeCLI(t, "flow", "folder", "move", "x", "--to", "")
	without := completeCLI(t, "flow", "list", "--folder", "")

	if want := []string{"root", folder.String() + "\tRoot"}; !slices.Equal(withReserved, want) {
		t.Errorf("--to completes %q, want %q", withReserved, want)
	}
	if want := []string{"Root"}; !slices.Equal(without, want) {
		t.Errorf("--folder completes %q, want %q", without, want)
	}
}
