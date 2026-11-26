package commands

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/output"

	"github.com/spf13/cobra"
)

// variableScope is one owner of a variable set: the organization or the
// selected workspace. The env commands of both are built from it, so they read,
// write and print the same way and differ only in the calls they make.
//
// Echopoint ADR-0043: an environment is one catalog entry of the organization.
// A shared entry (scope organization) is seen by every workspace; a workspace
// entry is seen by its workspace only. A name is unique in the organization.
type variableScope struct {
	// noun names the owner in help and output: "organization" or "workspace".
	noun string
	// command is the command path up to env, for the examples: "org env".
	command string
	// showScope lists environments with their scope. The organization lists
	// only its shared entries, so the scope says nothing there.
	showScope bool

	read   func(ctx context.Context, state *AppState) (*api.VariableSet, error)
	write  func(ctx context.Context, state *AppState, environment, key string, body api.SetVariableRequest) error
	remove func(ctx context.Context, state *AppState, environment, key string) error

	search            func(state *AppState) pageFetcher[api.Environment]
	createEnvironment func(ctx context.Context, state *AppState, name string) (*api.Environment, error)
	deleteEnvironment func(ctx context.Context, state *AppState, name string) error

	// environmentsLong is the help of the environments group.
	environmentsLong string
	// deleteEnvironmentShort and deleteEnvironmentLong describe what a delete
	// of one environment removes, which differs by owner.
	deleteEnvironmentShort string
	deleteEnvironmentLong  string
}

func (s variableScope) title() string {
	return strings.ToUpper(s.noun[:1]) + s.noun[1:]
}

// require fails before any call that has no organization context. A workspace
// command needs no --workspace: without one the API uses the organization's
// oldest workspace.
func (s variableScope) require(state *AppState) error {
	if err := requireToken(state); err != nil {
		return err
	}
	return requireOrg(state)
}

// environmentFlag registers -e/--environment with completion from this
// owner's environments.
func (s variableScope) environmentFlag(state *AppState, cmd *cobra.Command, target *string) {
	cmd.Flags().StringVarP(target, "environment", "e", "", "Target a named environment instead of the base layer")
	_ = cmd.RegisterFlagCompletionFunc("environment", completeEnvironments(state, s.search))
}

func (s variableScope) newViewCmd(state *AppState) *cobra.Command {
	var (
		environment string
		showValues  bool
	)

	cmd := &cobra.Command{
		Use:     viewVerb,
		Aliases: []string{getVerb},
		Short:   fmt.Sprintf("Show %s variables", s.noun),
		Example: fmt.Sprintf(`  echopoint %[1]s view
  echopoint %[1]s view -e prd --show-values`, s.command),
		Long: fmt.Sprintf(`Show %s variables.

A variable set can hold live credentials, so values are hidden by default and
only names are shown. Pass --show-values to reveal them. A secret has no value
to reveal: a read never returns one.`, s.noun),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := s.require(state); err != nil {
				return err
			}

			set, err := s.read(cmd.Context(), state)
			if err != nil {
				return err
			}

			if environment != "" {
				layer, ok := set.Environments[environment]
				if !ok {
					return fmt.Errorf("environment %q not found", environment)
				}
				vars := layerValues(layer)
				switch state.OutputFormat {
				case output.FormatJSON:
					return output.PrintJSON(os.Stdout, layerPayload(vars, showValues))
				case output.FormatYAML:
					return output.PrintYAML(os.Stdout, layerPayload(vars, showValues))
				}
				printVars(os.Stdout, fmt.Sprintf("Environment %q variables", environment), vars, showValues)
				return nil
			}

			base := layerValues(set.Base)
			names := make([]string, 0, len(set.Environments))
			for name := range set.Environments {
				names = append(names, name)
			}
			sort.Strings(names)

			payload := map[string]any{"base": layerPayload(base, showValues), environmentsVerb: names}
			switch state.OutputFormat {
			case output.FormatJSON:
				return output.PrintJSON(os.Stdout, payload)
			case output.FormatYAML:
				return output.PrintYAML(os.Stdout, payload)
			}

			printVars(os.Stdout, s.title()+" base variables", base, showValues)
			if len(names) > 0 {
				fmt.Printf("\nEnvironments: %s\n", strings.Join(names, ", "))
			}
			return nil
		},
	}

	s.environmentFlag(state, cmd, &environment)
	cmd.Flags().BoolVar(&showValues, "show-values", false, "Reveal variable values instead of names only")
	return quietOnError(cmd)
}

// writeAll writes each variable on its own, in key order.
func (s variableScope) writeAll(
	ctx context.Context, state *AppState, environment string, updates map[string]string, secret bool,
) error {
	for _, key := range sortedKeys(updates) {
		body := api.SetVariableRequest{Value: updates[key]}
		if secret {
			body.Secret = &secret
		}
		if err := s.write(ctx, state, environment, key, body); err != nil {
			return err
		}
	}
	return nil
}

func (s variableScope) newSetCmd(state *AppState) *cobra.Command {
	var (
		variables   []string
		environment string
		secret      bool
	)

	cmd := &cobra.Command{
		Use:   "set",
		Short: fmt.Sprintf("Set %s variables", s.noun),
		Long: fmt.Sprintf(`Set %s variables.

Each variable is written on its own; the others are left alone. Use
-e/--environment to target a named environment, which has to exist already.

Pass --secret to encrypt the values at rest. A read never returns a secret, and
execution results, progress events and flow exports replace it with `+"`***`"+`. A
plain variable can become a secret; the reverse is refused, so delete it and set
it again.`, s.noun),
		Example: fmt.Sprintf(`  echopoint %[1]s set --var KEY=value
  echopoint %[1]s set --var KEY1=v1 --var KEY2=v2
  echopoint %[1]s set -e prd --var BASE_URL=https://api.example.com
  echopoint %[1]s set --secret --var API_KEY=sk-live-...`, s.command),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := s.require(state); err != nil {
				return err
			}
			if len(variables) == 0 {
				return fmt.Errorf("at least one --var KEY=value is required")
			}

			updates, err := parseVarFlags(variables)
			if err != nil {
				return err
			}
			if err := s.writeAll(cmd.Context(), state, environment, updates, secret); err != nil {
				return err
			}

			kind := "variable(s)"
			if secret {
				kind = "secret(s)"
			}
			fmt.Printf("Set %d %s in the %s %s\n", len(updates), kind, s.noun, layerTarget(environment))
			return nil
		},
	}

	cmd.Flags().StringArrayVar(&variables, "var", nil, "Variable in KEY=value format (repeatable)")
	s.environmentFlag(state, cmd, &environment)
	cmd.Flags().BoolVar(&secret, "secret", false, "Store the values as secrets (encrypted, never read back)")
	return quietOnError(cmd)
}

func (s variableScope) newUnsetCmd(state *AppState) *cobra.Command {
	var environment string

	cmd := &cobra.Command{
		Use:   "unset KEY [KEY...]",
		Short: fmt.Sprintf("Remove %s variables", s.noun),
		Example: fmt.Sprintf(`  echopoint %[1]s unset API_KEY
  echopoint %[1]s unset -e prd BASE_URL`, s.command),
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := s.require(state); err != nil {
				return err
			}
			for _, key := range args {
				if err := s.remove(cmd.Context(), state, environment, key); err != nil {
					return err
				}
			}
			fmt.Printf("Removed %d variable(s) from the %s %s\n", len(args), s.noun, layerTarget(environment))
			return nil
		},
	}

	s.environmentFlag(state, cmd, &environment)
	return quietOnError(cmd)
}

func (s variableScope) newImportCmd(state *AppState) *cobra.Command {
	var (
		file        string
		environment string
		secret      bool
	)

	cmd := &cobra.Command{
		Use:   "import -f <file>",
		Short: fmt.Sprintf("Import %s variables from a file", s.noun),
		Long: `Import variables from a JSON object ({"KEY":"value"}) or a dotenv (KEY=value) file.

Each variable is written on its own; the others are left alone. Pass --secret to
store every imported value as a secret.`,
		Example: fmt.Sprintf(`  echopoint %[1]s import -f .env
  echopoint %[1]s import -f vars.json -e prd --secret`, s.command),
		Args: fileFlagArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := s.require(state); err != nil {
				return err
			}

			updates, err := parseVarFile(file)
			if err != nil {
				return err
			}
			if len(updates) == 0 {
				return fmt.Errorf("no variables found in %s", file)
			}
			if err := s.writeAll(cmd.Context(), state, environment, updates, secret); err != nil {
				return err
			}

			fmt.Printf("Imported %d variable(s) into the %s %s from %s\n",
				len(updates), s.noun, layerTarget(environment), file)
			return nil
		},
	}

	addFileFlag(cmd, &file, "JSON or dotenv file of variables", "json", "env")
	_ = cmd.MarkFlagRequired("file")
	s.environmentFlag(state, cmd, &environment)
	cmd.Flags().BoolVar(&secret, "secret", false, "Store the imported values as secrets")
	return quietOnError(cmd)
}

func (s variableScope) newEnvironmentsCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:     environmentsVerb,
		Aliases: []string{"env"},
		Short:   fmt.Sprintf("Manage the environments the %s sees", s.noun),
		Long:    s.environmentsLong,
	}
	cmd.AddCommand(
		s.newEnvironmentsListCmd(state),
		s.newEnvironmentsCreateCmd(state),
		s.newEnvironmentsDeleteCmd(state),
	)
	return cmd
}

// environmentRow is one listed environment, with its scope.
type environmentRow struct {
	Name  string `json:"name"`
	Scope string `json:"scope"`
}

func (s variableScope) newEnvironmentsListCmd(state *AppState) *cobra.Command {
	return quietOnError(&cobra.Command{
		Use:     listVerb,
		Short:   "List named environments",
		Example: fmt.Sprintf(`  echopoint %s environments list`, s.command),
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := s.require(state); err != nil {
				return err
			}

			// the list is the names of every environment: walk the search to its end
			environments, err := walkAll(cmd.Context(), s.search(state))
			if err != nil {
				return fmt.Errorf("failed to list environments: %w", err)
			}
			sort.Slice(environments, func(i, j int) bool { return environments[i].Name < environments[j].Name })

			out := cmd.OutOrStdout()
			if !s.showScope {
				names := make([]string, 0, len(environments))
				for _, environment := range environments {
					names = append(names, environment.Name)
				}
				switch state.OutputFormat {
				case output.FormatJSON:
					return output.PrintJSON(out, names)
				case output.FormatYAML:
					return output.PrintYAML(out, names)
				}
				if len(names) == 0 {
					fmt.Fprintln(out, "No environments")
				}
				for _, name := range names {
					fmt.Fprintf(out, "  %s\n", name)
				}
				return nil
			}

			rows := make([]environmentRow, 0, len(environments))
			for _, environment := range environments {
				rows = append(rows, environmentRow{Name: environment.Name, Scope: string(environment.Scope)})
			}
			switch state.OutputFormat {
			case output.FormatJSON:
				return output.PrintJSON(out, rows)
			case output.FormatYAML:
				return output.PrintYAML(out, rows)
			}
			if len(rows) == 0 {
				fmt.Fprintln(out, "No environments")
				return nil
			}
			table := make([][]string, 0, len(rows))
			for _, row := range rows {
				table = append(table, []string{row.Name, scopeLabel(row.Scope)})
			}
			return output.PrintTableTo(out, []string{columnName, "Scope"}, table)
		},
	})
}

// scopeLabel names a scope the way the web app does.
func scopeLabel(scope string) string {
	if scope == string(api.EnvironmentScopeOrganization) {
		return "shared"
	}
	return scope
}

func (s variableScope) newEnvironmentsCreateCmd(state *AppState) *cobra.Command {
	return quietOnError(&cobra.Command{
		Use:   createVerb + " <name>",
		Short: "Create a named environment",
		Long: fmt.Sprintf(`Create a named environment of the %s, such as dev or prd.

It starts empty, and stays empty until a variable is written into it. A name is
unique in the organization: a name that already exists, shared or in any
workspace, is refused with ENVIRONMENT_ALREADY_EXISTS.`, s.noun),
		Example: fmt.Sprintf(`  echopoint %s environments create prd`, s.command),
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := s.require(state); err != nil {
				return err
			}
			environment, err := s.createEnvironment(cmd.Context(), state, args[0])
			if err != nil {
				return err
			}
			fmt.Printf("Created environment %q\n", environment.Name)
			return nil
		},
	})
}

func (s variableScope) newEnvironmentsDeleteCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   deleteVerb + " <name>",
		Short: s.deleteEnvironmentShort,
		Long:  s.deleteEnvironmentLong,
		Example: fmt.Sprintf(`  echopoint %[1]s environments delete dev
  echopoint %[1]s environments delete dev --yes`, s.command),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := s.require(state); err != nil {
				return err
			}
			if err := confirmDestructive(cmd, state, "delete", "environment "+args[0]); err != nil {
				return err
			}
			if err := s.deleteEnvironment(cmd.Context(), state, args[0]); err != nil {
				return err
			}
			fmt.Printf("Deleted environment %q\n", args[0])
			return nil
		},
	}
	cmd.ValidArgsFunction = completeEnvironmentArg(state, s.search)
	return quietOnError(cmd)
}

// expectStatus turns a response that is not the expected status into an error.
func expectStatus(resp *http.Response, body []byte, want int) error {
	if resp != nil && resp.StatusCode == want {
		return nil
	}
	return formatAPIError(resp, body)
}
