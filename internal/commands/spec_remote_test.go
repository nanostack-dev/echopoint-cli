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
// response registered for "METHOD path".
type specServer struct {
	responses map[string]any
	statuses  map[string]int
	bodies    map[string][]byte
	queries   map[string]string
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

func runRemoteSpec(t *testing.T, serverURL string, args ...string) (string, string, error) {
	t.Helper()
	return runRemoteSpecAs(t, serverURL, output.FormatTable, args...)
}

func runRemoteSpecAs(t *testing.T, serverURL string, format output.Format, args ...string) (string, string, error) {
	t.Helper()
	state := makeState(t, "test-api-key", "", serverURL)
	state.OutputFormat = format
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
		Version:   version,
		Bump:      api.SpecBump("minor"),
		CreatedAt: time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC),
	}
}

func TestSpecPushNewCreatesTheSpec(t *testing.T) {
	fake, server := newSpecServer(t)
	fake.responses["POST /specs"] = api.Spec{Slug: "pets-api", Live: liveSummary("0.3.0")}
	fake.statuses["POST /specs"] = http.StatusCreated

	stdout, _, err := runRemoteSpec(t, server.URL, "push", "--new", "--spec", "pets-api", writeSpec(t, specFixture))

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

func TestSpecPushPublishesTheNextVersion(t *testing.T) {
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

	stdout, _, err := runRemoteSpec(t, server.URL, "push", "--spec", "pets-api", writeSpec(t, specFixture))

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
	fake.responses["POST /specs/pets-api/versions"] = map[string]any{
		"errors": []map[string]any{{
			"code":    "SPEC_NOTHING_TO_PUBLISH",
			"message": "Nothing to publish: the document is identical to the Live version.",
		}},
	}
	fake.statuses["POST /specs/pets-api/versions"] = http.StatusConflict

	_, _, err := runRemoteSpec(t, server.URL, "push", "--spec", "pets-api", writeSpec(t, specFixture))

	if err == nil || !strings.Contains(err.Error(), "Nothing to publish") {
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

	_, _, err := runRemoteSpec(t, server.URL, "push", "--bundle", "--spec", "pets-api", root)

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

	if _, _, err := runRemoteSpec(t, server.URL, "push", "--spec", "pets-api", writeSpec(t, specFixture)); err != nil {
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

func TestSpecPullWritesTheLiveDocumentByteForByte(t *testing.T) {
	fake, server := newSpecServer(t)
	fake.responses["GET /specs/pets-api/document"] = pulledDocument(canonicalSpecFixture)
	path := filepath.Join(t.TempDir(), "openapi.yaml")

	if _, _, err := runRemoteSpec(t, server.URL, "pull", "--spec", "pets-api", path); err != nil {
		t.Fatal(err)
	}
	if written, _ := os.ReadFile(path); string(written) != canonicalSpecFixture {
		t.Errorf("written:\n%s", written)
	}
}

func TestSpecPullVersionAsksForThatVersion(t *testing.T) {
	fake, server := newSpecServer(t)
	fake.responses["GET /specs/pets-api/document"] = pulledDocument(canonicalSpecFixture)

	_, _, err := runRemoteSpec(t, server.URL, "pull", "--spec", "pets-api", "--version", "1.2.0",
		filepath.Join(t.TempDir(), "openapi.yaml"))

	if err != nil {
		t.Fatal(err)
	}
	if query := fake.queries["GET /specs/pets-api/document"]; query != "version=1.2.0" {
		t.Errorf("query = %q", query)
	}
}

func TestSpecCheckPassesWhenTheFileMatchesLive(t *testing.T) {
	fake, server := newSpecServer(t)
	fake.responses["GET /specs/pets-api/document"] = pulledDocument(canonicalSpecFixture)

	stdout, _, err := runRemoteSpec(t, server.URL, "check", "--spec", "pets-api", writeSpec(t, canonicalSpecFixture))

	if err != nil || !strings.Contains(stdout, "matches pets-api 1.4.0") {
		t.Errorf("stdout %q, err %v", stdout, err)
	}
}

func TestSpecCheckFailsOnDrift(t *testing.T) {
	fake, server := newSpecServer(t)
	fake.responses["GET /specs/pets-api/document"] = pulledDocument(canonicalSpecFixture)

	_, stderr, err := runRemoteSpec(t, server.URL, "check", "--spec", "pets-api", writeSpec(t, specFixture))

	if exitCode(err) != 1 || !strings.Contains(stderr, "echopoint spec pull --spec pets-api") {
		t.Errorf("exit %d, stderr %q", exitCode(err), stderr)
	}
}

func TestSpecCheckFailsWhenTheFileIsMissing(t *testing.T) {
	fake, server := newSpecServer(t)
	fake.responses["GET /specs/pets-api/document"] = pulledDocument(canonicalSpecFixture)

	_, _, err := runRemoteSpec(t, server.URL, "check", "--spec", "pets-api",
		filepath.Join(t.TempDir(), "openapi.yaml"))

	if exitCode(err) != 1 {
		t.Errorf("exit %d, want 1", exitCode(err))
	}
}

func TestSpecListReturnsSlugsAndLiveVersions(t *testing.T) {
	fake, server := newSpecServer(t)
	fake.responses["GET /specs"] = api.SpecListResponse{
		Count: 1, Total: 1,
		Items: []api.Spec{{Slug: "pets-api", Title: "Pets", Live: liveSummary("1.4.0")}},
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
}

func TestSpecRemoteCommandsRequireTheSpecFlag(t *testing.T) {
	_, server := newSpecServer(t)

	_, _, err := runRemoteSpec(t, server.URL, "pull", filepath.Join(t.TempDir(), "openapi.yaml"))

	if err == nil || !strings.Contains(err.Error(), `"spec" not set`) {
		t.Errorf("err = %v", err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
