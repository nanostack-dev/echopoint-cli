package commands

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"echopoint-cli/internal/api"
)

func TestALeftoverPositionalFileFailsWithAPointerToF(t *testing.T) {
	flow := idFor("1").String()
	for _, args := range [][]string{
		{"flow", "create", "flow.json"},
		{"flow", "update", flow, "flow.json"},
		{"collection", "import", "openapi.json"},
		{"org", "env", "import", ".env"},
		{"flow", "env", "set", flow, "vars.json"},
		{"status-page", "save", "page.json"},
		{"status-page", "validate", "page.json"},
		{"status-page", "save", "-"},
		{"status-page", "validate", "-"},
		{"flows", "create", "flow.json"},
		{"collections", "import", "openapi.json"},
		{"status-pages", "save", "page.json"},
	} {
		_, _, err := runRoot(t, args...)

		if err == nil || err.Error() != "pass the file with -f" {
			t.Errorf("%v: err = %v", args, err)
		}
	}
}

func TestFileCommandsTakeTheFileThroughFAndOfferTheRightExtensions(t *testing.T) {
	root := NewRootCmd()
	for _, tc := range []struct {
		path       []string
		extensions []string
	}{
		{[]string{"flow", "create"}, []string{"json"}},
		{[]string{"flow", "update"}, []string{"json"}},
		{[]string{"collection", "import"}, []string{"json"}},
		{[]string{"org", "env", "import"}, []string{"json", "env"}},
		{[]string{"flow", "env", "set"}, []string{"json", "env"}},
		{[]string{"status-page", "save"}, []string{"json"}},
		{[]string{"status-page", "validate"}, []string{"json"}},
		{[]string{"spec", "create"}, []string{"yaml", "yml", "json"}},
		{[]string{"spec", "push"}, []string{"yaml", "yml", "json"}},
	} {
		cmd := find(t, root, tc.path...)
		file := cmd.Flags().Lookup("file")
		if file == nil || file.Shorthand != "f" {
			t.Errorf("%v: --file = %+v, want it with -f", tc.path, file)
			continue
		}
		if got := file.Annotations[cobra.BashCompFilenameExt]; !slices.Equal(got, tc.extensions) {
			t.Errorf("%v: file extensions %v, want %v", tc.path, got, tc.extensions)
		}
	}
}

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func bodyOf(t *testing.T, request stubRequest) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(request.body, &body); err != nil {
		t.Fatalf("body %q: %v", request.body, err)
	}
	return body
}

func TestFlowCreateAndUpdateReadTheirFileFromF(t *testing.T) {
	flowID := idFor("1")
	stub := newAPIStub(t)
	stub.on(http.MethodPost, "/flows", http.StatusCreated, sampleFlow(flowID, "From file", nil))
	stub.on(http.MethodPut, "/flows/"+flowID.String(), http.StatusOK, sampleFlow(flowID, "Renamed", nil))
	created := writeTemp(t, "create.json", `{"name":"From file","flow_definition":{"nodes":[],"edges":[]}}`)
	updated := writeTemp(t, "update.json", `{"name":"Renamed"}`)

	if _, _, err := execute(t, newFlowCmd(stub.state(t)), "", "create", "-f", created); err != nil {
		t.Fatal(err)
	}
	if _, _, err := execute(t, newFlowCmd(stub.state(t)), "", "update", flowID.String(), "-f", updated); err != nil {
		t.Fatal(err)
	}

	if got := bodyOf(t, stub.requestsTo(http.MethodPost, "/flows")[0])["name"]; got != "From file" {
		t.Errorf("create sent name %v", got)
	}
	if got := bodyOf(t, stub.requestsTo(http.MethodPut, "/flows/"+flowID.String())[0])["name"]; got != "Renamed" {
		t.Errorf("update sent name %v", got)
	}
}

func TestCollectionImportReadsItsFileFromF(t *testing.T) {
	stub := newAPIStub(t)
	stub.on(http.MethodPost, "/collections/import/openapi", http.StatusCreated, api.OpenAPIImportResult{
		Collection: api.Collection{Id: idFor("2"), Name: "Pets"}, RequestsCreated: 3,
	})
	spec := writeTemp(t, "openapi.json", `{"openapi":"3.0.0","info":{"title":"Pets","version":"1"},"paths":{}}`)

	_, _, err := execute(t, newCollectionCmd(stub.tableState(t)), "", "import", "-f", spec, "--name", "Pets")

	if err != nil {
		t.Fatal(err)
	}
	sent := bodyOf(t, stub.requestsTo(http.MethodPost, "/collections/import/openapi")[0])
	if sent["spec"].(map[string]any)["openapi"] != "3.0.0" ||
		sent["options"].(map[string]any)["collection_name"] != "Pets" {
		t.Errorf("sent %v", sent)
	}
}

func TestOrgEnvImportAndFlowEnvSetReadTheirFileFromF(t *testing.T) {
	flowID := idFor("1")
	stub := newAPIStub(t)
	for _, key := range []string{"A", "B"} {
		stub.on(http.MethodPut, "/organization/variables/"+key, http.StatusOK, map[string]any{})
		stub.on(http.MethodPut, "/flows/"+flowID.String()+"/variables/"+key, http.StatusOK, map[string]any{})
	}
	vars := writeTemp(t, "vars.env", "A=1\nB=2\n")

	if _, _, err := execute(t, newOrgCmd(stub.state(t)), "", "env", "import", "-f", vars); err != nil {
		t.Fatal(err)
	}
	if _, _, err := execute(t, newFlowCmd(stub.state(t)), "", "env", "set", flowID.String(), "-f", vars); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{
		"/organization/variables/A", "/organization/variables/B",
		"/flows/" + flowID.String() + "/variables/A", "/flows/" + flowID.String() + "/variables/B",
	} {
		if len(stub.requestsTo(http.MethodPut, path)) != 1 {
			t.Errorf("PUT %s sent %d times", path, len(stub.requestsTo(http.MethodPut, path)))
		}
	}
}

func TestStatusPageSaveReadsAFileFromFAndStdinFromDash(t *testing.T) {
	fixture, err := os.ReadFile("testdata/status-page.json")
	if err != nil {
		t.Fatal(err)
	}
	stub := newAPIStub(t)
	stub.on(http.MethodPut, "/status-pages/current", http.StatusOK, map[string]any{"slug": "acme"})

	if _, _, err := execute(
		t,
		newStatusPageCmd(stub.state(t)),
		"",
		"save",
		"-f",
		"testdata/status-page.json",
	); err != nil {
		t.Fatal(err)
	}
	if _, _, err := execute(t, newStatusPageCmd(stub.state(t)), string(fixture), "save", "-f", "-"); err != nil {
		t.Fatal(err)
	}

	sent := stub.requestsTo(http.MethodPut, "/status-pages/current")
	if len(sent) != 2 || string(sent[0].body) == "" || bodyOf(t, sent[0])["slug"] != bodyOf(t, sent[1])["slug"] {
		t.Errorf("saved %d times, bodies differ", len(sent))
	}
}

func TestStatusPageValidateStaysOfflineWithF(t *testing.T) {
	stub := newAPIStub(t)

	if _, _, err := execute(
		t,
		newStatusPageCmd(stub.state(t)),
		"",
		"validate",
		"-f",
		"testdata/status-page.json",
	); err != nil {
		t.Fatal(err)
	}
	if stub.requestCount() != 0 {
		t.Errorf("validate made %d requests", stub.requestCount())
	}
}

func TestACommandThatNeedsItsFileFailsWithoutF(t *testing.T) {
	for _, args := range [][]string{
		{"collection", "import"},
		{"org", "env", "import"},
		{"status-page", "save"},
		{"status-page", "validate"},
	} {
		_, _, err := runRoot(t, args...)

		if err == nil || err.Error() != `required flag(s) "file" not set` {
			t.Errorf("%v: err = %v", args, err)
		}
	}
}

func TestAnEmptyFAsAnUnsetVariableMakesItIsRefusedEverywhere(t *testing.T) {
	flow := idFor("1").String()
	for _, args := range [][]string{
		{"spec", "lint", "pets-api", "-f", ""},
		{"spec", "diff", "pets-api", "-f", ""},
		{"spec", "pull", "pets-api", "-f", ""},
		{"spec", "push", "pets-api", "-f", ""},
		{"spec", "check", "pets-api", "-f", ""},
		{"spec", "create", "pets-api", "-f", ""},
		{"flow", "create", "-f", ""},
		{"flow", "update", flow, "-f", ""},
		{"flow", "env", "set", flow, "-f", ""},
		{"collection", "import", "-f", ""},
		{"org", "env", "import", "-f", ""},
		{"status-page", "save", "-f", ""},
		{"status-page", "validate", "--file", ""},
		{"spec", "lint", "pets-api", "-f", "  "},
		{"spec", "lint", "pets-api", "--file="},
	} {
		_, _, err := runRoot(t, args...)

		if err == nil || err.Error() != "-f needs a file path" {
			t.Errorf("%v: err = %v", args, err)
		}
	}
}

func TestAnUnsetOpenAPIFileCannotFallBackToLiveFindingsOrVersionDiffs(t *testing.T) {
	stub := newAPIStub(t)
	isolateCLIEnvironment(t, stub.server.URL)
	t.Setenv("OPENAPI_FILE", "")
	// a shell turns -f "$OPENAPI_FILE" into -f ""
	unset := os.Getenv("OPENAPI_FILE")

	for _, args := range [][]string{
		{"spec", "lint", "pets-api", "-f", unset},
		{"spec", "lint", "pets-api", "-f", unset, "--fail-on-findings"},
		{"spec", "diff", "pets-api", "-f", unset},
		{"spec", "pull", "pets-api", "-f", unset},
	} {
		_, stderr, code := runCLI(t, args...)

		if code == 0 || !strings.Contains(stderr, "-f needs a file path") {
			t.Errorf("%v: exit %d, stderr %q", args, code, stderr)
		}
	}
	if stub.requestCount() != 0 {
		t.Errorf("%d requests were made: the command fell back to another mode", stub.requestCount())
	}
}
