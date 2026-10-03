package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"echopoint-cli/internal/output"
)

const specFixture = `info: {version: 1.0.0, title: Pets}
openapi: 3.0.3
paths:
  /pets:
    get:
      responses:
        "200": {description: OK}
      operationId: listPets
`

const canonicalSpecFixture = `openapi: 3.0.3
info:
  title: Pets
  version: 1.0.0
paths:
  /pets:
    get:
      operationId: listPets
      responses:
        "200":
          description: OK
`

func writeSpec(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "openapi.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func runSpec(t *testing.T, format output.Format, args ...string) (string, string, error) {
	t.Helper()
	cmd := newSpecCmd(&AppState{OutputFormat: format})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func exitCode(err error) int {
	if coded, ok := errors.AsType[*exitCodeError](err); ok {
		return coded.ExitCode()
	}
	if err != nil {
		return -1
	}
	return 0
}

func TestSpecValidateAcceptsValidDocument(t *testing.T) {
	stdout, _, err := runSpec(t, output.FormatTable, "validate", writeSpec(t, specFixture))
	if err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	if !strings.Contains(stdout, "valid OpenAPI 3.0.3 document") {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestSpecValidateRefusesSwagger2(t *testing.T) {
	_, stderr, err := runSpec(t, output.FormatTable, "validate",
		writeSpec(t, "swagger: \"2.0\"\ninfo: {title: Pets, version: 1.0.0}\npaths: {}\n"))
	if exitCode(err) != 1 || !strings.Contains(stderr, "swagger 2.0 is not supported") {
		t.Errorf("exit %d, stderr %q", exitCode(err), stderr)
	}
}

func TestSpecValidateReportsExternalRefWithItsPointer(t *testing.T) {
	spec := strings.Replace(specFixture, `"200": {description: OK}`, `"200": {$ref: "responses.yaml#/Ok"}`, 1)
	stdout, _, err := runSpec(t, output.FormatJSON, "validate", writeSpec(t, spec))
	if exitCode(err) != 1 {
		t.Fatalf("exit %d, want 1", exitCode(err))
	}
	var result specValidation
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatal(err)
	}
	if result.Valid || len(result.Problems) != 1 ||
		!strings.Contains(result.Problems[0], "/paths/~1pets/get/responses/200/$ref") {
		t.Errorf("result = %+v", result)
	}
}

func TestSpecValidateReportsInvalidDocument(t *testing.T) {
	spec := strings.Replace(specFixture, "title: Pets", "", 1)
	_, stderr, err := runSpec(t, output.FormatTable, "validate", writeSpec(t, spec))
	if exitCode(err) != 1 || !strings.Contains(stderr, "problem(s)") {
		t.Errorf("exit %d, stderr %q", exitCode(err), stderr)
	}
}

func TestSpecFmtPrintsTheCanonicalLayout(t *testing.T) {
	stdout, _, err := runSpec(t, output.FormatTable, "fmt", writeSpec(t, specFixture))
	if err != nil {
		t.Fatal(err)
	}
	if stdout != canonicalSpecFixture {
		t.Errorf("fmt output:\n%s\nwant:\n%s", stdout, canonicalSpecFixture)
	}
}

func TestSpecFmtWriteRewritesTheFile(t *testing.T) {
	path := writeSpec(t, specFixture)
	if _, _, err := runSpec(t, output.FormatTable, "fmt", "--write", path); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != canonicalSpecFixture {
		t.Errorf("file after fmt --write:\n%s", written)
	}
}

func TestSpecFmtCheckFailsOnNonCanonicalFile(t *testing.T) {
	path := writeSpec(t, specFixture)
	_, stderr, err := runSpec(t, output.FormatTable, "fmt", "--check", path)
	if exitCode(err) != 1 || !strings.Contains(stderr, "not in the canonical layout") {
		t.Errorf("exit %d, stderr %q", exitCode(err), stderr)
	}
	if unchanged, _ := os.ReadFile(path); string(unchanged) != specFixture {
		t.Error("fmt --check modified the file")
	}
}

func TestSpecFmtCheckPassesOnCanonicalFile(t *testing.T) {
	if _, _, err := runSpec(t, output.FormatTable, "fmt", "--check", writeSpec(t, canonicalSpecFixture)); err != nil {
		t.Errorf("fmt --check on a canonical file: %v", err)
	}
}

func TestSpecDiffReportsBumpAndChanges(t *testing.T) {
	base := writeSpec(t, specFixture)
	revision := writeSpec(t, specFixture+`  /owners:
    get:
      operationId: listOwners
      responses:
        "200": {description: OK}
`)
	stdout, _, err := runSpec(t, output.FormatJSON, "diff", base, revision)
	if err != nil {
		t.Fatal(err)
	}
	var diff specDiff
	if err := json.Unmarshal([]byte(stdout), &diff); err != nil {
		t.Fatal(err)
	}
	if diff.Bump != "minor" || len(diff.Changes) == 0 || diff.Changes[0].Severity != "additive" {
		t.Errorf("diff = %+v", diff)
	}
}

func TestSpecDiffGroupsChangesForReading(t *testing.T) {
	base := writeSpec(t, specFixture)
	revision := writeSpec(t, strings.Replace(specFixture, "  /pets:", "  /animals:", 1))
	stdout, _, err := runSpec(t, output.FormatTable, "diff", base, revision)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Breaks clients", "Adds", "Version bump: major"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("diff output misses %q:\n%s", want, stdout)
		}
	}
}

func TestSpecDiffOfIdenticalDocumentsReportsNoChanges(t *testing.T) {
	stdout, _, err := runSpec(
		t,
		output.FormatTable,
		"diff",
		writeSpec(t, specFixture),
		writeSpec(t, canonicalSpecFixture),
	)
	if err != nil || strings.TrimSpace(stdout) != "No changes." {
		t.Errorf("stdout %q, err %v", stdout, err)
	}
}
