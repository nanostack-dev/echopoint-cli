package commands

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nanostack-dev/echopoint-kit/apispec"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/output"
)

// specServer records the last request body per path and answers with the
// response registered for "METHOD path", or for "METHOD path?query" when that
// one is registered.
type specServer struct {
	responses map[string]any
	statuses  map[string]int
	bodies    map[string][]byte
	queries   map[string]string
	requests  []string
}

func newSpecServer(t *testing.T) (*specServer, *httptest.Server) {
	t.Helper()
	fake := &specServer{
		responses: map[string]any{},
		statuses:  map[string]int{},
		bodies:    map[string][]byte{},
		queries:   map[string]string{},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		body, _ := io.ReadAll(r.Body)
		fake.bodies[key] = body
		fake.queries[key] = r.URL.RawQuery
		fake.requests = append(fake.requests, r.Method+" "+r.URL.RequestURI())
		if _, ok := fake.responses[r.Method+" "+r.URL.RequestURI()]; ok {
			key = r.Method + " " + r.URL.RequestURI()
		}
		response, ok := fake.responses[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		status := fake.statuses[key]
		if status == 0 {
			status = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(response)
	}))
	t.Cleanup(server.Close)
	return fake, server
}

func apiError(code, message string) map[string]any {
	return map[string]any{"errors": []map[string]any{{"code": code, "message": message}}}
}

func runRemoteSpec(t *testing.T, serverURL string, args ...string) (string, string, error) {
	t.Helper()
	return runRemoteSpecAs(t, serverURL, output.FormatTable, args...)
}

func runRemoteSpecAs(t *testing.T, serverURL string, format output.Format, args ...string) (string, string, error) {
	t.Helper()
	state := makeState(t, "test-api-key", "", serverURL)
	state.OutputFormat = format
	state.Config.FrontendURL = "https://echopoint.test/"
	cmd := newSpecCmd(state)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func pulledDocument(document string) api.SpecDocument {
	return api.SpecDocument{
		Document:      document,
		LayoutVersion: 1,
		Sha256:        "abc",
		SnapshotId:    "live:1",
		Version:       "1.4.0",
	}
}

func liveSummary(version string) api.SpecVersionSummary {
	return api.SpecVersionSummary{
		Version:        version,
		Bump:           api.SpecBump("minor"),
		CreatedAt:      time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC),
		CreatedBy:      api.SpecActor{Id: "key-9", Type: api.SpecActorTypeApiKey},
		OpenapiVersion: "3.0.3",
		FindingCount:   new(int32(3)),
		ChangeCounts:   api.SpecChangeCounts{Breaking: 1, Additive: 2},
	}
}

func specOf(name, title, version string) api.Spec {
	return api.Spec{
		Slug:      name,
		Title:     title,
		Live:      liveSummary(version),
		CreatedAt: time.Date(2026, 9, 1, 8, 30, 0, 0, time.UTC),
		CreatedBy: api.SpecActor{Id: "user-1", Type: api.SpecActorTypeMember},
		UpdatedAt: time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC),
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// line is the first line of text that starts with prefix, its columns
// separated by single spaces.
func line(text, prefix string) string {
	for l := range strings.SplitSeq(text, "\n") {
		if strings.HasPrefix(l, prefix) {
			return strings.Join(strings.Fields(l), " ")
		}
	}
	return ""
}

func TestSpecCreateCreatesTheSpecNamedByTheFirstArgument(t *testing.T) {
	fake, server := newSpecServer(t)
	fake.responses["POST /specs"] = api.Spec{Slug: "pets-api", Live: liveSummary("0.3.0")}
	fake.statuses["POST /specs"] = http.StatusCreated

	stdout, _, err := runRemoteSpec(t, server.URL, "create", "pets-api", "-f", writeSpec(t, specFixture))

	if err != nil {
		t.Fatal(err)
	}
	var sent api.CreateSpecRequest
	if err := json.Unmarshal(fake.bodies["POST /specs"], &sent); err != nil {
		t.Fatal(err)
	}
	if sent.Slug != "pets-api" || sent.Document != specFixture {
		t.Errorf("sent %+v", sent)
	}
	if !strings.Contains(stdout, "Created spec pets-api: Live version 0.3.0") {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestSpecCreateAcceptsTheLongFileFlagAndBundle(t *testing.T) {
	fake, server := newSpecServer(t)
	fake.responses["POST /specs"] = api.Spec{Slug: "pets-api", Live: liveSummary("0.3.0")}
	fake.statuses["POST /specs"] = http.StatusCreated
	dir := t.TempDir()
	root := filepath.Join(dir, "openapi.yaml")
	writeFile(
		t,
		root,
		strings.Replace(specFixture, `"200": {description: OK}`, `"200": {$ref: "responses.yaml#/Ok"}`, 1),
	)
	writeFile(t, filepath.Join(dir, "responses.yaml"), "Ok:\n  description: OK\n")

	_, _, err := runRemoteSpec(t, server.URL, "create", "pets-api", "--file", root, "--bundle")

	if err != nil {
		t.Fatal(err)
	}
	var sent api.CreateSpecRequest
	if err := json.Unmarshal(fake.bodies["POST /specs"], &sent); err != nil {
		t.Fatal(err)
	}
	if _, err := apispec.Parse([]byte(sent.Document)); err != nil {
		t.Errorf("the bundled document is not self-contained: %v", err)
	}
}

func TestSpecCreateRefusesATakenName(t *testing.T) {
	fake, server := newSpecServer(t)
	fake.responses["POST /specs"] = apiError("SPEC_SLUG_TAKEN", "A spec named pets-api already exists.")
	fake.statuses["POST /specs"] = http.StatusConflict

	_, _, err := runRemoteSpec(t, server.URL, "create", "pets-api", "-f", writeSpec(t, specFixture))

	if err == nil || !strings.Contains(err.Error(), "api error (409): A spec named pets-api already exists.") {
		t.Errorf("err = %v", err)
	}
}

func TestSpecCommandsThatReadAFileRequireIt(t *testing.T) {
	_, server := newSpecServer(t)
	for _, command := range []string{"create", "push", "check"} {
		_, _, err := runRemoteSpec(t, server.URL, command, "pets-api")
		if err == nil || !strings.Contains(err.Error(), `required flag(s) "file" not set`) {
			t.Errorf("%s: err = %v", command, err)
		}
	}
}

func TestSpecPushPublishesTheNextVersionOfTheNamedSpec(t *testing.T) {
	fake, server := newSpecServer(t)
	path := "/owners"
	fake.responses["POST /specs/pets-api/versions"] = api.SpecVersion{
		Version: "0.4.0",
		Bump:    api.SpecBump("minor"),
		Changes: []api.SpecChange{{
			Id: "endpoint-added", Severity: api.SpecChangeSeverity("additive"),
			Method: new("GET"), Path: &path, Text: "endpoint added",
		}},
	}
	fake.statuses["POST /specs/pets-api/versions"] = http.StatusCreated

	stdout, _, err := runRemoteSpec(t, server.URL, "push", "pets-api", "-f", writeSpec(t, specFixture))

	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Published pets-api 0.4.0 (minor)", "Adds (1)", "GET /owners: endpoint added"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout misses %q:\n%s", want, stdout)
		}
	}
}

func TestSpecPushReportsTheAPIRefusal(t *testing.T) {
	fake, server := newSpecServer(t)
	fake.responses["POST /specs/pets-api/versions"] = apiError("SPEC_NOTHING_TO_PUBLISH",
		"Nothing to publish: the document is identical to the Live version.")
	fake.statuses["POST /specs/pets-api/versions"] = http.StatusConflict

	_, _, err := runRemoteSpec(t, server.URL, "push", "pets-api", "-f", writeSpec(t, specFixture))

	if err == nil || !strings.Contains(err.Error(), "api error (409): Nothing to publish") {
		t.Errorf("err = %v", err)
	}
}

func TestSpecPushToAnUnknownSpecNamesItAndSuggestsList(t *testing.T) {
	fake, server := newSpecServer(t)
	fake.responses["POST /specs/pets-api/versions"] = apiError("SPEC_NOT_FOUND", "The spec does not exist.")
	fake.statuses["POST /specs/pets-api/versions"] = http.StatusNotFound

	_, _, err := runRemoteSpec(t, server.URL, "push", "pets-api", "-f", writeSpec(t, specFixture))

	want := `api error (404): no spec with slug "pets-api"; list the specs with: echopoint spec list`
	if err == nil || err.Error() != want {
		t.Errorf("err = %v", err)
	}
}

func TestSpecPushBundleInlinesExternalRefs(t *testing.T) {
	fake, server := newSpecServer(t)
	fake.responses["POST /specs/pets-api/versions"] = api.SpecVersion{Version: "0.4.0", Bump: api.SpecBump("patch")}
	fake.statuses["POST /specs/pets-api/versions"] = http.StatusCreated
	dir := t.TempDir()
	root := filepath.Join(dir, "openapi.yaml")
	writeFile(
		t,
		root,
		strings.Replace(specFixture, `"200": {description: OK}`, `"200": {$ref: "responses.yaml#/Ok"}`, 1),
	)
	writeFile(t, filepath.Join(dir, "responses.yaml"), "Ok:\n  description: OK\n")

	_, _, err := runRemoteSpec(t, server.URL, "push", "pets-api", "--bundle", "-f", root)

	if err != nil {
		t.Fatal(err)
	}
	var sent api.PushSpecVersionRequest
	if err := json.Unmarshal(fake.bodies["POST /specs/pets-api/versions"], &sent); err != nil {
		t.Fatal(err)
	}
	if _, err := apispec.Parse([]byte(sent.Document)); err != nil {
		t.Errorf("the bundled document is not self-contained: %v", err)
	}
}

func TestSpecPushWithoutBundleSendsTheFileAsIs(t *testing.T) {
	fake, server := newSpecServer(t)
	fake.responses["POST /specs/pets-api/versions"] = api.SpecVersion{Version: "0.4.0", Bump: api.SpecBump("patch")}
	fake.statuses["POST /specs/pets-api/versions"] = http.StatusCreated

	if _, _, err := runRemoteSpec(t, server.URL, "push", "pets-api", "-f", writeSpec(t, specFixture)); err != nil {
		t.Fatal(err)
	}
	var sent api.PushSpecVersionRequest
	if err := json.Unmarshal(fake.bodies["POST /specs/pets-api/versions"], &sent); err != nil {
		t.Fatal(err)
	}
	if sent.Document != specFixture {
		t.Errorf("sent document:\n%s", sent.Document)
	}
}

func TestSpecPushListsTheIntroducedFindings(t *testing.T) {
	fake, server := newSpecServer(t)
	fake.responses["POST /specs/pets-api/versions"] = publishedVersion("0.4.0",
		api.SpecFinding{
			Rule: api.SpecLintRule("operation-id-casing"), Pointer: "/paths/~1owners/get/operationId",
			Message: "list_owners is not camelCase", Introduced: true,
		},
		api.SpecFinding{Rule: api.SpecLintRule("missing-security"), Pointer: "/paths/~1old/get", Message: "old"})
	fake.statuses["POST /specs/pets-api/versions"] = http.StatusCreated

	stdout, _, err := runRemoteSpec(t, server.URL, "push", "pets-api", "-f", writeSpec(t, specFixture))

	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "New findings (1)") ||
		!strings.Contains(
			stdout,
			"operation-id-casing /paths/~1owners/get/operationId: list_owners is not camelCase",
		) ||
		strings.Contains(stdout, "~1old") {
		t.Errorf("stdout:\n%s", stdout)
	}
}

func TestSpecPullWritesTheLiveDocumentByteForByte(t *testing.T) {
	fake, server := newSpecServer(t)
	fake.responses["GET /specs/pets-api/document"] = pulledDocument(canonicalSpecFixture)
	path := filepath.Join(t.TempDir(), "openapi.yaml")

	stdout, stderr, err := runRemoteSpec(t, server.URL, "pull", "pets-api", "-f", path)

	if err != nil {
		t.Fatal(err)
	}
	if written, _ := os.ReadFile(path); string(written) != canonicalSpecFixture {
		t.Errorf("written:\n%s", written)
	}
	if stdout != "" || !strings.Contains(stderr, "✓ Wrote pets-api 1.4.0 to "+path) {
		t.Errorf("stdout %q, stderr %q", stdout, stderr)
	}
}

func TestSpecPullWithoutAFileWritesTheDocumentToStdout(t *testing.T) {
	fake, server := newSpecServer(t)
	fake.responses["GET /specs/pets-api/document"] = pulledDocument(canonicalSpecFixture)
	dir := t.TempDir()
	t.Chdir(dir)

	for _, args := range [][]string{{"pull", "pets-api"}, {"pull", "pets-api", "-f", "-"}, {"pull", "pets-api", "--file", "-"}} {
		stdout, stderr, err := runRemoteSpec(t, server.URL, args...)

		if err != nil {
			t.Fatal(err)
		}
		if stdout != canonicalSpecFixture || stderr != "" {
			t.Errorf("%v: stdout %q, stderr %q", args, stdout, stderr)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("pull to stdout created %d file(s)", len(entries))
	}
}

func TestSpecPullVersionAsksForThatVersion(t *testing.T) {
	fake, server := newSpecServer(t)
	fake.responses["GET /specs/pets-api/document"] = pulledDocument(canonicalSpecFixture)

	_, _, err := runRemoteSpec(t, server.URL, "pull", "pets-api", "--version", "1.2.0",
		"-f", filepath.Join(t.TempDir(), "openapi.yaml"))

	if err != nil {
		t.Fatal(err)
	}
	if query := fake.queries["GET /specs/pets-api/document"]; query != "version=1.2.0" {
		t.Errorf("query = %q", query)
	}
}

func TestSpecPullOfAnUnknownVersionNamesTheVersion(t *testing.T) {
	fake, server := newSpecServer(t)
	fake.responses["GET /specs/pets-api/document?version=9.9.9"] = apiError("SPEC_VERSION_NOT_FOUND",
		"The spec has no Live version \"9.9.9\".")
	fake.statuses["GET /specs/pets-api/document?version=9.9.9"] = http.StatusNotFound

	_, _, err := runRemoteSpec(t, server.URL, "pull", "pets-api", "--version", "9.9.9")

	want := `api error (404): pets-api has no version "9.9.9"; list its versions with: echopoint spec versions pets-api`
	if err == nil || err.Error() != want {
		t.Errorf("err = %v", err)
	}
}

func TestSpecCheckPassesWhenTheFileMatchesLive(t *testing.T) {
	fake, server := newSpecServer(t)
	fake.responses["GET /specs/pets-api/document"] = pulledDocument(canonicalSpecFixture)

	stdout, _, err := runRemoteSpec(t, server.URL, "check", "pets-api", "-f", writeSpec(t, canonicalSpecFixture))

	if err != nil || !strings.Contains(stdout, "matches pets-api 1.4.0") {
		t.Errorf("stdout %q, err %v", stdout, err)
	}
}

func TestSpecCheckFailsOnDrift(t *testing.T) {
	fake, server := newSpecServer(t)
	fake.responses["GET /specs/pets-api/document"] = pulledDocument(canonicalSpecFixture)
	path := writeSpec(t, specFixture)

	_, stderr, err := runRemoteSpec(t, server.URL, "check", "pets-api", "-f", path)

	if exitCode(err) != 1 || !strings.Contains(stderr, "Run: echopoint spec pull pets-api -f "+path) {
		t.Errorf("exit %d, stderr %q", exitCode(err), stderr)
	}
}

func TestSpecCheckFailsWhenTheFileIsMissing(t *testing.T) {
	fake, server := newSpecServer(t)
	fake.responses["GET /specs/pets-api/document"] = pulledDocument(canonicalSpecFixture)

	_, _, err := runRemoteSpec(t, server.URL, "check", "pets-api", "-f", filepath.Join(t.TempDir(), "openapi.yaml"))

	if exitCode(err) != 1 {
		t.Errorf("exit %d, want 1", exitCode(err))
	}
}

func TestSpecRemoteRefusalsPrintNoUsage(t *testing.T) {
	fake, server := newSpecServer(t)
	fake.responses["GET /specs/pets-api/document"] = pulledDocument(canonicalSpecFixture)

	_, stderr, err := runRemoteSpec(t, server.URL, "check", "pets-api", "-f", writeSpec(t, specFixture))

	if exitCode(err) != 1 || strings.Contains(stderr, "Usage:") || strings.Contains(stderr, "Error:") {
		t.Errorf("exit %d, stderr %q", exitCode(err), stderr)
	}
}

func TestSpecListReturnsNamesAndLiveVersions(t *testing.T) {
	fake, server := newSpecServer(t)
	fake.responses["GET /specs"] = api.SpecListResponse{
		Count: 1, Total: 1,
		Items: []api.Spec{specOf("pets-api", "Pets", "1.4.0")},
	}

	stdout, _, err := runRemoteSpecAs(t, server.URL, output.FormatJSON, "list")

	if err != nil {
		t.Fatal(err)
	}
	var listed api.SpecListResponse
	if err := json.Unmarshal([]byte(stdout), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Items) != 1 || listed.Items[0].Slug != "pets-api" || listed.Items[0].Live.Version != "1.4.0" {
		t.Errorf("listed = %+v", listed)
	}
	table, _, err := runRemoteSpec(t, server.URL, "list")
	if err != nil || line(table, "SLUG") != "SLUG TITLE LIVE BUMP PUBLISHED" ||
		line(table, "pets-api") != "pets-api Pets 1.4.0 minor 2026-10-03 12:00" {
		t.Errorf("table %q, err %v", table, err)
	}
}

func TestSpecYAMLOutputUsesTheAPIKeys(t *testing.T) {
	fake, server := newSpecServer(t)
	fake.responses["GET /specs/pets-api"] = specOf("pets-api", "Pets", "1.4.0")

	stdout, _, err := runRemoteSpecAs(t, server.URL, output.FormatYAML, "view", "pets-api")

	if err != nil || !strings.Contains(stdout, "slug: pets-api") || !strings.Contains(stdout, "created_at:") {
		t.Errorf("stdout %q, err %v", stdout, err)
	}
}

func TestSpecViewShowsTheSpecAndItsLiveVersion(t *testing.T) {
	fake, server := newSpecServer(t)
	fake.responses["GET /specs/pets-api"] = specOf("pets-api", "Pets", "1.4.0")

	stdout, _, err := runRemoteSpec(t, server.URL, "view", "pets-api")

	if err != nil {
		t.Fatal(err)
	}
	for prefix, want := range map[string]string{
		"Title:":     "Title: Pets",
		"Slug:":      "Slug: pets-api",
		"Live:":      "Live: 1.4.0 (minor)",
		"OpenAPI:":   "OpenAPI: 3.0.3",
		"Findings:":  "Findings: 3",
		"Changes:":   "Changes: 1 breaking, 2 additive",
		"Published:": "Published: 2026-10-03 12:00 by API key key-9",
		"Created:":   "Created: 2026-09-01 08:30 by member user-1",
		"Updated:":   "Updated: 2026-10-03 12:00",
		"URL:":       "URL: https://echopoint.test/api/specs/pets-api",
	} {
		if got := line(stdout, prefix); got != want {
			t.Errorf("%s line = %q, want %q\n%s", prefix, got, want, stdout)
		}
	}
	if got := fake.requests; len(got) != 1 || got[0] != "GET /specs/pets-api" {
		t.Errorf("requests = %v", got)
	}
}

func TestSpecViewOfAVersionWithoutStoredFindingsSaysSo(t *testing.T) {
	fake, server := newSpecServer(t)
	spec := specOf("pets-api", "Pets", "1.4.0")
	spec.Live.FindingCount = nil
	spec.Live.ChangeCounts = api.SpecChangeCounts{}
	fake.responses["GET /specs/pets-api"] = spec

	stdout, _, err := runRemoteSpec(t, server.URL, "view", "pets-api")

	if err != nil || line(stdout, "Findings:") != "Findings: not recorded" ||
		line(stdout, "Changes:") != "Changes: none" {
		t.Errorf("stdout %q, err %v", stdout, err)
	}
}

func TestSpecViewJSONIsTheSpec(t *testing.T) {
	fake, server := newSpecServer(t)
	fake.responses["GET /specs/pets-api"] = specOf("pets-api", "Pets", "1.4.0")

	stdout, _, err := runRemoteSpecAs(t, server.URL, output.FormatJSON, "view", "pets-api")

	var got api.Spec
	if err != nil || json.Unmarshal([]byte(stdout), &got) != nil || got.Slug != "pets-api" ||
		got.Live.Version != "1.4.0" {
		t.Errorf("stdout %q, err %v", stdout, err)
	}
}

func TestSpecViewOfAnUnknownNameSaysSoAndSuggestsList(t *testing.T) {
	fake, server := newSpecServer(t)
	fake.responses["GET /specs/nope"] = apiError("SPEC_NOT_FOUND", "The spec does not exist.")
	fake.statuses["GET /specs/nope"] = http.StatusNotFound

	_, _, err := runRemoteSpec(t, server.URL, "view", "nope")

	want := `api error (404): no spec with slug "nope"; list the specs with: echopoint spec list`
	if err == nil || err.Error() != want {
		t.Errorf("err = %v", err)
	}
}

func TestSpecAPIErrorsOtherThanNotFoundKeepTheStatusAndTheMessage(t *testing.T) {
	fake, server := newSpecServer(t)
	fake.responses["GET /specs/pets-api"] = apiError("FORBIDDEN", "Missing permission specs:read.")
	fake.statuses["GET /specs/pets-api"] = http.StatusForbidden

	_, _, err := runRemoteSpec(t, server.URL, "view", "pets-api")

	if err == nil || err.Error() != "api error (403): Missing permission specs:read." {
		t.Errorf("err = %v", err)
	}
}

func versionOf(version, bump string, counts api.SpecChangeCounts, findings *int32) api.SpecVersionSummary {
	summary := liveSummary(version)
	summary.Bump = api.SpecBump(bump)
	summary.ChangeCounts = counts
	summary.FindingCount = findings
	return summary
}

func TestSpecVersionsListsTheHistoryNewestFirst(t *testing.T) {
	fake, server := newSpecServer(t)
	older := versionOf("1.3.0", "patch", api.SpecChangeCounts{Edit: 4}, nil)
	older.CreatedAt = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	older.CreatedBy = api.SpecActor{Id: "user-1", Type: api.SpecActorTypeMember}
	fake.responses["GET /specs/pets-api/versions"] = api.SpecVersionListResponse{
		Count: 2,
		Total: 7,
		Items: []api.SpecVersionSummary{
			versionOf("1.4.0", "minor", api.SpecChangeCounts{Additive: 2, Risky: 1}, new(int32(3))),
			older,
		},
	}

	stdout, _, err := runRemoteSpec(t, server.URL, "versions", "pets-api")

	if err != nil {
		t.Fatal(err)
	}
	for prefix, want := range map[string]string{
		"Total:":  "Total: 7",
		"VERSION": "VERSION BUMP CHANGES FINDINGS BY WHEN",
		"1.4.0":   "1.4.0 (Live) minor 1 risky, 2 additive 3 API key key-9 2026-10-03 12:00",
		"1.3.0":   "1.3.0 patch 4 edit not recorded member user-1 2026-10-01 09:00",
	} {
		if got := line(stdout, prefix); got != want {
			t.Errorf("%q line = %q, want %q\n%s", prefix, got, want, stdout)
		}
	}
	if query := fake.queries["GET /specs/pets-api/versions"]; query != "limit=20&offset=0" {
		t.Errorf("query = %q", query)
	}
}

func TestSpecVersionsPagesWithLimitAndOffsetAndMarksLiveOnlyOnTheFirstPage(t *testing.T) {
	fake, server := newSpecServer(t)
	fake.responses["GET /specs/pets-api/versions"] = api.SpecVersionListResponse{
		Count: 1, Total: 7, Items: []api.SpecVersionSummary{versionOf("1.1.0", "minor", api.SpecChangeCounts{}, nil)},
	}

	stdout, _, err := runRemoteSpec(t, server.URL, "versions", "pets-api", "--limit", "100", "--offset", "6")

	if err != nil {
		t.Fatal(err)
	}
	if query := fake.queries["GET /specs/pets-api/versions"]; query != "limit=100&offset=6" {
		t.Errorf("query = %q", query)
	}
	if got := line(stdout, "1.1.0"); !strings.HasPrefix(got, "1.1.0 minor none") {
		t.Errorf("row = %q", got)
	}
}

func TestSpecVersionsJSONIsTheList(t *testing.T) {
	fake, server := newSpecServer(t)
	fake.responses["GET /specs/pets-api/versions"] = api.SpecVersionListResponse{
		Count: 1, Total: 1, Items: []api.SpecVersionSummary{liveSummary("1.4.0")},
	}

	stdout, _, err := runRemoteSpecAs(t, server.URL, output.FormatJSON, "versions", "pets-api")

	var got api.SpecVersionListResponse
	if err != nil || json.Unmarshal([]byte(stdout), &got) != nil || got.Total != 1 || got.Items[0].Version != "1.4.0" {
		t.Errorf("stdout %q, err %v", stdout, err)
	}
}

func TestSpecVersionsOfAnUnknownNameSaysSo(t *testing.T) {
	_, server := newSpecServer(t)

	_, _, err := runRemoteSpec(t, server.URL, "versions", "nope")

	if err == nil || !strings.Contains(err.Error(), `no spec with slug "nope"`) {
		t.Errorf("err = %v", err)
	}
}

func TestSpecCommandsNeedCredentials(t *testing.T) {
	_, server := newSpecServer(t)
	for _, leaf := range specLeaves {
		state := makeState(t, "test-api-key", "", server.URL)
		state.APIKey, state.Token = "", ""
		cmd := newSpecCmd(state)
		cmd.SetArgs(invocation(leaf.command, "pets-api", leaf.rest...))
		err := cmd.Execute()
		if err == nil || !strings.Contains(err.Error(), "authentication required") {
			t.Errorf("%v: err = %v", leaf.command, err)
		}
	}
}
