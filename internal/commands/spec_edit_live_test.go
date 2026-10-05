package commands

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/nanostack-dev/echopoint-kit/apispec"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/output"
)

const commandsRoute = "POST /specs/pets-api/commands"

type commandsBody struct {
	CommandID string            `json:"command_id"`
	Commands  []apispec.Command `json:"commands"`
}

func publishedVersion(version string, findings ...api.SpecFinding) api.SpecVersion {
	return api.SpecVersion{Version: version, Bump: api.SpecBump("minor"), Findings: findings}
}

// liveServer is a fake EchoPoint whose Live version is the document, and that
// accepts commands with a 201.
func liveServer(t *testing.T, document string) (*specServer, string) {
	t.Helper()
	fake, server := newSpecServer(t)
	fake.responses["GET /specs/pets-api/document"] = pulledDocument(document)
	fake.responses[commandsRoute] = publishedVersion("1.5.0")
	fake.statuses[commandsRoute] = http.StatusCreated
	return fake, server.URL
}

func sentCommands(t *testing.T, fake *specServer) commandsBody {
	t.Helper()
	body, ok := fake.bodies[commandsRoute]
	if !ok {
		t.Fatal("no request reached POST /specs/pets-api/commands")
	}
	var sent commandsBody
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return sent
}

func TestSpecEditLivePublishesTheCommandsInOneRequest(t *testing.T) {
	fake, url := liveServer(t, canonicalSpecFixture)

	stdout, _, err := runRemoteSpec(t, url, "route", "add", "POST", "/pets", "--spec", "pets-api", "--live",
		"--status", "201", "--operation-id", "createPet")

	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "✓ Published pets-api 1.5.0 (minor): Added POST /pets") {
		t.Errorf("stdout = %q", stdout)
	}
	sent := sentCommands(t, fake)
	if _, err = uuid.Parse(sent.CommandID); err != nil {
		t.Errorf("command_id %q is not a UUID", sent.CommandID)
	}
	want := []apispec.Command{{
		Kind: apispec.CommandAddOperation, Method: "post", Path: "/pets", OperationID: "createPet", Status: "201",
	}}
	if !reflect.DeepEqual(sent.Commands, want) {
		t.Errorf("commands = %+v, want %+v", sent.Commands, want)
	}
}

func TestSpecEditLiveSendsAllTheCommandsOfARouteUpdate(t *testing.T) {
	fake, url := liveServer(t, canonicalSpecFixture)

	_, _, err := runRemoteSpec(t, url, "route", "update", "GET", "/pets", "--spec", "pets-api", "--live",
		"--summary", "List", "--path", "/animals")

	if err != nil {
		t.Fatal(err)
	}
	pointer := apispec.OperationPointer("get", "/pets")
	want := []apispec.Command{
		{Kind: apispec.CommandSetText, Pointer: pointer, Field: "summary", Value: "List"},
		{Kind: apispec.CommandRenamePath, Pointer: pointer, Path: "/animals"},
	}
	if got := sentCommands(t, fake).Commands; !reflect.DeepEqual(got, want) {
		t.Errorf("commands = %+v, want %+v", got, want)
	}
}

func TestSpecEditLiveFindsAParameterInTheLiveDocument(t *testing.T) {
	fake, url := liveServer(t, paramSpec)

	_, _, err := runRemoteSpec(t, url, "param", "update", "GET", "/pets/{id}", "limit", "--in", "header",
		"--spec", "pets-api", "--live", "--description", "Cap")

	if err != nil {
		t.Fatal(err)
	}
	want := []apispec.Command{{
		Kind: apispec.CommandSetParameterField, Pointer: "/paths/~1pets~1{id}/get/parameters/1",
		Field: "description", Value: "Cap",
	}}
	if got := sentCommands(t, fake).Commands; !reflect.DeepEqual(got, want) {
		t.Errorf("commands = %+v, want %+v", got, want)
	}
}

func TestSpecEditLiveParamOfAnUnknownNameIsRefusedBeforeSending(t *testing.T) {
	fake, url := liveServer(t, paramSpec)

	_, stderr, err := runRemoteSpec(t, url, "param", "remove", "GET", "/pets/{id}", "nope", "--in", "query",
		"--spec", "pets-api", "--live")

	if exitCode(err) != 1 || !strings.Contains(stderr, "✗ ") || !strings.Contains(stderr, "nope") {
		t.Errorf("exit %d, stderr %q", exitCode(err), stderr)
	}
	if _, sent := fake.bodies[commandsRoute]; sent {
		t.Error("a request was sent")
	}
}

const parityDocument = paramSpec + `components:
  schemas:
    Pet:
      type: object
      properties:
        name: {type: string}
    Error:
      type: object
`

func TestSpecEditLiveCommandsEqualTheLocalOnes(t *testing.T) {
	invocations := [][]string{
		{"route", "add", "post", "/pets", "--status", "201", "--tag", "pets"},
		{"route", "update", "GET", "/pets/{id}", "--summary", "S", "--path", "/x/{id}", "--method", "put"},
		{"route", "remove", "GET", "/pets/{id}"},
		{"method", "update", "GET", "/pets/{id}", "--to", "patch"},
		{"schema", "add", "Owner", "--description", "An owner."},
		{"property", "add", "Pet", "city", "--type", "string", "--required"},
		{"property", "update", "Pet", "name", "--name", "title", "--enum", "a,b", "--required=false"},
		{"param", "add", "GET", "/pets/{id}", "q", "--in", "query", "--required"},
		{"param", "update", "GET", "/pets/{id}", "limit", "--in", "header", "--new-in", "cookie", "--type", "string"},
		{"param", "remove", "GET", "/pets/{id}", "limit", "--in", "query"},
		{"response", "add", "GET", "/pets/{id}", "4xx", "--schema", "Error"},
		{"response", "update", "GET", "/pets/{id}", "200", "--description", "Ok"},
		{"response", "remove", "GET", "/pets/{id}", "200"},
	}
	for _, invocation := range invocations {
		fake, url := liveServer(t, parityDocument)
		_, _, err := runRemoteSpec(t, url, append(slices.Clone(invocation), "--spec", "pets-api", "--live")...)
		if err != nil {
			t.Fatalf("%v remote: %v", invocation, err)
		}
		local := localCommands(t, invocation, writeSpec(t, parityDocument))
		if remote := sentCommands(t, fake).Commands; !reflect.DeepEqual(remote, local) {
			t.Errorf("%v\nremote %+v\nlocal  %+v", invocation, remote, local)
		}
	}
}

// localCommands are the commands the same invocation builds for a file.
func localCommands(t *testing.T, invocation []string, path string) []apispec.Command {
	t.Helper()
	stdout, stderr, err := runSpec(t, output.FormatJSON, append(slices.Clone(invocation), "--file", path)...)
	if err != nil {
		t.Fatalf("%v local: %v %s", invocation, err, stderr)
	}
	var result specEditResult
	if err = json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("decode %q: %v", stdout, err)
	}
	return result.Commands
}

func TestSpecEditLiveReplayIsReportedAndAppliesNothing(t *testing.T) {
	fake, url := liveServer(t, canonicalSpecFixture)
	fake.statuses[commandsRoute] = http.StatusOK

	stdout, _, err := runRemoteSpec(t, url, "schema", "add", "Pet", "--spec", "pets-api", "--live")

	if err != nil {
		t.Fatal(err)
	}
	if stdout != "✓ Already applied: pets-api 1.5.0\n" {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestSpecEditLiveListsOnlyTheIntroducedFindings(t *testing.T) {
	fake, url := liveServer(t, canonicalSpecFixture)
	fake.responses[commandsRoute] = publishedVersion("1.5.0",
		api.SpecFinding{
			Rule: api.SpecLintRule("property-casing"), Pointer: "/components/schemas/Pet/properties/pet_name",
			Message: "pet_name is not camelCase", Introduced: true,
		},
		api.SpecFinding{
			Rule: api.SpecLintRule("missing-description"), Pointer: "/paths/~1old", Message: "old", Introduced: false,
		})

	stdout, _, err := runRemoteSpec(t, url, "schema", "add", "Pet", "--spec", "pets-api", "--live")

	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"New findings (1)", "property-casing /components/schemas/Pet/properties/pet_name: pet_name is not camelCase",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout misses %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "/paths/~1old") {
		t.Errorf("stdout lists a finding that was not introduced:\n%s", stdout)
	}
}

func TestSpecEditLiveStructuredOutputIsTheVersionAndReplayed(t *testing.T) {
	fake, url := liveServer(t, canonicalSpecFixture)
	fake.statuses[commandsRoute] = http.StatusOK

	stdout, _, err := runRemoteSpecAs(t, url, output.FormatJSON, "schema", "add", "Pet", "--spec", "pets-api", "--live")

	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err = json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("decode %q: %v", stdout, err)
	}
	if result["version"] != "1.5.0" || result["replayed"] != true || result["bump"] != "minor" {
		t.Errorf("result = %v", result)
	}
	yamlOut, _, err := runRemoteSpecAs(
		t,
		url,
		output.FormatYAML,
		"schema",
		"add",
		"Pet",
		"--spec",
		"pets-api",
		"--live",
	)
	if err != nil || !strings.Contains(yamlOut, "version: 1.5.0") || !strings.Contains(yamlOut, "replayed: true") {
		t.Errorf("yaml = %q, err %v", yamlOut, err)
	}
}

func TestSpecEditLiveRefusalPrintsTheAPIMessageAndExitsOne(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusConflict} {
		fake, url := liveServer(t, canonicalSpecFixture)
		fake.statuses[commandsRoute] = status
		fake.responses[commandsRoute] = map[string]any{"errors": []map[string]any{{
			"code": "SPEC_COMMAND_REFUSED", "message": "Command 0 (add_schema) was refused: schema Pet already exists",
		}}}

		stdout, stderr, err := runRemoteSpec(t, url, "schema", "add", "Pet", "--spec", "pets-api", "--live")

		if exitCode(err) != 1 {
			t.Fatalf("exit %d, want 1 (err %v)", exitCode(err), err)
		}
		if stdout != "" ||
			!strings.Contains(stderr, "✗ Command 0 (add_schema) was refused: schema Pet already exists") ||
			strings.Contains(stderr, "Usage:") {
			t.Errorf("stdout %q, stderr %q", stdout, stderr)
		}
	}
}

func TestSpecEditLiveDryRunSendsNothing(t *testing.T) {
	fake, url := liveServer(t, canonicalSpecFixture)

	stdout, _, err := runRemoteSpec(t, url, "schema", "add", "Pet", "--spec", "pets-api", "--live", "--dry-run")

	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "    Pet:\n      type: object\n") {
		t.Errorf("stdout:\n%s", stdout)
	}
	if _, sent := fake.bodies[commandsRoute]; sent {
		t.Error("--dry-run sent the commands")
	}
}

func TestSpecEditLiveNeedsLive(t *testing.T) {
	fake, url := liveServer(t, canonicalSpecFixture)

	_, _, err := runRemoteSpec(t, url, "schema", "add", "Pet", "--spec", "pets-api")

	if err == nil || !strings.Contains(err.Error(),
		"drafts are not available yet: pass --live to write the Live version") {
		t.Errorf("err = %v", err)
	}
	if _, sent := fake.bodies[commandsRoute]; sent {
		t.Error("a request was sent")
	}
	_, _, err = runRemoteSpec(t, url, "schema", "add", "Pet", "--live")
	if err == nil || !strings.Contains(err.Error(), "--live needs --spec") {
		t.Errorf("--live alone: err = %v", err)
	}
}

func TestSpecEditFileAndSpecAreExclusive(t *testing.T) {
	_, url := liveServer(t, canonicalSpecFixture)

	_, _, err := runRemoteSpec(t, url, "schema", "add", "Pet", "--file", writeSpec(t, editFixture),
		"--spec", "pets-api", "--live")

	if err == nil || !strings.Contains(err.Error(), "--file and --spec cannot be used together") {
		t.Errorf("err = %v", err)
	}
}

func TestSpecEditLiveCommandIDCanBePinned(t *testing.T) {
	fake, url := liveServer(t, canonicalSpecFixture)
	id := "5b2a1c0e-8f0d-4c1b-9d57-3a7f2e6c9b10"

	if _, _, err := runRemoteSpec(t, url, "schema", "add", "Pet", "--spec", "pets-api", "--live",
		"--command-id", id); err != nil {
		t.Fatal(err)
	}

	if got := sentCommands(t, fake).CommandID; got != id {
		t.Errorf("command_id = %q, want %q", got, id)
	}
	_, _, err := runRemoteSpec(t, url, "schema", "add", "Pet", "--spec", "pets-api", "--live", "--command-id", "nope")
	if err == nil || !strings.Contains(err.Error(), "not a UUID") {
		t.Errorf("err = %v", err)
	}
	_, _, err = runRemoteSpec(t, url, "schema", "add", "Pet", "--file", writeSpec(t, editFixture), "--command-id", id)
	if err == nil || !strings.Contains(err.Error(), "--command-id") {
		t.Errorf("--command-id with --file: err = %v", err)
	}
}

func TestSpecEditLiveGeneratesANewCommandIDForEachRun(t *testing.T) {
	fake, url := liveServer(t, canonicalSpecFixture)
	var ids []string
	for range 2 {
		if _, _, err := runRemoteSpec(t, url, "schema", "add", "Pet", "--spec", "pets-api", "--live"); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, sentCommands(t, fake).CommandID)
	}
	if ids[0] == ids[1] {
		t.Errorf("both runs sent command_id %s", ids[0])
	}
}

func TestSpecEditLiveRetriesANetworkErrorWithTheSameCommandID(t *testing.T) {
	var mutex sync.Mutex
	var ids []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(pulledDocument(canonicalSpecFixture))
			return
		}
		var sent commandsBody
		_ = json.NewDecoder(r.Body).Decode(&sent)
		mutex.Lock()
		ids = append(ids, sent.CommandID)
		first := len(ids) == 1
		mutex.Unlock()
		if first {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				conn.Close()
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(publishedVersion("1.5.0"))
	}))
	t.Cleanup(server.Close)

	stdout, _, err := runRemoteSpec(t, server.URL, "schema", "add", "Pet", "--spec", "pets-api", "--live")

	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "✓ Published pets-api 1.5.0") {
		t.Errorf("stdout = %q", stdout)
	}
	if len(ids) < 2 || ids[0] == "" || ids[0] != ids[1] {
		t.Errorf("command IDs sent = %v, want the same one twice", ids)
	}
}

func TestSpecEditLiveNeedsCredentials(t *testing.T) {
	_, url := liveServer(t, canonicalSpecFixture)
	state := makeState(t, "test-api-key", "", url)
	state.APIKey, state.Token = "", ""
	cmd := newSpecCmd(state)
	cmd.SetArgs([]string{"schema", "add", "Pet", "--spec", "pets-api", "--live"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "authentication required") {
		t.Errorf("err = %v", err)
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

	stdout, _, err := runRemoteSpec(t, server.URL, "push", "--spec", "pets-api", writeSpec(t, specFixture))

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
