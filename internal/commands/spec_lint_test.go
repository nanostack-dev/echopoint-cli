package commands

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"echopoint-cli/internal/api"
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

func findingPointers(findings []api.SpecFinding) []string {
	pointers := make([]string, 0, len(findings))
	for _, finding := range findings {
		pointers = append(pointers, string(finding.Rule)+" "+finding.Pointer)
	}
	return pointers
}

func storedFindings() []api.SpecFinding {
	return []api.SpecFinding{
		{
			Rule: api.SpecLintRule("operation-id-casing"), Pointer: "/paths/~1owners/get",
			Message: "list_owners is not camelCase", Convention: "camelCase (90% of 10 operation IDs)",
			Evidence: "listPets, getPet", Introduced: true,
		},
		{
			Rule: api.SpecLintRule("missing-security"), Pointer: "/paths/~1old/get",
			Message: "no security", Convention: "security (80% of 5 operations)",
		},
	}
}

// storedLintServer is a fake EchoPoint whose Live version is 1.4.0 and whose
// versions 1.4.0 and 1.3.0 carry findings.
func storedLintServer(t *testing.T, findings []api.SpecFinding) (*specServer, string) {
	t.Helper()
	fake, server := newSpecServer(t)
	fake.responses["GET /specs/pets-api"] = specOf("pets-api", "Pets", "1.4.0")
	fake.responses["GET /specs/pets-api/versions/1.4.0"] = publishedVersion("1.4.0", findings...)
	fake.responses["GET /specs/pets-api/versions/1.3.0"] = publishedVersion("1.3.0", storedFindings()[1])
	return fake, server.URL
}

func TestSpecLintListsTheFindingsStoredForLive(t *testing.T) {
	fake, url := storedLintServer(t, storedFindings())

	stdout, _, err := runRemoteSpec(t, url, "lint", "pets-api")

	if err != nil {
		t.Fatal(err)
	}
	for want, got := range map[string]string{
		"pets-api 1.4.0: 2 finding(s), 1 new":             line(stdout, "pets-api"),
		"operation-id-casing /paths/~1owners/get (new)":   line(stdout, "operation-id-casing"),
		"missing-security /paths/~1old/get":               line(stdout, "missing-security"),
		"Convention: camelCase (90% of 10 operation IDs)": line(stdout, "  Convention: camelCase"),
		"Evidence: listPets, getPet":                      line(stdout, "  Evidence:"),
	} {
		if got != want {
			t.Errorf("line = %q, want %q\n%s", got, want, stdout)
		}
	}
	if want := []string{
		"GET /specs/pets-api",
		"GET /specs/pets-api/versions/1.4.0",
	}; strings.Join(
		fake.requests,
		",",
	) != strings.Join(
		want,
		",",
	) {
		t.Errorf("requests = %v, want %v", fake.requests, want)
	}
}

func TestSpecLintVersionListsThatVersionsFindings(t *testing.T) {
	fake, url := storedLintServer(t, storedFindings())

	stdout, _, err := runRemoteSpecAs(t, url, output.FormatJSON, "lint", "pets-api", "--version", "1.3.0")

	if err != nil {
		t.Fatal(err)
	}
	result := lintResult(t, stdout)
	if result.Spec != "pets-api" || result.Version != "1.3.0" || result.File != "" ||
		strings.Join(findingPointers(result.Findings), ",") != "missing-security /paths/~1old/get" {
		t.Errorf("result = %+v", result)
	}
	if strings.Join(fake.requests, ",") != "GET /specs/pets-api/versions/1.3.0" {
		t.Errorf("requests = %v", fake.requests)
	}
	if result.Findings[0].Introduced {
		t.Error("a finding that was not introduced is marked introduced")
	}
}

func TestSpecLintOfAVersionWithoutFindingsSaysItFollowsItsConventions(t *testing.T) {
	_, url := storedLintServer(t, nil)

	stdout, _, err := runRemoteSpec(t, url, "lint", "pets-api", "--fail-on-findings")

	if err != nil || !strings.Contains(stdout, "✓ pets-api 1.4.0 follows its conventions") {
		t.Errorf("stdout %q, err %v", stdout, err)
	}
	jsonOut, _, err := runRemoteSpecAs(t, url, output.FormatJSON, "lint", "pets-api")
	if err != nil || !strings.Contains(jsonOut, `"findings": []`) {
		t.Errorf("stdout %q, err %v", jsonOut, err)
	}
}

func TestSpecLintExitsZeroWithFindingsByDefaultAndOneWithFailOnFindings(t *testing.T) {
	_, url := storedLintServer(t, storedFindings())

	_, _, err := runRemoteSpec(t, url, "lint", "pets-api")
	if exitCode(err) != 0 {
		t.Errorf("exit %d", exitCode(err))
	}
	_, _, err = runRemoteSpec(t, url, "lint", "pets-api", "--fail-on-findings")
	if exitCode(err) != 1 {
		t.Errorf("exit %d", exitCode(err))
	}
}

func TestSpecLintOfAnUnknownVersionNamesTheVersion(t *testing.T) {
	_, url := storedLintServer(t, nil)

	_, _, err := runRemoteSpec(t, url, "lint", "pets-api", "--version", "0.0.1")

	if err == nil || !strings.Contains(err.Error(), `pets-api has no version "0.0.1"`) {
		t.Errorf("err = %v", err)
	}
}

func TestSpecLintOfAnUnknownSpecSuggestsList(t *testing.T) {
	_, server := newSpecServer(t)

	_, _, err := runRemoteSpec(t, server.URL, "lint", "nope")

	if err == nil || !strings.Contains(err.Error(), `no spec with slug "nope"`) ||
		!strings.Contains(err.Error(), "echopoint spec list") {
		t.Errorf("err = %v", err)
	}
}

func liveWith(t *testing.T, document string) (*specServer, string) {
	t.Helper()
	fake, server := newSpecServer(t)
	fake.responses["GET /specs/pets-api/document"] = pulledDocument(document)
	return fake, server.URL
}

func TestSpecLintFileReportsOnlyWhatItAddsToLive(t *testing.T) {
	_, url := liveWith(t, conventionalSpec+snakeCaseOwners)

	stdout, _, err := runRemoteSpecAs(t, url, output.FormatJSON, "lint", "pets-api", "-f",
		writeSpec(t, conventionalSpec+snakeCaseOwners+camelCaseToys))

	if err != nil {
		t.Fatalf("lint failed: %v", err)
	}
	result := lintResult(t, stdout)
	if len(result.Findings) != 0 || result.Spec != "pets-api" || result.Version != "1.4.0" || result.File == "" {
		t.Errorf("result = %+v", result)
	}
}

func TestSpecLintFileReportsANewDeparture(t *testing.T) {
	_, url := liveWith(t, conventionalSpec)
	path := writeSpec(t, conventionalSpec+snakeCaseOwners)

	stdout, _, err := runRemoteSpec(t, url, "lint", "pets-api", "-f", path)

	if err != nil {
		t.Fatalf("lint failed: %v", err)
	}
	for _, want := range []string{
		path + " adds 1 finding(s) to pets-api 1.4.0 (Live)", "operation-id-casing  /paths/~1owners/get", "Convention: ",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "(new)") {
		t.Errorf("every finding of a file is new; stdout marks them:\n%s", stdout)
	}
	jsonOut, _, err := runRemoteSpecAs(t, url, output.FormatJSON, "lint", "pets-api", "-f", path)
	if err != nil {
		t.Fatal(err)
	}
	if got := findingPointers(
		lintResult(t, jsonOut).Findings,
	); len(got) != 1 ||
		got[0] != "operation-id-casing /paths/~1owners/get" {
		t.Errorf("findings = %v", got)
	}
}

func TestSpecLintFileOfAConventionalChangeReportsNothing(t *testing.T) {
	_, url := liveWith(t, conventionalSpec)

	stdout, _, err := runRemoteSpec(t, url, "lint", "pets-api", "-f", writeSpec(t, conventionalSpec+camelCaseToys),
		"--fail-on-findings")

	if err != nil || !strings.Contains(stdout, "adds no findings to pets-api 1.4.0 (Live)") {
		t.Errorf("stdout %q, err %v", stdout, err)
	}
}

func TestSpecLintFileFailOnFindingsExitsOne(t *testing.T) {
	_, url := liveWith(t, conventionalSpec)

	_, _, err := runRemoteSpec(t, url, "lint", "pets-api", "-f", writeSpec(t, conventionalSpec+snakeCaseOwners),
		"--fail-on-findings")

	if exitCode(err) != 1 {
		t.Errorf("exit %d", exitCode(err))
	}
}

func TestSpecLintFileRefusesAVersion(t *testing.T) {
	fake, url := liveWith(t, conventionalSpec)

	_, _, err := runRemoteSpec(t, url, "lint", "pets-api", "-f", writeSpec(t, conventionalSpec), "--version", "1.3.0")

	if err == nil || !strings.Contains(err.Error(), "--version and -f cannot be used together") {
		t.Errorf("err = %v", err)
	}
	if len(fake.requests) != 0 {
		t.Errorf("requests were made: %v", fake.requests)
	}
}

func TestSpecLintFileNamesAnUnreadableFile(t *testing.T) {
	_, url := liveWith(t, conventionalSpec)
	path := writeSpec(t, "swagger: \"2.0\"\n")

	_, _, err := runRemoteSpec(t, url, "lint", "pets-api", "-f", path)

	if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "swagger 2.0") {
		t.Errorf("err = %v", err)
	}
}

func TestSpecLintFileOfAnUnknownSpecSuggestsList(t *testing.T) {
	fake, server := newSpecServer(t)
	fake.statuses["GET /specs/pets-api/document"] = http.StatusNotFound

	_, _, err := runRemoteSpec(t, server.URL, "lint", "pets-api", "-f", writeSpec(t, conventionalSpec))

	if err == nil || !strings.Contains(err.Error(), `no spec with slug "pets-api"`) {
		t.Errorf("err = %v", err)
	}
}
