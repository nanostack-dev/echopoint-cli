package commands

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
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

func exitCode(err error) int {
	if coded, ok := errors.AsType[*exitCodeError](err); ok {
		return coded.ExitCode()
	}
	if err != nil {
		return -1
	}
	return 0
}

// specLeaves are the spec commands that act on a spec: the command words,
// the arguments that follow the spec slug, and whether the command takes -f.
var specLeaves = []struct {
	command []string
	rest    []string
	takesF  bool
}{
	{[]string{"view"}, nil, false},
	{[]string{"versions"}, nil, false},
	{[]string{"create"}, []string{"-f", "x.yaml"}, true},
	{[]string{"push"}, []string{"-f", "x.yaml"}, true},
	{[]string{"pull"}, nil, true},
	{[]string{"check"}, []string{"-f", "x.yaml"}, true},
	{[]string{"lint"}, nil, true},
	{[]string{"diff"}, nil, true},
	{[]string{"route", "add"}, []string{"POST", "/pets", "--live"}, false},
	{[]string{"route", "update"}, []string{"GET", "/pets", "--live", "--summary", "S"}, false},
	{[]string{"route", "remove"}, []string{"GET", "/pets", "--live"}, false},
	{[]string{"method", "update"}, []string{"GET", "/pets", "--to", "PATCH", "--live"}, false},
	{[]string{"schema", "add"}, []string{"Pet", "--live"}, false},
	{[]string{"schema", "update"}, []string{"Pet", "--live", "--description", "D"}, false},
	{[]string{"schema", "remove"}, []string{"Pet", "--live"}, false},
	{[]string{"property", "add"}, []string{"Pet", "name", "--live"}, false},
	{[]string{"property", "update"}, []string{"Pet", "name", "--live", "--description", "D"}, false},
	{[]string{"property", "remove"}, []string{"Pet", "name", "--live"}, false},
	{[]string{"param", "add"}, []string{"GET", "/pets", "q", "--in", "query", "--live"}, false},
	{
		[]string{"param", "update"},
		[]string{"GET", "/pets", "q", "--in", "query", "--live", "--description", "D"},
		false,
	},
	{[]string{"param", "remove"}, []string{"GET", "/pets", "q", "--in", "query", "--live"}, false},
	{[]string{"response", "add"}, []string{"GET", "/pets", "404", "--live"}, false},
	{[]string{"response", "update"}, []string{"GET", "/pets", "404", "--live", "--description", "D"}, false},
	{[]string{"response", "remove"}, []string{"GET", "/pets", "404", "--live"}, false},
}

// invocation is the command line of a leaf with the spec slug first.
func invocation(command []string, name string, rest ...string) []string {
	return append(append(slices.Clone(command), name), rest...)
}

func TestSpecPathLookingNamesAreRefusedBeforeAnyRequest(t *testing.T) {
	fake, server := newSpecServer(t)
	for _, leaf := range specLeaves {
		for _, name := range []string{"openapi.yaml", "specs/pets.yml", "./pets-api", "pets.json", "PETS.YAML"} {
			args := invocation(leaf.command, name, leaf.rest...)
			_, _, err := runRemoteSpec(t, server.URL, args...)
			if err == nil ||
				!strings.Contains(
					err.Error(),
					`"`+name+`" is not a spec slug. Spec slugs are listed by echopoint spec list`,
				) {
				t.Errorf("%v: err = %v", args, err)
			}
		}
	}
	if len(fake.requests) != 0 {
		t.Errorf("requests were made: %v", fake.requests)
	}
}

func TestSpecPathLookingNameSaysHowToPassAFileOnlyWhereFIsAccepted(t *testing.T) {
	_, server := newSpecServer(t)
	for _, leaf := range specLeaves {
		_, _, err := runRemoteSpec(t, server.URL, invocation(leaf.command, "openapi.yaml", leaf.rest...)...)
		if err == nil {
			t.Fatalf("%v: no error", leaf.command)
		}
		want := `"openapi.yaml" is not a spec slug. Spec slugs are listed by echopoint spec list.`
		if leaf.takesF {
			want = `"openapi.yaml" is not a spec slug. Spec slugs are listed by echopoint spec list; pass a file with -f.`
		}
		if err.Error() != want {
			t.Errorf("%v: err = %q, want %q", leaf.command, err, want)
		}
	}
}

func TestSpecPushOfAFileNamedLikeTheOldGrammarFailsWithoutCredentials(t *testing.T) {
	cmd := newSpecCmd(&AppState{})
	cmd.SetArgs([]string{"push", "openapi.yaml"})

	err := cmd.Execute()

	want := `"openapi.yaml" is not a spec slug. Spec slugs are listed by echopoint spec list; pass a file with -f.`
	if err == nil || err.Error() != want {
		t.Errorf("err = %v", err)
	}
}

func TestSpecSlugThatIsAnExistingFileIsRefusedUnlessItIsAValidSlug(t *testing.T) {
	fake, server := newSpecServer(t)
	fake.responses["GET /specs/pets-api"] = specOf("pets-api", "Pets", "1.4.0")
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "pets-api"), "x")
	writeFile(t, filepath.Join(dir, "my_notes"), "x")
	t.Chdir(dir)

	_, _, err := runRemoteSpec(t, server.URL, "view", "my_notes")
	if err == nil || !strings.Contains(err.Error(), `"my_notes" is not a spec slug`) {
		t.Errorf("a file that is not a valid name: err = %v", err)
	}
	if len(fake.requests) != 0 {
		t.Errorf("requests were made: %v", fake.requests)
	}
	if _, _, err = runRemoteSpec(t, server.URL, "view", "pets-api"); err != nil {
		t.Errorf("a valid name that is also a file: err = %v", err)
	}
}

func TestSpecDirectoryNamedLikeAnInvalidNameIsNotAFile(t *testing.T) {
	fake, server := newSpecServer(t)
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "Pets_Api"), 0o750); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	_, _, err := runRemoteSpec(t, server.URL, "view", "Pets_Api")

	if err == nil || strings.Contains(err.Error(), "is not a spec slug") {
		t.Errorf("err = %v", err)
	}
	if len(fake.requests) != 1 {
		t.Errorf("requests = %v", fake.requests)
	}
}

func TestSpecCommandsRefuseFWhereTheyDoNotReadOrWriteAFile(t *testing.T) {
	fake, server := newSpecServer(t)
	for _, leaf := range specLeaves {
		if leaf.takesF {
			continue
		}
		args := invocation(leaf.command, "pets-api", append(slices.Clone(leaf.rest), "-f", "openapi.yaml")...)
		_, _, err := runRemoteSpec(t, server.URL, args...)
		if err == nil || !strings.Contains(err.Error(), "unknown shorthand flag: 'f' in -f") {
			t.Errorf("%v: err = %v", args, err)
		}
		args = invocation(leaf.command, "pets-api", append(slices.Clone(leaf.rest), "--file", "openapi.yaml")...)
		_, _, err = runRemoteSpec(t, server.URL, args...)
		if err == nil || !strings.Contains(err.Error(), "unknown flag: --file") {
			t.Errorf("%v: err = %v", args, err)
		}
	}
	if _, _, err := runRemoteSpec(t, server.URL, "list", "-f", "openapi.yaml"); err == nil {
		t.Error("list accepted -f")
	}
	if len(fake.requests) != 0 {
		t.Errorf("requests were made: %v", fake.requests)
	}
}

func TestSpecCommandsThatTakeAFileAcceptBothSpellings(t *testing.T) {
	for _, leaf := range specLeaves {
		if !leaf.takesF {
			continue
		}
		cmd, _, err := newSpecCmd(&AppState{}).Find(leaf.command)
		if err != nil {
			t.Fatal(err)
		}
		flag := cmd.Flags().Lookup("file")
		if flag == nil || flag.Shorthand != "f" {
			t.Errorf("%v: --file flag = %+v", leaf.command, flag)
		}
	}
}

func TestSpecCommandsNeedTheSpecSlug(t *testing.T) {
	_, server := newSpecServer(t)
	for _, leaf := range specLeaves {
		_, _, err := runRemoteSpec(t, server.URL, leaf.command...)
		if err == nil || !strings.Contains(err.Error(), "name the spec") {
			t.Errorf("%v: err = %v", leaf.command, err)
		}
	}
}

func TestSpecCommandsCountTheirArguments(t *testing.T) {
	_, server := newSpecServer(t)

	_, _, err := runRemoteSpec(t, server.URL, "view", "pets-api", "more")
	if err == nil || !strings.Contains(err.Error(), "accepts 1 arg(s), received 2; usage: spec view <slug>") {
		t.Errorf("err = %v", err)
	}
	_, _, err = runRemoteSpec(t, server.URL, "route", "add", "pets-api", "POST", "--live")
	if err == nil || !strings.Contains(err.Error(), "accepts 3 arg(s), received 2") ||
		!strings.Contains(err.Error(), "route add <slug> <METHOD> <path>") {
		t.Errorf("err = %v", err)
	}
}

func TestEverySpecCommandShowsTheNameAndAnExample(t *testing.T) {
	spec := newSpecCmd(&AppState{})
	var leaves []*cobra.Command
	var collect func(cmd *cobra.Command)
	collect = func(cmd *cobra.Command) {
		for _, child := range cmd.Commands() {
			if child.HasSubCommands() {
				collect(child)
				continue
			}
			leaves = append(leaves, child)
		}
	}
	collect(spec)
	if len(leaves) != len(specLeaves)+1 {
		t.Fatalf("%d spec commands, want %d (list takes no name)", len(leaves), len(specLeaves)+1)
	}
	for _, leaf := range leaves {
		name := leaf.CommandPath()
		if !strings.Contains(leaf.Example, "echopoint "+name) {
			t.Errorf("%s: example = %q", name, leaf.Example)
		}
		if leaf.Name() != listVerb && !strings.Contains(leaf.Use, "<slug>") {
			t.Errorf("%s: use = %q, want the <slug> argument", name, leaf.Use)
		}
		if flag := leaf.Flags().Lookup("spec"); flag != nil {
			t.Errorf("%s still has --spec", name)
		}
	}
}

func TestSpecGroupSaysEveryCommandWorksOnASpecInEchoPoint(t *testing.T) {
	spec := newSpecCmd(&AppState{})

	for _, want := range []string{"in EchoPoint", "unique slug", "-f/--file"} {
		if !strings.Contains(spec.Short+spec.Long, want) {
			t.Errorf("group help misses %q:\n%s\n%s", want, spec.Short, spec.Long)
		}
	}
}

func TestSpecPushUsageLineShowsTheSlugAndTheFile(t *testing.T) {
	spec := newSpecCmd(&AppState{})
	var stdout bytes.Buffer
	spec.SetOut(&stdout)
	spec.SetArgs([]string{"push", "--help"})

	if err := spec.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"push <slug> -f <file> [flags]", "Examples:", "echopoint spec push pets-api -f openapi.yaml"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("help misses %q:\n%s", want, stdout.String())
		}
	}
}
