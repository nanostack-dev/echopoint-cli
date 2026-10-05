package commands

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/nanostack-dev/echopoint-kit/apispec"
	"github.com/spf13/cobra"

	"echopoint-cli/internal/output"
)

const editFixture = `# The pets API, kept by hand.
openapi: 3.0.3
info:
  title: Pets
  version: 1.0.0
paths:
  # Everything about pets.
  /pets:
    get:
      operationId: listPets
      parameters:
        - name: limit # page size
          in: query
          schema:
            type: integer
      responses:
        "200":
          description: The pets.
components:
  schemas:
    # The error every failure returns.
    Error:
      type: object
      properties:
        message:
          type: string
`

func editSpec(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	return runSpec(t, output.FormatTable, args...)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSpecSchemaAddEditsTheFileAndKeepsItsComments(t *testing.T) {
	path := writeSpec(t, editFixture)
	stdout, _, err := editSpec(t, "schema", "add", "Pet", "--file", path)
	if err != nil {
		t.Fatalf("schema add failed: %v", err)
	}
	if !strings.Contains(stdout, "✓ Added schema Pet to "+path) {
		t.Errorf("stdout = %q", stdout)
	}
	edited := readFile(t, path)
	if !strings.Contains(edited, "    Pet:\n      type: object\n") {
		t.Errorf("Pet schema missing:\n%s", edited)
	}
	for line := range strings.SplitSeq(strings.TrimSuffix(editFixture, "\n"), "\n") {
		if !strings.Contains(edited, line+"\n") {
			t.Errorf("line %q did not survive:\n%s", line, edited)
		}
	}
	if !strings.HasPrefix(edited, "# The pets API, kept by hand.\n") {
		t.Errorf("leading comment lost:\n%s", edited)
	}
}

func TestSpecEditKeepsTheFileMode(t *testing.T) {
	path := writeSpec(t, editFixture)
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, _, err := editSpec(t, "schema", "add", "Pet", "--file", path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v, want 0640", info.Mode().Perm())
	}
	entries, err := os.ReadDir(strings.TrimSuffix(path, "/openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("directory holds %d entries, want only the document", len(entries))
	}
}

func TestSpecRouteAddCreatesTheOperationWithItsStatus(t *testing.T) {
	path := writeSpec(t, editFixture)
	stdout, _, err := editSpec(t, "route", "add", "post", "/pets", "--file", path, "--status", "201",
		"--operation-id", "createPet", "--summary", "Create a pet", "--tag", "pets")
	if err != nil {
		t.Fatalf("route add failed: %v", err)
	}
	if !strings.Contains(stdout, "✓ Added POST /pets to "+path) {
		t.Errorf("stdout = %q", stdout)
	}
	edited := readFile(t, path)
	for _, want := range []string{"    post:\n", "operationId: createPet", "summary: Create a pet", `"201":`, "# Everything about pets."} {
		if !strings.Contains(edited, want) {
			t.Errorf("edited document lacks %q:\n%s", want, edited)
		}
	}
}

func TestSpecRouteUpdateRenamesThePathAndTheMethod(t *testing.T) {
	path := writeSpec(t, editFixture)
	if _, _, err := editSpec(t, "route", "update", "GET", "/pets", "--file", path,
		"--summary", "List", "--path", "/animals", "--method", "put"); err != nil {
		t.Fatalf("route update failed: %v", err)
	}
	edited := readFile(t, path)
	if !strings.Contains(edited, "  /animals:\n    put:\n") || !strings.Contains(edited, "summary: List") {
		t.Errorf("edited document:\n%s", edited)
	}
}

func TestSpecRouteRemoveDropsTheOperation(t *testing.T) {
	path := writeSpec(t, editFixture)
	stdout, _, err := editSpec(t, "route", "remove", "GET", "/pets", "--file", path)
	if err != nil {
		t.Fatalf("route remove failed: %v", err)
	}
	if !strings.Contains(stdout, "✓ Removed GET /pets from "+path) || strings.Contains(readFile(t, path), "listPets") {
		t.Errorf("stdout = %q, file:\n%s", stdout, readFile(t, path))
	}
}

func TestSpecMethodUpdateChangesTheMethod(t *testing.T) {
	path := writeSpec(t, editFixture)
	if _, _, err := editSpec(t, "method", "update", "GET", "/pets", "--file", path, "--to", "PATCH"); err != nil {
		t.Fatalf("method update failed: %v", err)
	}
	if edited := readFile(
		t,
		path,
	); !strings.Contains(edited, "    patch:\n") ||
		strings.Contains(edited, "    get:\n") {
		t.Errorf("edited document:\n%s", edited)
	}
}

func TestSpecParamAddUpdateRemoveFindParametersByNameAndLocation(t *testing.T) {
	path := writeSpec(t, editFixture)
	steps := [][]string{
		{"param", "add", "GET", "/pets", "limit", "--in", "header", "--type", "string"},
		{"param", "update", "GET", "/pets", "limit", "--in", "query", "--required=true", "--description", "Page size"},
		{"param", "update", "GET", "/pets", "limit", "--in", "header", "--name", "x-limit"},
	}
	for _, step := range steps {
		if _, _, err := editSpec(t, append(step, "--file", path)...); err != nil {
			t.Fatalf("%v failed: %v", step, err)
		}
	}
	edited := readFile(t, path)
	for _, want := range []string{"name: limit # page size", "description: Page size", "name: x-limit", "in: header"} {
		if !strings.Contains(edited, want) {
			t.Errorf("edited document lacks %q:\n%s", want, edited)
		}
	}
	stdout, _, err := editSpec(t, "param", "remove", "GET", "/pets", "limit", "--in", "query", "--file", path)
	if err != nil {
		t.Fatalf("param remove failed: %v", err)
	}
	if !strings.Contains(stdout, "✓ Removed parameter limit (query) of GET /pets from "+path) {
		t.Errorf("stdout = %q", stdout)
	}
	edited = readFile(t, path)
	if strings.Contains(edited, "name: limit") || !strings.Contains(edited, "name: x-limit") {
		t.Errorf("edited document:\n%s", edited)
	}
}

func TestSpecParamUpdateRefusesAParameterThatIsNotThere(t *testing.T) {
	path := writeSpec(t, editFixture)
	_, stderr, err := editSpec(t, "param", "update", "GET", "/pets", "limit", "--in", "cookie",
		"--description", "x", "--file", path)
	if exitCode(err) != 1 || !strings.Contains(stderr, "✗ ") || !strings.Contains(stderr, "limit") {
		t.Errorf("exit %d, stderr %q", exitCode(err), stderr)
	}
	if readFile(t, path) != editFixture {
		t.Error("the file changed")
	}
}

func TestSpecResponseAddWithASchemaReference(t *testing.T) {
	path := writeSpec(t, editFixture)
	if _, _, err := editSpec(t, "response", "add", "GET", "/pets", "default", "--schema", "Error",
		"--description", "Failure", "--file", path); err != nil {
		t.Fatalf("response add failed: %v", err)
	}
	edited := readFile(t, path)
	for _, want := range []string{"default:\n", "description: Failure", "application/json:", "$ref: '#/components/schemas/Error'"} {
		if !strings.Contains(edited, want) {
			t.Errorf("edited document lacks %q:\n%s", want, edited)
		}
	}
	if _, _, err := editSpec(t, "response", "update", "GET", "/pets", "default", "--description", "Oops",
		"--file", path); err != nil {
		t.Fatalf("response update failed: %v", err)
	}
	if _, _, err := editSpec(t, "response", "remove", "GET", "/pets", "default", "--file", path); err != nil {
		t.Fatalf("response remove failed: %v", err)
	}
	if strings.Contains(readFile(t, path), "Oops") {
		t.Error("the response was not removed")
	}
}

func TestSpecPropertyAddUpdateRemoveWithNestedNames(t *testing.T) {
	path := writeSpec(t, editFixture)
	steps := [][]string{
		{"property", "add", "Error", "details", "--type", "object"},
		{"property", "add", "Error", "details.code", "--type", "integer", "--required"},
		{"property", "update", "Error", "details.code", "--name", "number", "--description", "The code"},
		{"property", "add", "Error", "kind", "--enum", "a,b"},
	}
	for _, step := range steps {
		if _, _, err := editSpec(t, append(step, "--file", path)...); err != nil {
			t.Fatalf("%v failed: %v", step, err)
		}
	}
	edited := readFile(t, path)
	for _, want := range []string{"details:", "number:", "description: The code", "- a", "- b"} {
		if !strings.Contains(edited, want) {
			t.Errorf("edited document lacks %q:\n%s", want, edited)
		}
	}
	if _, _, err := editSpec(t, "property", "remove", "Error", "details.number", "--file", path); err != nil {
		t.Fatalf("property remove failed: %v", err)
	}
	if strings.Contains(readFile(t, path), "number:") {
		t.Error("the nested property was not removed")
	}
}

func TestSpecSchemaUpdateAndRemove(t *testing.T) {
	path := writeSpec(t, editFixture)
	if _, _, err := editSpec(t, "schema", "update", "Error", "--description", "Failure", "--file", path); err != nil {
		t.Fatalf("schema update failed: %v", err)
	}
	if !strings.Contains(readFile(t, path), "description: Failure") {
		t.Error("description not set")
	}
	if _, _, err := editSpec(t, "schema", "remove", "Error", "--file", path); err != nil {
		t.Fatalf("schema remove failed: %v", err)
	}
	if strings.Contains(readFile(t, path), "Error:") {
		t.Error("schema not removed")
	}
}

func TestSpecEditRefusalExitsOneAndLeavesTheFileAlone(t *testing.T) {
	path := writeSpec(t, editFixture)
	stdout, stderr, err := editSpec(t, "route", "add", "GET", "/pets", "--file", path)
	if exitCode(err) != 1 {
		t.Fatalf("exit %d, want 1 (err %v)", exitCode(err), err)
	}
	if stdout != "" || !strings.Contains(stderr, "✗ operation get /pets already exists") {
		t.Errorf("stdout %q, stderr %q", stdout, stderr)
	}
	if strings.Contains(stderr, "Usage:") {
		t.Errorf("stderr holds a usage block: %q", stderr)
	}
	if readFile(t, path) != editFixture {
		t.Error("the file changed")
	}
}

func TestSpecEditOfAnUnreadableFileNamesIt(t *testing.T) {
	path := writeSpec(t, "swagger: \"2.0\"\n")
	_, _, err := editSpec(t, "schema", "add", "Pet", "--file", path)
	if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "swagger 2.0") {
		t.Errorf("err = %v", err)
	}
}

func TestSpecEditDryRunPrintsTheDocumentAndWritesNothing(t *testing.T) {
	path := writeSpec(t, editFixture)
	stdout, _, err := editSpec(t, "schema", "add", "Pet", "--file", path, "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "    Pet:\n      type: object\n") ||
		!strings.Contains(stdout, "# The pets API, kept by hand.") {
		t.Errorf("stdout:\n%s", stdout)
	}
	if readFile(t, path) != editFixture {
		t.Error("--dry-run wrote the file")
	}
	_, _, err = runSpec(t, output.FormatJSON, "schema", "add", "Pet", "--file", path, "--dry-run")
	if err == nil || !strings.Contains(err.Error(), "--dry-run") {
		t.Errorf("--dry-run with -o json: err = %v", err)
	}
}

func TestSpecEditJSONOutputShape(t *testing.T) {
	path := writeSpec(t, editFixture)
	stdout, _, err := runSpec(t, output.FormatJSON, "route", "add", "POST", "/pets", "--file", path, "--status", "201")
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		File     string            `json:"file"`
		Commands []apispec.Command `json:"commands"`
		Changed  bool              `json:"changed"`
	}
	if err = json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("decode %q: %v", stdout, err)
	}
	if result.File != path || !result.Changed || len(result.Commands) != 1 ||
		result.Commands[0].Kind != apispec.CommandAddOperation || result.Commands[0].Method != "post" ||
		result.Commands[0].Path != "/pets" || result.Commands[0].Status != "201" {
		t.Errorf("result = %+v", result)
	}
	var raw map[string]any
	if err = json.Unmarshal([]byte(stdout), &raw); err != nil || len(raw) != 3 {
		t.Errorf("want exactly file, commands, changed: %v %v", raw, err)
	}
}

func TestSpecEditYAMLOutputUsesTheJSONKeys(t *testing.T) {
	path := writeSpec(t, editFixture)
	stdout, _, err := runSpec(t, output.FormatYAML, "route", "add", "POST", "/pets", "--file", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"file: " + path, "changed: true", "kind: add_operation", "method: post"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
}

func TestSpecEditThatChangesNothingReportsIt(t *testing.T) {
	path := writeSpec(t, editFixture)
	stdout, _, err := runSpec(t, output.FormatJSON, "route", "update", "GET", "/pets", "--file", path,
		"--operation-id", "listPets")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, `"changed": false`) || readFile(t, path) != editFixture {
		t.Errorf("stdout %q", stdout)
	}
}

func TestSpecUpdateWithNoFlagNamesTheFlags(t *testing.T) {
	path := writeSpec(t, editFixture)
	for _, args := range [][]string{
		{"route", "update", "GET", "/pets"},
		{"schema", "update", "Error"},
		{"property", "update", "Error", "message"},
		{"param", "update", "GET", "/pets", "limit", "--in", "query"},
		{"response", "update", "GET", "/pets", "200"},
	} {
		_, _, err := editSpec(t, append(args, "--file", path)...)
		if err == nil || !strings.Contains(err.Error(), "nothing to change") || !strings.Contains(err.Error(), "--") {
			t.Errorf("%v: err = %v", args, err)
		}
	}
}

func TestSpecEditNeedsAFile(t *testing.T) {
	_, _, err := editSpec(t, "schema", "add", "Pet")
	if err == nil || !strings.Contains(err.Error(), "--file") {
		t.Errorf("err = %v", err)
	}
}

func TestSpecEditCommandsNeedCredentialsOnlyWithSpec(t *testing.T) {
	root := &cobra.Command{Use: "echopoint"}
	root.AddCommand(newSpecCmd(&AppState{}))
	specCmd, _, err := root.Find([]string{"spec"})
	if err != nil {
		t.Fatal(err)
	}
	var leaves []*cobra.Command
	for _, group := range specCmd.Commands() {
		if slices.Contains([]string{"route", "method", "schema", "property", "param", "response"}, group.Name()) {
			leaves = append(leaves, group.Commands()...)
		}
	}
	if len(leaves) != 16 {
		t.Fatalf("%d edit commands, want 16", len(leaves))
	}
	for _, cmd := range leaves {
		name := cmd.Parent().Name() + " " + cmd.Name()
		if requiresToken(cmd) {
			t.Errorf("%s requires a token without --spec", name)
		}
		if err := cmd.Flags().Set("spec", "pets-api"); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !requiresToken(cmd) {
			t.Errorf("%s with --spec does not require a token", name)
		}
	}
}
