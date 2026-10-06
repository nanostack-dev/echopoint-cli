package commands

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func walkCommands(root *cobra.Command, visit func(path string, cmd *cobra.Command)) {
	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		visit(cmd.CommandPath(), cmd)
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(root)
}

func TestPluralAliasesReachTheSameCommands(t *testing.T) {
	root := NewRootCmd()
	for plural, singular := range map[string]string{
		"flows":        "flow",
		"collections":  "collection",
		"status-pages": "status-page",
	} {
		group, _, err := root.Find([]string{singular})
		if err != nil || !slices.Contains(group.Aliases, plural) {
			t.Fatalf("%s is not an alias of %s: %v", plural, singular, err)
		}
		walkCommands(group, func(path string, cmd *cobra.Command) {
			if cmd == group || !cmd.Runnable() {
				return
			}
			words := strings.Fields(strings.TrimPrefix(path, "echopoint "))
			viaAlias, _, err := root.Find(append([]string{plural}, words[1:]...))
			if err != nil || viaAlias != cmd {
				t.Errorf("%s %v does not reach %s: %v", plural, words[1:], path, err)
			}
		})
	}
}

func TestGetAndShowStayAsAliasesOfView(t *testing.T) {
	root := NewRootCmd()
	for _, tc := range []struct {
		view    []string
		aliases []string
	}{
		{[]string{"flow", "view"}, []string{"get", "show"}},
		{[]string{"collection", "view"}, []string{"get"}},
		{[]string{"status-page", "view"}, []string{"get"}},
		{[]string{"flow", "execution", "view"}, []string{"get"}},
		{[]string{"flow", "env", "view"}, []string{"get"}},
		{[]string{"org", "env", "view"}, []string{"get"}},
		{[]string{"config", "view"}, []string{"show"}},
	} {
		view := find(t, root, tc.view...)
		for _, alias := range tc.aliases {
			reached, _, err := root.Find(append(slices.Clone(tc.view[:len(tc.view)-1]), alias))
			if err != nil || reached != view {
				t.Errorf("%v: %s does not reach it: %v", tc.view, alias, err)
			}
		}
	}
}

func TestTheRetiredVerbsAndNounsAreGone(t *testing.T) {
	_, _, err := runRoot(t, "flow", "create-interactive")
	if err == nil || !strings.Contains(err.Error(), `unknown command "create-interactive" for "echopoint flow"`) {
		t.Errorf("create-interactive: err = %v", err)
	}

	var names []string
	walkCommands(NewRootCmd(), func(_ string, cmd *cobra.Command) { names = append(names, cmd.Name()) })
	for _, retired := range []string{"flows", "collections", "status-pages", "create-interactive"} {
		if slices.Contains(names, retired) {
			t.Errorf("%q is still the name of a command", retired)
		}
	}
	for _, root := range NewRootCmd().Commands() {
		if root.Name() == "get" || root.Name() == "show" {
			t.Errorf("%s is a command of the root", root.Name())
		}
	}
}

func TestRootCommandsAreGroupedUnderTheirHeadings(t *testing.T) {
	stdout, _, err := runRoot(t, "--help")
	if err != nil {
		t.Fatal(err)
	}

	sections := map[string][]string{}
	var heading string
	for line := range strings.SplitSeq(stdout, "\n") {
		switch {
		case line == "Resources:" || line == "Account and setup:" || line == "Tools:":
			heading = strings.TrimSuffix(line, ":")
		case strings.HasPrefix(line, "  ") && heading != "":
			sections[heading] = append(sections[heading], strings.Fields(line)[0])
		case strings.TrimSpace(line) == "":
			heading = ""
		}
	}

	want := map[string][]string{
		"Resources":         {"collection", "flow", "org", "spec", "status-page"},
		"Account and setup": {"auth", "config", "profile"},
		"Tools":             {"completion", "help", "mcp", "update", "version"},
	}
	for heading, names := range want {
		if !slices.Equal(sections[heading], names) {
			t.Errorf("%s = %v, want %v", heading, sections[heading], names)
		}
	}
	if strings.Contains(stdout, "Available Commands:") || strings.Contains(stdout, "Additional Commands:") {
		t.Errorf("a root command is outside every group:\n%s", stdout)
	}
	if strings.Contains(stdout, "webhooks") || strings.Contains(stdout, "analytics") {
		t.Errorf("the root help still describes what the CLI no longer manages:\n%s", stdout)
	}
	for _, vocabulary := range []string{"Cloud", "Self-hosted", "Ephemeral"} {
		if !strings.Contains(stdout, vocabulary) {
			t.Errorf("the root help does not explain %s", vocabulary)
		}
	}
}

func TestEveryRootCommandBelongsToAGroup(t *testing.T) {
	root := NewRootCmd()
	for _, command := range root.Commands() {
		if command.GroupID == "" || !root.ContainsGroup(command.GroupID) {
			t.Errorf("%s is in group %q", command.Name(), command.GroupID)
		}
	}
}

func TestEveryLeafCommandHasAnExample(t *testing.T) {
	walkCommands(NewRootCmd(), func(path string, cmd *cobra.Command) {
		if cmd.HasSubCommands() || !cmd.Runnable() {
			return
		}
		if !strings.Contains(cmd.Example, "echopoint ") {
			t.Errorf("%s has no Example", path)
		}
	})
}

func TestNoHelpTextStillUsesTheRetiredNames(t *testing.T) {
	retired := []string{
		"echopoint flows ", "echopoint collections ", "echopoint status-pages ", "create-interactive",
		"<flow>", "flows get", "flows show", "config show", "status-pages get",
	}
	walkCommands(NewRootCmd(), func(path string, cmd *cobra.Command) {
		text := strings.Join([]string{cmd.Use, cmd.Short, cmd.Long, cmd.Example}, "\n")
		cmd.Flags().VisitAll(func(flag *pflag.Flag) { text += "\n" + flag.Usage })
		for _, name := range retired {
			if strings.Contains(text, name) {
				t.Errorf("%s still says %q", path, name)
			}
		}
	})
}

func TestFlowArgumentsAreCalledFlowAndCompleteFlows(t *testing.T) {
	walkCommands(find(t, NewRootCmd(), "flow"), func(path string, cmd *cobra.Command) {
		if !cmd.Runnable() || cmd.HasSubCommands() {
			return
		}
		if strings.Contains(cmd.Use, "<flow>") || strings.Contains(cmd.Use, "<id>") {
			t.Errorf("%s names its flow argument %q", path, cmd.Use)
		}
		words := strings.Fields(cmd.Use)
		takesFlow := len(words) > 1 && (words[1] == "<flow-id>" || words[1] == "[<flow-id>...]")
		if takesFlow && cmd.ValidArgsFunction == nil {
			t.Errorf("%s takes a <flow-id> and completes nothing", path)
		}
	})
}

func TestOrgIsTheVisibleFlagAndOrganizationIDIsAHiddenAlias(t *testing.T) {
	root := NewRootCmd()
	org := root.PersistentFlags().Lookup("org")
	alias := root.PersistentFlags().Lookup("organization-id")
	if org == nil || org.Hidden || alias == nil || !alias.Hidden {
		t.Fatalf("org = %+v, organization-id = %+v", org, alias)
	}

	stdout, _, _ := runRoot(t, "flow", "list", "--help")
	if !strings.Contains(stdout, "--org ") || strings.Contains(stdout, "organization-id") {
		t.Errorf("help:\n%s", stdout)
	}
}

func TestOrgAndItsAliasSelectTheOrganizationAndTheEnvironmentVariableStillDoes(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		env  string
		want string
	}{
		"--org":             {[]string{"--org", "org_flag"}, "", "org_flag"},
		"--organization-id": {[]string{"--organization-id", "org_alias"}, "", "org_alias"},
		"environment":       {nil, "org_env", "org_env"},
		"flag over env":     {[]string{"--org", "org_flag"}, "org_env", "org_flag"},
	} {
		t.Run(name, func(t *testing.T) {
			stub := newAPIStub(t)
			stub.on(http.MethodGet, "/flows", http.StatusOK, map[string]any{"count": 0, "total": 0, "items": []any{}})
			isolateCLIEnvironment(t, stub.server.URL)
			t.Setenv("ECHOPOINT_ORGANIZATION_ID", tc.env)

			_, stderr, code := runCLI(t, append([]string{"flow", "list"}, tc.args...)...)

			requests := stub.requestsTo(http.MethodGet, "/flows")
			if code != 0 || len(requests) != 1 {
				t.Fatalf("exit %d, %d requests, stderr %q", code, len(requests), stderr)
			}
			if got := requests[0].header.Get("X-Organization-Id"); got != tc.want {
				t.Errorf("organization = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAuthLoginStoresTheOrganizationFromOrgOrItsHiddenAlias(t *testing.T) {
	for _, flag := range []string{"--org", "--organization-id"} {
		stub := newAPIStub(t)
		isolateCLIEnvironment(t, stub.server.URL)
		t.Setenv("ECHOPOINT_API_KEY", "")
		t.Setenv("ECHOPOINT_ORGANIZATION_ID", "")

		stdout, stderr, code := runCLI(t, "auth", "login", "--api-key", "key-1", flag, "org_stored")

		if code != 0 || !strings.Contains(stdout, "Organization: org_stored") {
			t.Errorf("%s: exit %d, stdout %q, stderr %q", flag, code, stdout, stderr)
		}
		if stub.requestCount() != 0 {
			t.Errorf("%s: resolved the organization over the network although it was given", flag)
		}
	}
}
