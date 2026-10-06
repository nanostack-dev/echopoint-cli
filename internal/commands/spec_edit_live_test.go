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
	CommandID   string            `json:"command_id"`
	Commands    []apispec.Command `json:"commands"`
	BaseVersion string            `json:"base_version"`
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

	stdout, _, err := runRemoteSpec(t, url, "route", "add", "pets-api", "POST", "/pets", "--live",
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

	_, _, err := runRemoteSpec(t, url, "route", "update", "pets-api", "GET", "/pets", "--live",
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

	_, _, err := runRemoteSpec(t, url, "param", "update", "pets-api", "GET", "/pets/{id}", "limit", "--in", "header",
		"--live", "--description", "Cap")

	if err != nil {
		t.Fatal(err)
	}
	want := []apispec.Command{{
		Kind: apispec.CommandSetParameterField, Pointer: "/paths/~1pets~1{id}/get/parameters/1",
		Field: "description", Value: "Cap",
	}}
	sent := sentCommands(t, fake)
	if !reflect.DeepEqual(sent.Commands, want) {
		t.Errorf("commands = %+v, want %+v", sent.Commands, want)
	}
	if sent.BaseVersion != "1.4.0" {
		t.Errorf("base_version = %q, want the pulled Live version 1.4.0", sent.BaseVersion)
	}
}

func TestSpecEditLiveParamOfAnUnknownNameIsRefusedBeforeSending(t *testing.T) {
	fake, url := liveServer(t, paramSpec)

	_, stderr, err := runRemoteSpec(t, url, "param", "remove", "pets-api", "GET", "/pets/{id}", "nope", "--in", "query",
		"--live")

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

func TestSpecEditEveryCommandTakesTheNameFirstAndSendsItsCommands(t *testing.T) {
	invocations := []struct {
		args []string
		want []apispec.CommandKind
	}{
		{[]string{"route", "add", "pets-api", "post", "/pets", "--status", "201", "--tag", "pets"},
			[]apispec.CommandKind{apispec.CommandAddOperation}},
		{
			[]string{
				"route",
				"update",
				"pets-api",
				"GET",
				"/pets/{id}",
				"--summary",
				"S",
				"--path",
				"/x/{id}",
				"--method",
				"put",
			},
			[]apispec.CommandKind{apispec.CommandSetText, apispec.CommandRenamePath, apispec.CommandSetMethod},
		},
		{[]string{"route", "remove", "pets-api", "GET", "/pets/{id}"},
			[]apispec.CommandKind{apispec.CommandRemoveOperation}},
		{[]string{"method", "update", "pets-api", "GET", "/pets/{id}", "--to", "patch"},
			[]apispec.CommandKind{apispec.CommandSetMethod}},
		{[]string{"schema", "add", "pets-api", "Owner", "--description", "An owner."},
			[]apispec.CommandKind{apispec.CommandAddSchema}},
		{[]string{"schema", "update", "pets-api", "Pet", "--description", "D"},
			[]apispec.CommandKind{apispec.CommandSetText}},
		{[]string{"schema", "remove", "pets-api", "Pet"}, []apispec.CommandKind{apispec.CommandRemoveSchema}},
		{[]string{"property", "add", "pets-api", "Pet", "city", "--type", "string", "--required"},
			[]apispec.CommandKind{apispec.CommandAddProperty}},
		{
			[]string{
				"property",
				"update",
				"pets-api",
				"Pet",
				"name",
				"--name",
				"title",
				"--enum",
				"a,b",
				"--required=false",
			},
			[]apispec.CommandKind{apispec.CommandRenameProperty, apispec.CommandSetEnum, apispec.CommandSetRequired},
		},
		{[]string{"property", "remove", "pets-api", "Pet", "name"},
			[]apispec.CommandKind{apispec.CommandRemoveProperty}},
		{[]string{"param", "add", "pets-api", "GET", "/pets/{id}", "q", "--in", "query", "--required"},
			[]apispec.CommandKind{apispec.CommandAddParameter}},
		{
			[]string{
				"param",
				"update",
				"pets-api",
				"GET",
				"/pets/{id}",
				"limit",
				"--in",
				"header",
				"--new-in",
				"cookie",
				"--type",
				"string",
			},
			[]apispec.CommandKind{apispec.CommandSetParameterType, apispec.CommandSetParameterField},
		},
		{[]string{"param", "remove", "pets-api", "GET", "/pets/{id}", "limit", "--in", "query"},
			[]apispec.CommandKind{apispec.CommandRemoveParameter}},
		{[]string{"response", "add", "pets-api", "GET", "/pets/{id}", "4xx", "--schema", "Error"},
			[]apispec.CommandKind{apispec.CommandAddResponse}},
		{[]string{"response", "update", "pets-api", "GET", "/pets/{id}", "200", "--description", "Ok"},
			[]apispec.CommandKind{apispec.CommandSetResponse}},
		{[]string{"response", "remove", "pets-api", "GET", "/pets/{id}", "200"},
			[]apispec.CommandKind{apispec.CommandRemoveResponse}},
	}
	for _, invocation := range invocations {
		fake, url := liveServer(t, parityDocument)

		_, _, err := runRemoteSpec(t, url, append(slices.Clone(invocation.args), "--live")...)

		if err != nil {
			t.Fatalf("%v: %v", invocation.args, err)
		}
		var sent []apispec.CommandKind
		for _, command := range sentCommands(t, fake).Commands {
			sent = append(sent, command.Kind)
		}
		for _, want := range invocation.want {
			if !slices.Contains(sent, want) {
				t.Errorf("%v sent %v, want %s among them", invocation.args, sent, want)
			}
		}
	}
}

func TestSpecEditLiveReplayIsReportedAndAppliesNothing(t *testing.T) {
	fake, url := liveServer(t, canonicalSpecFixture)
	fake.statuses[commandsRoute] = http.StatusOK

	stdout, _, err := runRemoteSpec(t, url, "schema", "add", "pets-api", "Pet", "--live")

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

	stdout, _, err := runRemoteSpec(t, url, "schema", "add", "pets-api", "Pet", "--live")

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

	stdout, _, err := runRemoteSpecAs(t, url, output.FormatJSON, "schema", "add", "pets-api", "Pet", "--live")

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
	yamlOut, _, err := runRemoteSpecAs(t, url, output.FormatYAML, "schema", "add", "pets-api", "Pet", "--live")
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

		stdout, stderr, err := runRemoteSpec(t, url, "schema", "add", "pets-api", "Pet", "--live")

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

	stdout, _, err := runRemoteSpec(t, url, "schema", "add", "pets-api", "Pet", "--live", "--dry-run")

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

func TestSpecEditNeedsLive(t *testing.T) {
	fake, url := liveServer(t, canonicalSpecFixture)

	_, _, err := runRemoteSpec(t, url, "schema", "add", "pets-api", "Pet")

	if err == nil || !strings.Contains(err.Error(),
		"drafts are not available yet: pass --live to write the Live version") {
		t.Errorf("err = %v", err)
	}
	if len(fake.requests) != 0 {
		t.Errorf("requests were made: %v", fake.requests)
	}
}

func TestSpecEditOfAnUnknownSpecNamesItAndSuggestsList(t *testing.T) {
	fake, server := newSpecServer(t)
	fake.responses["GET /specs/nope/document"] = apiError("SPEC_NOT_FOUND", "The spec does not exist.")
	fake.statuses["GET /specs/nope/document"] = http.StatusNotFound

	_, _, err := runRemoteSpec(t, server.URL, "schema", "add", "nope", "Pet", "--live")

	want := `api error (404): no spec with slug "nope"; list the specs with: echopoint spec list`
	if err == nil || err.Error() != want {
		t.Errorf("err = %v", err)
	}
	if len(fake.requests) != 1 {
		t.Errorf("requests = %v, want only the pull of Live", fake.requests)
	}
}

func TestSpecUpdateWithNoFlagNamesTheFlags(t *testing.T) {
	for _, args := range [][]string{
		{"route", "update", "pets-api", "GET", "/pets/{id}"},
		{"schema", "update", "pets-api", "Pet"},
		{"property", "update", "pets-api", "Pet", "name"},
		{"param", "update", "pets-api", "GET", "/pets/{id}", "limit", "--in", "query"},
		{"response", "update", "pets-api", "GET", "/pets/{id}", "200"},
	} {
		fake, url := liveServer(t, parityDocument)

		_, _, err := runRemoteSpec(t, url, append(args, "--live")...)

		if err == nil || !strings.Contains(err.Error(), "nothing to change") || !strings.Contains(err.Error(), "--") {
			t.Errorf("%v: err = %v", args, err)
		}
		if _, sent := fake.bodies[commandsRoute]; sent {
			t.Errorf("%v: a request was sent", args)
		}
	}
}

func TestSpecEditJSONAndDryRunDoNotCombine(t *testing.T) {
	_, url := liveServer(t, canonicalSpecFixture)

	_, _, err := runRemoteSpecAs(t, url, output.FormatJSON, "schema", "add", "pets-api", "Pet", "--live", "--dry-run")

	if err == nil || !strings.Contains(err.Error(), "--dry-run") {
		t.Errorf("err = %v", err)
	}
}

func TestSpecEditLiveCommandIDCanBePinned(t *testing.T) {
	fake, url := liveServer(t, canonicalSpecFixture)
	id := "5b2a1c0e-8f0d-4c1b-9d57-3a7f2e6c9b10"

	if _, _, err := runRemoteSpec(t, url, "schema", "add", "pets-api", "Pet", "--live",
		"--command-id", id); err != nil {
		t.Fatal(err)
	}

	if got := sentCommands(t, fake).CommandID; got != id {
		t.Errorf("command_id = %q, want %q", got, id)
	}
	_, _, err := runRemoteSpec(t, url, "schema", "add", "pets-api", "Pet", "--live", "--command-id", "nope")
	if err == nil || !strings.Contains(err.Error(), "not a UUID") {
		t.Errorf("err = %v", err)
	}
}

func TestSpecEditLiveGeneratesANewCommandIDForEachRun(t *testing.T) {
	fake, url := liveServer(t, canonicalSpecFixture)
	var ids []string
	for range 2 {
		if _, _, err := runRemoteSpec(t, url, "schema", "add", "pets-api", "Pet", "--live"); err != nil {
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

	stdout, _, err := runRemoteSpec(t, server.URL, "schema", "add", "pets-api", "Pet", "--live")

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
	cmd.SetArgs([]string{"schema", "add", "pets-api", "Pet", "--live"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "authentication required") {
		t.Errorf("err = %v", err)
	}
}
