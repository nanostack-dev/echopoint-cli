package commands

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
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
	`^(Checkout smoke|Login)\s+550e8400-e29b-41d4-a716-0000000000\d\d\s+standalone\s+2026-10-05 12:00$`)

func TestFlowListShowsNameIDRoleAndUpdatedOnEveryPath(t *testing.T) {
	folder := idFor("10")
	stub := newAPIStub(t).onFlows(
		sampleFlow(idFor("1"), "Checkout smoke", nil),
		sampleFlow(idFor("2"), "Login", &folder),
	)
	stub.onFolders(sampleFolder(folder, "Anchor", nil))
	isolateCLIEnvironment(t, stub.server.URL)

	for _, tc := range []struct {
		args []string
		want map[string]any
	}{
		{[]string{"flow", "list"}, map[string]any{}},
		{[]string{"flow", "list", "--folder", "Anchor"}, map[string]any{"folder_id": folder.String()}},
		{[]string{"flow", "list", "--uncategorized"}, map[string]any{"uncategorized": true}},
	} {
		before := len(stub.requestsTo(http.MethodPost, flowSearchPath))
		stdout, stderr, code := runCLI(t, tc.args...)

		lines := strings.Split(strings.TrimSpace(stdout), "\n")
		if code != 0 || len(lines) < 3 {
			t.Fatalf("%v: exit %d, stdout %q, stderr %q", tc.args, code, stdout, stderr)
		}
		if header := strings.Join(strings.Fields(lines[1]), " "); header != "NAME ID ROLE UPDATED" {
			t.Errorf("%v: header = %q", tc.args, header)
		}
		if !flowRowPattern.MatchString(lines[2]) {
			t.Errorf("%v: the row is not name, id, role, updated: %q", tc.args, lines[2])
		}
		sent := stub.requestsTo(http.MethodPost, flowSearchPath)
		if len(sent) != before+1 {
			t.Fatalf("%v: %d searches, want one more than %d", tc.args, len(sent), before)
		}
		body := bodyOf(t, sent[before])
		for key, want := range tc.want {
			if body[key] != want {
				t.Errorf("%v: the search sent %s = %v, want %v (body %v)", tc.args, key, body[key], want, body)
			}
		}
		if len(tc.want) == 0 && (body["folder_id"] != nil || body["uncategorized"] != nil) {
			t.Errorf("%v: the plain listing scoped the search: %v", tc.args, body)
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
