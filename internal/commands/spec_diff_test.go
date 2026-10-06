package commands

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/output"
)

const withOwners = specFixture + `  /owners:
    get:
      operationId: listOwners
      responses:
        "200": {description: OK}
`

func storedChange(id, severity, method, path, text string) api.SpecChange {
	change := api.SpecChange{Id: id, Severity: api.SpecChangeSeverity(severity), Text: text}
	if method != "" {
		change.Method, change.Path = &method, &path
	}
	return change
}

// diffServer is a fake EchoPoint whose Live version is 1.4.0, with the changes
// of 1.4.0 and 1.3.0 stored, and a document for each of three versions.
func diffServer(t *testing.T) (*specServer, string) {
	t.Helper()
	fake, server := newSpecServer(t)
	fake.responses["GET /specs/pets-api"] = specOf("pets-api", "Pets", "1.4.0")
	fake.responses["GET /specs/pets-api/versions/1.4.0"] = api.SpecVersion{
		Version: "1.4.0", Bump: api.SpecBump("major"),
		Changes: []api.SpecChange{
			storedChange("endpoint-removed", "breaking", "DELETE", "/pets/{id}", "endpoint removed"),
			storedChange("endpoint-added", "additive", "GET", "/owners", "endpoint added"),
			storedChange("description-changed", "edit", "", "", "description changed"),
		},
	}
	fake.responses["GET /specs/pets-api/versions/1.3.0"] = api.SpecVersion{
		Version: "1.3.0", Bump: api.SpecBump("minor"),
		Changes: []api.SpecChange{storedChange("endpoint-added", "additive", "GET", "/pets", "endpoint added")},
	}
	fake.responses["GET /specs/pets-api/versions/1.0.0"] = api.SpecVersion{Version: "1.0.0", Bump: api.Initial}
	fake.responses["GET /specs/pets-api/document?version=1.2.0"] = pulledDocument(specFixture)
	fake.responses["GET /specs/pets-api/document?version=1.3.0"] = pulledDocument(specFixture)
	fake.responses["GET /specs/pets-api/document?version=1.4.0"] = pulledDocument(withOwners)
	return fake, server.URL
}

func TestSpecDiffDefaultsToLiveAgainstTheVersionBeforeIt(t *testing.T) {
	fake, url := diffServer(t)

	stdout, _, err := runRemoteSpec(t, url, "diff", "pets-api")

	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Changes in pets-api 1.4.0 against the version before it",
		"Breaks clients (1)\n  - DELETE /pets/{id}: endpoint removed",
		"Adds (1)\n  - GET /owners: endpoint added",
		"Edits (1)\n  - description changed",
		"Version bump: major",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout misses %q:\n%s", want, stdout)
		}
	}
	if want := "GET /specs/pets-api,GET /specs/pets-api/versions/1.4.0"; strings.Join(fake.requests, ",") != want {
		t.Errorf("requests = %v", fake.requests)
	}
}

func TestSpecDiffToDefaultsFromToTheVersionBeforeIt(t *testing.T) {
	fake, url := diffServer(t)

	stdout, _, err := runRemoteSpec(t, url, "diff", "pets-api", "--to", "1.3.0")

	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "Changes in pets-api 1.3.0") || !strings.Contains(stdout, "Version bump: minor") {
		t.Errorf("stdout:\n%s", stdout)
	}
	if want := "GET /specs/pets-api/versions/1.3.0"; strings.Join(fake.requests, ",") != want {
		t.Errorf("requests = %v", fake.requests)
	}
}

func TestSpecDiffFromComparesThatVersionWithLive(t *testing.T) {
	fake, url := diffServer(t)

	stdout, _, err := runRemoteSpec(t, url, "diff", "pets-api", "--from", "1.2.0")

	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Changes in pets-api from 1.2.0 to 1.4.0", "Adds (", "GET /owners", "Version bump: minor"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout misses %q:\n%s", want, stdout)
		}
	}
	want := "GET /specs/pets-api,GET /specs/pets-api/document?version=1.2.0,GET /specs/pets-api/document?version=1.4.0"
	if strings.Join(fake.requests, ",") != want {
		t.Errorf("requests = %v", fake.requests)
	}
}

func TestSpecDiffFromAndToCompareTwoVersions(t *testing.T) {
	fake, url := diffServer(t)

	stdout, _, err := runRemoteSpec(t, url, "diff", "pets-api", "--from", "1.2.0", "--to", "1.3.0")

	if err != nil || strings.TrimSpace(strings.SplitN(stdout, "\n\n", 2)[1]) != "No changes." {
		t.Errorf("stdout %q, err %v", stdout, err)
	}
	want := "GET /specs/pets-api/document?version=1.2.0,GET /specs/pets-api/document?version=1.3.0"
	if strings.Join(fake.requests, ",") != want {
		t.Errorf("requests = %v", fake.requests)
	}
}

func TestSpecDiffJSONHasTheBumpAndTheChanges(t *testing.T) {
	_, url := diffServer(t)

	stdout, _, err := runRemoteSpecAs(t, url, output.FormatJSON, "diff", "pets-api")

	if err != nil {
		t.Fatal(err)
	}
	var diff specDiff
	if err = json.Unmarshal([]byte(stdout), &diff); err != nil {
		t.Fatalf("decode %q: %v", stdout, err)
	}
	if diff.Bump != "major" || len(diff.Changes) != 3 || diff.Changes[0].ID != "endpoint-removed" ||
		diff.Changes[0].Method != "DELETE" || diff.Changes[0].Path != "/pets/{id}" {
		t.Errorf("diff = %+v", diff)
	}
}

func TestSpecDiffOfTheFirstVersionHasNothingToCompareWith(t *testing.T) {
	_, url := diffServer(t)

	_, _, err := runRemoteSpec(t, url, "diff", "pets-api", "--to", "1.0.0")

	if err == nil || !strings.Contains(err.Error(), "pets-api 1.0.0 is the first version of the spec") {
		t.Errorf("err = %v", err)
	}
}

func TestSpecDiffOfAnUnknownVersionNamesTheVersion(t *testing.T) {
	_, url := diffServer(t)

	_, _, err := runRemoteSpec(t, url, "diff", "pets-api", "--to", "9.9.9")

	if err == nil || !strings.Contains(err.Error(), `pets-api has no version "9.9.9"`) {
		t.Errorf("err = %v", err)
	}
}

func TestSpecDiffFileShowsWhatPushingWouldChangeInLive(t *testing.T) {
	fake, url := liveWith(t, specFixture)
	path := writeSpec(t, withOwners)

	stdout, _, err := runRemoteSpec(t, url, "diff", "pets-api", "-f", path)

	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Pushing " + path + " would change pets-api 1.4.0 (Live)", "Adds (1)\n  - GET /owners", "Version bump: minor",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout misses %q:\n%s", want, stdout)
		}
	}
	if strings.Join(fake.requests, ",") != "GET /specs/pets-api/document" {
		t.Errorf("requests = %v", fake.requests)
	}
	for method := range fake.bodies {
		if strings.HasPrefix(method, "POST") {
			t.Errorf("diff -f sent a %s", method)
		}
	}
}

func TestSpecDiffFileGroupsABreakingChange(t *testing.T) {
	_, url := liveWith(t, specFixture)

	stdout, _, err := runRemoteSpec(t, url, "diff", "pets-api", "-f",
		writeSpec(t, strings.Replace(specFixture, "  /pets:", "  /animals:", 1)))

	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Breaks clients", "Adds", "Version bump: major"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("diff output misses %q:\n%s", want, stdout)
		}
	}
}

func TestSpecDiffFileOfTheLiveDocumentReportsNoChanges(t *testing.T) {
	_, url := liveWith(t, canonicalSpecFixture)

	stdout, _, err := runRemoteSpec(t, url, "diff", "pets-api", "-f", writeSpec(t, specFixture))

	if err != nil || !strings.HasSuffix(strings.TrimSpace(stdout), "No changes.") {
		t.Errorf("stdout %q, err %v", stdout, err)
	}
}

func TestSpecDiffFileJSONHasTheBumpAndTheChanges(t *testing.T) {
	_, url := liveWith(t, specFixture)

	stdout, _, err := runRemoteSpecAs(t, url, output.FormatJSON, "diff", "pets-api", "-f", writeSpec(t, withOwners))

	if err != nil {
		t.Fatal(err)
	}
	var diff specDiff
	if err = json.Unmarshal([]byte(stdout), &diff); err != nil {
		t.Fatal(err)
	}
	if diff.Bump != "minor" || len(diff.Changes) == 0 || diff.Changes[0].Severity != "additive" {
		t.Errorf("diff = %+v", diff)
	}
}

func TestSpecDiffFileRefusesFromAndTo(t *testing.T) {
	fake, url := liveWith(t, specFixture)
	path := writeSpec(t, specFixture)

	for _, flag := range []string{"--from", "--to"} {
		_, _, err := runRemoteSpec(t, url, "diff", "pets-api", "-f", path, flag, "1.2.0")
		if err == nil || !strings.Contains(err.Error(), "--from and --to cannot be used with -f") {
			t.Errorf("%s: err = %v", flag, err)
		}
	}
	if len(fake.requests) != 0 {
		t.Errorf("requests were made: %v", fake.requests)
	}
}

func TestSpecDiffOfAnUnknownSpecSuggestsList(t *testing.T) {
	fake, server := newSpecServer(t)
	fake.statuses["GET /specs/nope"] = http.StatusNotFound

	_, _, err := runRemoteSpec(t, server.URL, "diff", "nope")

	if err == nil || !strings.Contains(err.Error(), `no spec with slug "nope"`) {
		t.Errorf("err = %v", err)
	}
}
