package commands

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nanostack-dev/echopoint-kit/apispec"

	"echopoint-cli/internal/output"
)

const conventionalSpec = `openapi: 3.0.3
info:
  title: Pets
  version: 1.0.0
paths:
  /pets:
    get:
      operationId: listPets
      responses:
        "200":
          description: The pets.
    post:
      operationId: createPet
      responses:
        "201":
          description: Created.
  /pets/{petId}:
    parameters:
      - name: petId
        in: path
        required: true
        schema:
          type: string
    get:
      operationId: getPet
      responses:
        "200":
          description: The pet.
    put:
      operationId: updatePet
      responses:
        "200":
          description: The pet.
    delete:
      operationId: deletePet
      responses:
        "204":
          description: Deleted.
`

const snakeCaseOwners = `  /owners:
    get:
      operationId: list_owners
      responses:
        "200":
          description: The owners.
`

const camelCaseToys = `  /toys:
    get:
      operationId: listToys
      responses:
        "200":
          description: The toys.
`

func lintResult(t *testing.T, stdout string) specLint {
	t.Helper()
	var result specLint
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("decode %q: %v", stdout, err)
	}
	return result
}

func findingPointers(findings []apispec.Finding) []string {
	pointers := make([]string, 0, len(findings))
	for _, finding := range findings {
		pointers = append(pointers, string(finding.Rule)+" "+finding.Pointer)
	}
	return pointers
}

func TestSpecLintReportsWhereTheDocumentDepartsFromItsConventions(t *testing.T) {
	stdout, _, err := runSpec(t, output.FormatJSON, "lint", writeSpec(t, conventionalSpec+snakeCaseOwners))

	if err != nil {
		t.Fatalf("lint failed: %v", err)
	}
	got := findingPointers(lintResult(t, stdout).Findings)
	if len(got) != 1 || got[0] != "operation-id-casing /paths/~1owners/get" {
		t.Errorf("findings = %v", got)
	}
}

func TestSpecLintPrintsFindingsForReading(t *testing.T) {
	stdout, _, err := runSpec(t, output.FormatTable, "lint", writeSpec(t, conventionalSpec+snakeCaseOwners))

	if err != nil {
		t.Fatalf("lint failed: %v", err)
	}
	for _, want := range []string{"1 finding(s)", "operation-id-casing  /paths/~1owners/get", "Convention: "} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
}

func TestSpecLintOfAConventionalDocumentReportsNothing(t *testing.T) {
	stdout, _, err := runSpec(t, output.FormatTable, "lint", "--fail-on-findings", writeSpec(t, conventionalSpec))

	if err != nil {
		t.Fatalf("lint failed: %v", err)
	}
	if !strings.Contains(stdout, "follows its conventions") {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestSpecLintExitsZeroWithFindingsByDefault(t *testing.T) {
	_, _, err := runSpec(t, output.FormatTable, "lint", writeSpec(t, conventionalSpec+snakeCaseOwners))

	if exitCode(err) != 0 {
		t.Errorf("exit %d", exitCode(err))
	}
}

func TestSpecLintFailOnFindingsExitsOne(t *testing.T) {
	_, _, err := runSpec(t, output.FormatTable, "lint", "--fail-on-findings",
		writeSpec(t, conventionalSpec+snakeCaseOwners))

	if exitCode(err) != 1 {
		t.Errorf("exit %d", exitCode(err))
	}
}

func TestSpecLintFileFlagReadsTheFile(t *testing.T) {
	stdout, _, err := runSpec(t, output.FormatJSON, "lint", "--file", writeSpec(t, conventionalSpec+snakeCaseOwners))

	if err != nil {
		t.Fatalf("lint failed: %v", err)
	}
	if len(lintResult(t, stdout).Findings) != 1 {
		t.Errorf("stdout = %s", stdout)
	}
}

func TestSpecLintRefusesTheFileTwice(t *testing.T) {
	path := writeSpec(t, conventionalSpec)

	_, _, err := runSpec(t, output.FormatTable, "lint", "--file", path, path)

	if err == nil || !strings.Contains(err.Error(), "not both") {
		t.Errorf("err = %v", err)
	}
}

func TestSpecLintBaseReportsOnlyWhatTheDocumentAdds(t *testing.T) {
	base := writeSpec(t, conventionalSpec+snakeCaseOwners)

	stdout, _, err := runSpec(t, output.FormatJSON, "lint", "--base", base,
		writeSpec(t, conventionalSpec+snakeCaseOwners+camelCaseToys))

	if err != nil {
		t.Fatalf("lint failed: %v", err)
	}
	if got := lintResult(t, stdout).Findings; len(got) != 0 {
		t.Errorf("findings = %v", findingPointers(got))
	}
}

func TestSpecLintBaseReportsANewDeparture(t *testing.T) {
	base := writeSpec(t, conventionalSpec)

	stdout, _, err := runSpec(
		t,
		output.FormatJSON,
		"lint",
		"--base",
		base,
		writeSpec(t, conventionalSpec+snakeCaseOwners),
	)

	if err != nil {
		t.Fatalf("lint failed: %v", err)
	}
	if got := findingPointers(lintResult(t, stdout).Findings); len(got) != 1 {
		t.Errorf("findings = %v", got)
	}
}

func TestSpecLintSpecComparesWithTheLiveVersion(t *testing.T) {
	fake, server := newSpecServer(t)
	fake.responses["GET /specs/pets-api/document"] = pulledDocument(conventionalSpec + snakeCaseOwners)

	stdout, _, err := runRemoteSpecAs(t, server.URL, output.FormatJSON, "lint", "--spec", "pets-api",
		writeSpec(t, conventionalSpec+snakeCaseOwners+camelCaseToys))

	if err != nil {
		t.Fatalf("lint failed: %v", err)
	}
	result := lintResult(t, stdout)
	if len(result.Findings) != 0 || result.Base != "pets-api (Live 1.4.0)" {
		t.Errorf("result = %+v", result)
	}
}

func TestSpecLintNeedsCredentialsOnlyWithSpec(t *testing.T) {
	cmd := newSpecLintCmd(&AppState{})
	if requiresToken(cmd) {
		t.Error("lint of a local file requires a token")
	}
	if err := cmd.Flags().Set("spec", "pets-api"); err != nil {
		t.Fatal(err)
	}
	if !requiresToken(cmd) {
		t.Error("lint --spec does not require a token")
	}
}
