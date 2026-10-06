package commands

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"echopoint-cli/internal/api"
)

func TestFlowCreateHasNoSlugAndSendsOnlyTheName(t *testing.T) {
	stub := newAPIStub(t)
	stub.on(http.MethodPost, "/flows", http.StatusCreated, sampleFlow(idFor("1"), "Checkout", nil))

	_, _, err := execute(t, newFlowCmd(stub.tableState(t)), "", "create", "--name", "Checkout")
	if err != nil {
		t.Fatal(err)
	}

	sent := bodyOf(t, stub.requestsTo(http.MethodPost, "/flows")[0])
	if _, has := sent["slug"]; has || sent["name"] != "Checkout" {
		t.Errorf("sent %v", sent)
	}
	create := find(t, NewRootCmd(), "flow", "create")
	update := find(t, NewRootCmd(), "flow", "update")
	if create.Flags().Lookup("slug") != nil || update.Flags().Lookup("slug") != nil {
		t.Error("--slug is still a flag of flow create or flow update")
	}
}

func TestFlowUpdateTakesAFileOrFlagsAndExactlyOneOfThem(t *testing.T) {
	flow := idFor("1")
	file := writeTemp(t, "flow.json", `{"description":"from the file"}`)
	stub := newAPIStub(t)
	stub.on(http.MethodPut, "/flows/"+flow.String(), http.StatusOK, sampleFlow(flow, "Checkout smoke", nil))

	stdout, _, err := execute(t, newFlowCmd(stub.tableState(t)), "", "update", flow.String(),
		"--name", "Checkout smoke", "--description", "Smoke test")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = execute(t, newFlowCmd(stub.state(t)), "", "update", flow.String(), "-f", file); err != nil {
		t.Fatal(err)
	}
	if _, _, err = execute(
		t,
		newFlowCmd(stub.state(t)),
		"",
		"update",
		flow.String(),
		"--name",
		"Only the name",
	); err != nil {
		t.Fatal(err)
	}

	sent := stub.requestsTo(http.MethodPut, "/flows/"+flow.String())
	if got := bodyOf(
		t,
		sent[0],
	); got["name"] != "Checkout smoke" || got["description"] != "Smoke test" ||
		len(got) != 2 {
		t.Errorf("flags sent %s", sent[0].body)
	}
	if got := bodyOf(t, sent[1]); got["description"] != "from the file" || len(got) != 1 {
		t.Errorf("the file sent %s", sent[1].body)
	}
	if got := bodyOf(t, sent[2]); got["name"] != "Only the name" || len(got) != 1 {
		t.Errorf("--name alone sent %s: the other fields must stay", sent[2].body)
	}
	if want := "ID: " + flow.String() + "\nName: Checkout smoke\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestFlowUpdateRefusesBothModesAndNeither(t *testing.T) {
	flow := idFor("1").String()
	file := writeTemp(t, "flow.json", `{}`)
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"both":          {[]string{"update", flow, "-f", file, "--name", "x"}, "pass either -f or --name and --description, not both"},
		"neither":       {[]string{"update", flow}, "pass -f, or at least one of --name and --description"},
		"empty name":    {[]string{"update", flow, "--name", " "}, "--name must not be empty"},
		"file as extra": {[]string{"update", flow, file}, "pass the file with -f"},
	} {
		t.Run(name, func(t *testing.T) {
			stub := newAPIStub(t)

			_, _, err := execute(t, newFlowCmd(stub.state(t)), "", tc.args...)

			if err == nil || !strings.Contains(err.Error(), tc.want) || stub.requestCount() != 0 {
				t.Errorf("err = %v, %d requests", err, stub.requestCount())
			}
		})
	}
}

var flowRowPattern = regexp.MustCompile(
	`^(Checkout smoke|Login)\s+550e8400-e29b-41d4-a716-0000000000\d\d\s+2026-10-05 12:00$`)

func TestFlowListShowsNameIDAndUpdatedOnEveryPath(t *testing.T) {
	folder := idFor("10")
	stub := newAPIStub(t).onFlows(
		sampleFlow(idFor("1"), "Checkout smoke", nil),
		sampleFlow(idFor("2"), "Login", &folder),
	)
	stub.on(http.MethodPost, "/flows/search", http.StatusOK, map[string]any{
		"count": 1, "total": 1, "items": []map[string]any{{
			"id": idFor("2"), "name": "Login", "updated_at": "2026-10-05T12:00:00Z",
		}},
	})
	stub.on(http.MethodGet, "/flows/folders", http.StatusOK, api.FlowFolderListResponse{
		Count: 1, Total: 1, Items: []api.FlowFolder{sampleFolder(folder, "Anchor", nil)},
	})
	isolateCLIEnvironment(t, stub.server.URL)

	for _, args := range [][]string{{"flow", "list"}, {"flow", "list", "--folder", "Anchor"}, {"flow", "list", "--uncategorized"}} {
		stdout, stderr, code := runCLI(t, args...)

		lines := strings.Split(strings.TrimSpace(stdout), "\n")
		if code != 0 || len(lines) < 3 {
			t.Fatalf("%v: exit %d, stdout %q, stderr %q", args, code, stdout, stderr)
		}
		if header := strings.Join(strings.Fields(lines[1]), " "); header != "NAME ID UPDATED" {
			t.Errorf("%v: header = %q", args, header)
		}
		if !flowRowPattern.MatchString(lines[2]) {
			t.Errorf("%v: the row is not name, id, updated: %q", args, lines[2])
		}
	}
}

func TestFlowViewShowsTheNameAndNoSlug(t *testing.T) {
	flow := idFor("1")
	stub := newAPIStub(t)
	stub.on(http.MethodGet, "/flows/"+flow.String(), http.StatusOK, sampleFlow(flow, "Checkout smoke", nil))

	stdout, _, err := execute(t, newFlowCmd(stub.tableState(t)), "", "view", flow.String())
	if err != nil {
		t.Fatal(err)
	}

	for _, line := range []string{"Name: Checkout smoke", "ID: " + flow.String()} {
		if !strings.Contains(stdout, line+"\n") {
			t.Errorf("the summary lacks %q:\n%s", line, stdout)
		}
	}
	if strings.Contains(strings.ToLower(stdout), "slug") {
		t.Errorf("the summary mentions a slug:\n%s", stdout)
	}
}

func TestTheFlowGroupSaysAFlowIDIsTheFlowsID(t *testing.T) {
	long := find(t, NewRootCmd(), "flow").Long

	if !strings.Contains(long, "A <flow-id> is the flow's id") || strings.Contains(strings.ToLower(long), "slug") {
		t.Errorf("the flow group Long:\n%s", long)
	}
}
