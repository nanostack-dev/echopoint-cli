package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func groupPaths(root *cobra.Command) [][]string {
	var groups [][]string
	var walk func(cmd *cobra.Command, path []string)
	walk = func(cmd *cobra.Command, path []string) {
		for _, child := range cmd.Commands() {
			childPath := append(append([]string{}, path...), child.Name())
			if child.HasSubCommands() {
				groups = append(groups, childPath)
				walk(child, childPath)
			}
		}
	}
	walk(root, nil)
	return groups
}

func runRoot(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	root := NewRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func TestEveryCommandGroupRejectsAnUnknownSubcommand(t *testing.T) {
	groups := groupPaths(NewRootCmd())
	if len(groups) < 10 {
		t.Fatalf("found only %d command groups: %v", len(groups), groups)
	}
	for _, group := range groups {
		stdout, stderr, err := runRoot(t, append(append([]string{}, group...), "zzzzzz")...)

		want := `unknown command "zzzzzz" for "echopoint ` + strings.Join(group, " ") + `"`
		if err == nil || err.Error() != want {
			t.Errorf("%v: err = %v, want %q", group, err, want)
		}
		if stdout != "" || stderr != "" {
			t.Errorf("%v printed stdout %q, stderr %q: the error is printed once, by the caller", group, stdout, stderr)
		}
	}
}

func TestEveryCommandGroupStillPrintsItsHelpWhenBare(t *testing.T) {
	for _, group := range groupPaths(NewRootCmd()) {
		stdout, _, err := runRoot(t, group...)

		if err != nil {
			t.Errorf("%v: err = %v", group, err)
		}
		if !strings.Contains(stdout, "Usage:") ||
			!strings.Contains(stdout, "echopoint "+strings.Join(group, " ")+" [command]") {
			t.Errorf("%v: help:\n%s", group, stdout)
		}
	}
}

func TestUnknownSubcommandSuggestsTheCommandsItIsCloseTo(t *testing.T) {
	for args, suggestion := range map[string]string{
		"spec lnt":    "lint",
		"flow lst":    "list",
		"flows lst":   "list",
		"spec pul":    "pull",
		"flow creat":  "create",
		"flows creat": "create",
	} {
		_, _, err := runRoot(t, strings.Fields(args)...)

		if err == nil || !strings.Contains(err.Error(), "\n\nDid you mean this?\n") ||
			!strings.Contains(err.Error(), "\t"+suggestion+"\n") && !strings.HasSuffix(err.Error(), "\t"+suggestion) {
			t.Errorf("%s: err = %v", args, err)
		}
	}
}

func TestUnknownSubcommandWithoutACloseMatchSuggestsNothing(t *testing.T) {
	_, _, err := runRoot(t, "spec", "zzzzzz")

	if err == nil || strings.Contains(err.Error(), "Did you mean") {
		t.Errorf("err = %v", err)
	}
}

func TestRemovedSpecCommandsAreUnknown(t *testing.T) {
	for _, removed := range []string{"validate", "fmt"} {
		_, _, err := runRoot(t, "spec", removed, "pets-api")

		if err == nil || !strings.Contains(err.Error(), `unknown command "`+removed+`" for "echopoint spec"`) {
			t.Errorf("%s: err = %v", removed, err)
		}
	}
}

func TestUnknownCommandAtTheRootStillFails(t *testing.T) {
	_, _, err := runRoot(t, "flwos")

	if err == nil || !strings.Contains(err.Error(), `unknown command "flwos" for "echopoint"`) ||
		!strings.Contains(err.Error(), "flow") {
		t.Errorf("err = %v", err)
	}
}

func TestABareGroupNeedsNoConfigurationAndACommandDoes(t *testing.T) {
	broken := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(broken, []byte("profiles: [unterminated"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ECHOPOINT_CONFIG", broken)

	if _, _, err := runRoot(t, "spec"); err != nil {
		t.Errorf("bare group: err = %v", err)
	}
	if _, _, err := runRoot(t, "spec", "list"); err == nil {
		t.Error("spec list ran with a broken config")
	}
}
