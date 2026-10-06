package commands

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/config"
)

func terminal(yes bool) func() bool { return func() bool { return yes } }

func exitCodeOfError(t *testing.T, err error) int {
	t.Helper()
	var coded interface{ ExitCode() int }
	if !errors.As(err, &coded) {
		t.Fatalf("error %v carries no exit code", err)
	}
	return coded.ExitCode()
}

func TestConfirmDestructiveAsksOnATerminalAndOnlyYesOrYAgrees(t *testing.T) {
	for answer, agrees := range map[string]bool{
		"y\n": true, "yes\n": true, "Y\n": true, "YES\n": true, " y \n": true, "y": true,
		"n\n": false, "no\n": false, "\n": false, "": false, "maybe\n": false, "yep\n": false, "ye\n": false,
	} {
		state := &AppState{IsTerminal: terminal(true)}
		cmd := &cobra.Command{}
		var stderr strings.Builder
		cmd.SetIn(strings.NewReader(answer))
		cmd.SetErr(&stderr)

		err := confirmDestructive(cmd, state, "delete", "flow 550e8400")

		if stderr.String() != "Delete flow 550e8400? [y/N] " {
			t.Errorf("answer %q: prompt = %q", answer, stderr.String())
		}
		switch {
		case agrees && err != nil:
			t.Errorf("answer %q: err = %v", answer, err)
		case !agrees && (err == nil || exitCodeOfError(t, err) != 2 || err.Error() != "cancelled"):
			t.Errorf("answer %q: err = %v, want a cancelled error with exit code 2", answer, err)
		}
	}
}

func TestConfirmDestructiveRefusesWithoutATerminalUnlessYes(t *testing.T) {
	cmd := &cobra.Command{}
	var stderr strings.Builder
	cmd.SetIn(strings.NewReader("y\n"))
	cmd.SetErr(&stderr)

	err := confirmDestructive(cmd, &AppState{IsTerminal: terminal(false)}, "delete", "flow 550e8400")

	if err == nil || err.Error() != "pass --yes to delete flow 550e8400 without a prompt" {
		t.Errorf("err = %v", err)
	}
	if stderr.Len() != 0 {
		t.Errorf("prompted without a terminal: %q", stderr.String())
	}
	if exitCodeFrom(err) != 1 {
		t.Errorf("a refusal exits %d, want 1", exitCodeFrom(err))
	}
}

func TestConfirmDestructiveSkipsThePromptWithYes(t *testing.T) {
	for _, onTerminal := range []bool{true, false} {
		cmd := &cobra.Command{}
		var stderr strings.Builder
		cmd.SetIn(strings.NewReader("n\n"))
		cmd.SetErr(&stderr)

		err := confirmDestructive(cmd, &AppState{AssumeYes: true, IsTerminal: terminal(onTerminal)}, "delete", "flow x")

		if err != nil || stderr.Len() != 0 {
			t.Errorf("terminal %v: err = %v, prompt %q", onTerminal, err, stderr.String())
		}
	}
}

func TestConfirmDestructiveNamesTheVerbItAsks(t *testing.T) {
	cmd := &cobra.Command{}
	var stderr strings.Builder
	cmd.SetIn(strings.NewReader("y\n"))
	cmd.SetErr(&stderr)

	if err := confirmDestructive(
		cmd,
		&AppState{IsTerminal: terminal(true)},
		"unpublish",
		"the status page",
	); err != nil {
		t.Fatal(err)
	}
	if stderr.String() != "Unpublish the status page? [y/N] " {
		t.Errorf("prompt = %q", stderr.String())
	}
	err := confirmDestructive(&cobra.Command{}, &AppState{IsTerminal: terminal(false)}, "unpublish", "the status page")
	if err == nil || err.Error() != "pass --yes to unpublish the status page without a prompt" {
		t.Errorf("err = %v", err)
	}
}

func TestIsTerminalRejectsFilesPipesAndEveryNonTerminalCharacterDevice(t *testing.T) {
	file, err := os.Create(filepath.Join(t.TempDir(), "stdin"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()

	candidates := map[string]*os.File{"file": file, "pipe": reader}
	for _, device := range []string{os.DevNull, "/dev/zero", "/dev/urandom", "/dev/full"} {
		opened, openErr := os.Open(device)
		if openErr != nil {
			continue
		}
		defer opened.Close()
		if info, statErr := opened.Stat(); statErr == nil && info.Mode()&os.ModeCharDevice != 0 {
			candidates[device] = opened
		}
	}
	if len(candidates) < 4 {
		t.Fatalf("only %d candidates: the character-device case is not exercised", len(candidates))
	}

	for name, candidate := range candidates {
		if isTerminal(candidate) {
			t.Errorf("%s counted as a terminal", name)
		}
	}
}

type destructiveCase struct {
	name        string
	build       func(*AppState) *cobra.Command
	args        func(stub *apiStub) []string
	verb, thing string
	setup       func(t *testing.T, stub *apiStub)
	performed   func(stub *apiStub) bool
}

func stubPerformed(method, path string) func(*apiStub) bool {
	return func(stub *apiStub) bool { return len(stub.requestsTo(method, path)) > 0 }
}

func destructiveCases() []destructiveCase {
	flowID := idFor("1")
	collectionID := idFor("2")
	folderID := idFor("3")
	return []destructiveCase{
		{
			name:  "flow delete",
			build: newFlowCmd, verb: "delete", thing: "flow " + flowID.String(),
			args: func(*apiStub) []string { return []string{"delete", flowID.String()} },
			setup: func(_ *testing.T, stub *apiStub) {
				stub.on(http.MethodDelete, "/flows/"+flowID.String(), http.StatusNoContent, nil)
			},
			performed: stubPerformed(http.MethodDelete, "/flows/"+flowID.String()),
		},
		{
			name:  "collection delete",
			build: newCollectionCmd, verb: "delete", thing: "collection " + collectionID.String(),
			args: func(*apiStub) []string { return []string{"delete", collectionID.String()} },
			setup: func(_ *testing.T, stub *apiStub) {
				stub.on(http.MethodDelete, "/collections/"+collectionID.String(), http.StatusNoContent, nil)
			},
			performed: stubPerformed(http.MethodDelete, "/collections/"+collectionID.String()),
		},
		{
			name:  "flow folder delete",
			build: newFlowCmd, verb: "delete", thing: "folder Anchor",
			args: func(*apiStub) []string { return []string{"folder", "delete", "Anchor"} },
			setup: func(_ *testing.T, stub *apiStub) {
				stub.on(http.MethodGet, "/flows/folders", http.StatusOK, api.FlowFolderListResponse{
					Count: 1, Total: 1, Items: []api.FlowFolder{sampleFolder(folderID, "Anchor", nil)},
				})
				stub.on(http.MethodDelete, "/flows/folders/"+folderID.String(), http.StatusOK,
					api.FlowFolderDeletionResult{DeletedFolders: 1})
			},
			performed: stubPerformed(http.MethodDelete, "/flows/folders/"+folderID.String()),
		},
		{
			name:  "flow env delete",
			build: newFlowCmd, verb: "delete", thing: "the variables of flow " + flowID.String(),
			args: func(*apiStub) []string { return []string{"env", "delete", flowID.String()} },
			setup: func(_ *testing.T, stub *apiStub) {
				stub.on(http.MethodDelete, "/flows/"+flowID.String()+"/variables", http.StatusNoContent, nil)
			},
			performed: stubPerformed(http.MethodDelete, "/flows/"+flowID.String()+"/variables"),
		},
		{
			name:  "org env delete",
			build: newOrgCmd, verb: "delete", thing: "the organization variable set",
			args: func(*apiStub) []string { return []string{"env", "delete"} },
			setup: func(_ *testing.T, stub *apiStub) {
				stub.on(http.MethodDelete, "/organization/variables", http.StatusNoContent, nil)
			},
			performed: stubPerformed(http.MethodDelete, "/organization/variables"),
		},
		{
			name:  "org env environments delete",
			build: newOrgCmd, verb: "delete", thing: "environment dev",
			args: func(*apiStub) []string { return []string{"env", "environments", "delete", "dev"} },
			setup: func(_ *testing.T, stub *apiStub) {
				stub.on(http.MethodDelete, "/organization/environments/dev", http.StatusNoContent, nil)
			},
			performed: stubPerformed(http.MethodDelete, "/organization/environments/dev"),
		},
		{
			name:  "status-page unpublish",
			build: newStatusPageCmd, verb: "unpublish", thing: "the status page",
			args: func(*apiStub) []string { return []string{"unpublish", "--expected-intent-version", "4"} },
			setup: func(_ *testing.T, stub *apiStub) {
				stub.on(http.MethodPost, "/status-pages/current/unpublish", http.StatusOK,
					map[string]any{"slug": "acme", "published": false})
			},
			performed: stubPerformed(http.MethodPost, "/status-pages/current/unpublish"),
		},
	}
}

func TestEveryDestructiveCommandConfirmsBeforeActing(t *testing.T) {
	prompt := func(c destructiveCase) string {
		return strings.ToUpper(c.verb[:1]) + c.verb[1:] + " " + c.thing + "? [y/N] "
	}
	for _, c := range destructiveCases() {
		t.Run(c.name+" without a terminal", func(t *testing.T) {
			stub := newAPIStub(t)
			c.setup(t, stub)
			state := stub.state(t)
			state.IsTerminal = terminal(false)

			_, stderr, err := execute(t, c.build(state), "y\n", c.args(stub)...)

			want := "pass --yes to " + c.verb + " " + c.thing + " without a prompt"
			if err == nil || err.Error() != want {
				t.Errorf("err = %v, want %q", err, want)
			}
			if c.performed(stub) || stderr != "" {
				t.Errorf("acted or prompted without a terminal: performed %v, stderr %q", c.performed(stub), stderr)
			}
		})
		t.Run(c.name+" declined on a terminal", func(t *testing.T) {
			stub := newAPIStub(t)
			c.setup(t, stub)
			state := stub.state(t)
			state.IsTerminal = terminal(true)

			_, stderr, err := execute(t, c.build(state), "n\n", c.args(stub)...)

			if err == nil || exitCodeOfError(t, err) != 2 {
				t.Errorf("err = %v, want a cancelled error with exit code 2", err)
			}
			if c.performed(stub) {
				t.Error("acted after the answer no")
			}
			if stderr != prompt(c) {
				t.Errorf("prompt = %q, want %q", stderr, prompt(c))
			}
		})
		t.Run(c.name+" agreed on a terminal", func(t *testing.T) {
			stub := newAPIStub(t)
			c.setup(t, stub)
			state := stub.state(t)
			state.IsTerminal = terminal(true)

			_, stderr, err := execute(t, c.build(state), "y\n", c.args(stub)...)

			if err != nil || !c.performed(stub) {
				t.Errorf("err = %v, performed %v", err, c.performed(stub))
			}
			if stderr != prompt(c) {
				t.Errorf("prompt = %q, want %q", stderr, prompt(c))
			}
		})
		t.Run(c.name+" with yes", func(t *testing.T) {
			stub := newAPIStub(t)
			c.setup(t, stub)
			state := stub.state(t)
			state.IsTerminal = terminal(false)
			state.AssumeYes = true

			_, stderr, err := execute(t, c.build(state), "", c.args(stub)...)

			if err != nil || !c.performed(stub) || stderr != "" {
				t.Errorf("err = %v, performed %v, stderr %q", err, c.performed(stub), stderr)
			}
		})
	}
}

func TestProfileDeleteConfirmsBeforeActing(t *testing.T) {
	setup := func(t *testing.T) {
		t.Helper()
		t.Setenv("HOME", t.TempDir())
		store := config.DefaultStore()
		store.Profiles["staging"] = config.Profile{APIBaseURL: "https://staging.example.com"}
		if _, err := config.SaveStore(store); err != nil {
			t.Fatal(err)
		}
	}
	stillThere := func(t *testing.T) bool {
		t.Helper()
		store, _, err := config.LoadStore()
		if err != nil {
			t.Fatal(err)
		}
		_, ok := store.Profiles["staging"]
		return ok
	}

	setup(t)
	state := &AppState{IsTerminal: terminal(false)}
	_, _, err := execute(t, newProfileCmd(state), "y\n", "delete", "staging")
	if err == nil || err.Error() != "pass --yes to delete profile staging without a prompt" || !stillThere(t) {
		t.Errorf("without a terminal: err = %v, still there %v", err, stillThere(t))
	}

	state = &AppState{IsTerminal: terminal(true)}
	_, stderr, err := execute(t, newProfileCmd(state), "n\n", "delete", "staging")
	if err == nil || exitCodeOfError(t, err) != 2 || !stillThere(t) || stderr != "Delete profile staging? [y/N] " {
		t.Errorf("declined: err = %v, still there %v, prompt %q", err, stillThere(t), stderr)
	}

	_, _, err = execute(t, newProfileCmd(state), "yes\n", "delete", "staging")
	if err != nil || stillThere(t) {
		t.Errorf("agreed: err = %v, still there %v", err, stillThere(t))
	}

	setup(t)
	state = &AppState{IsTerminal: terminal(false), AssumeYes: true}
	_, _, err = execute(t, newProfileCmd(state), "", "delete", "staging")
	if err != nil || stillThere(t) {
		t.Errorf("with yes: err = %v, still there %v", err, stillThere(t))
	}
}

func TestConfigResetConfirmsBeforeActing(t *testing.T) {
	setup := func(t *testing.T) {
		t.Helper()
		t.Setenv("HOME", t.TempDir())
		store := config.DefaultStore()
		store.Profiles["staging"] = config.Profile{APIBaseURL: "https://staging.example.com"}
		if _, err := config.SaveStore(store); err != nil {
			t.Fatal(err)
		}
	}
	profiles := func(t *testing.T) int {
		t.Helper()
		store, _, err := config.LoadStore()
		if err != nil {
			t.Fatal(err)
		}
		return len(store.Profiles)
	}

	setup(t)
	_, _, err := execute(t, newConfigCmd(&AppState{IsTerminal: terminal(false)}), "y\n", "reset")
	if want := "pass --yes to reset the configuration and every profile without a prompt"; err == nil ||
		err.Error() != want || profiles(t) != 1 {
		t.Errorf("without a terminal: err = %v, profiles %d", err, profiles(t))
	}

	_, stderr, err := execute(t, newConfigCmd(&AppState{IsTerminal: terminal(true)}), "n\n", "reset")
	if err == nil || exitCodeOfError(t, err) != 2 || profiles(t) != 1 ||
		stderr != "Reset the configuration and every profile? [y/N] " {
		t.Errorf("declined: err = %v, profiles %d, prompt %q", err, profiles(t), stderr)
	}

	if _, _, err = execute(
		t,
		newConfigCmd(&AppState{IsTerminal: terminal(true)}),
		"y\n",
		"reset",
	); err != nil ||
		profiles(t) != 0 {
		t.Errorf("agreed: err = %v, profiles %d", err, profiles(t))
	}

	setup(t)
	if _, _, err = execute(t, newConfigCmd(&AppState{AssumeYes: true}), "", "reset"); err != nil || profiles(t) != 0 {
		t.Errorf("with yes: err = %v, profiles %d", err, profiles(t))
	}
}

func TestFlowFolderDeleteAsksAboutTheFlowsItWouldDeleteToo(t *testing.T) {
	folderID := idFor("3")
	stub := newAPIStub(t)
	stub.on(http.MethodGet, "/flows/folders", http.StatusOK, api.FlowFolderListResponse{
		Count: 1, Total: 1, Items: []api.FlowFolder{sampleFolder(folderID, "Anchor", nil)},
	})
	stub.on(http.MethodDelete, "/flows/folders/"+folderID.String(), http.StatusOK, api.FlowFolderDeletionResult{})
	state := stub.state(t)
	state.IsTerminal = terminal(true)

	_, stderr, err := execute(t, newFlowCmd(state), "n\n", "folder", "delete", "Anchor", "--delete-flows")

	if err == nil || stderr != "Delete folder Anchor and every flow in it? [y/N] " {
		t.Errorf("err = %v, prompt %q", err, stderr)
	}
	if len(stub.requestsTo(http.MethodDelete, "/flows/folders/"+folderID.String())) != 0 {
		t.Error("deleted after the answer no")
	}
}

func TestFlowFolderDeleteHasNoLocalYesAnyMore(t *testing.T) {
	for _, group := range [][]string{{"flow", "folder", "delete"}, {"flows", "folder", "delete"}} {
		cmd, _, err := NewRootCmd().Find(group)
		if err != nil {
			t.Fatal(err)
		}
		if cmd.LocalNonPersistentFlags().Lookup("yes") != nil {
			t.Errorf("%v has a local --yes", group)
		}
	}
	yes := NewRootCmd().PersistentFlags().Lookup("yes")
	if yes == nil || yes.Shorthand != "y" {
		t.Errorf("the persistent --yes = %+v, want it with -y", yes)
	}
}

func TestYesOnTheCommandLineLetsADestructiveCommandRunWithoutATerminal(t *testing.T) {
	flowID := idFor("1")
	for _, tc := range []struct {
		args    []string
		deletes bool
	}{
		{[]string{"flow", "delete", flowID.String()}, false},
		{[]string{"flow", "delete", flowID.String(), "--yes"}, true},
		{[]string{"flows", "delete", "-y", flowID.String()}, true},
	} {
		stub := newAPIStub(t)
		stub.on(http.MethodDelete, "/flows/"+flowID.String(), http.StatusNoContent, nil)
		isolateCLIEnvironment(t, stub.server.URL)

		_, stderr, code := runCLI(t, tc.args...)

		deleted := len(stub.requestsTo(http.MethodDelete, "/flows/"+flowID.String())) == 1
		if deleted != tc.deletes {
			t.Errorf("%v: deleted %v, want %v (exit %d, stderr %q)", tc.args, deleted, tc.deletes, code, stderr)
		}
		if tc.deletes && code != 0 || !tc.deletes && code != 1 {
			t.Errorf("%v: exit %d", tc.args, code)
		}
	}
}
