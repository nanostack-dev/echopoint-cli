package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/output"

	"github.com/spf13/cobra"
)

// secretPlaceholder stands where a secret's value would be printed. A read
// never returns one, so there is nothing else to show.
const secretPlaceholder = "<secret>"

// newOrgCmd creates the top-level "org" command group for organization-scoped
// resources. Today it hosts variable management; future org-wide resources
// belong here too.
func newOrgCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "org",
		Short: "Manage organization-scoped resources",
	}
	cmd.AddCommand(newOrgEnvCmd(state))
	return cmd
}

func newOrgEnvCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   envCommandName,
		Short: "Manage organization variables",
		Long: `Manage organization variables.

A variable set holds two kinds of layer:
  - base variables, loaded for every flow execution
  - named environments (e.g. dev, staging, prd) selected at launch time

A variable can be stored as a secret. A secret is encrypted at rest, is never
returned by a read, and is replaced by ` + "`***`" + ` in execution results, progress
events and flow exports. A plain variable can become a secret; the reverse is
refused, so delete it and set it again.`,
	}
	cmd.AddCommand(
		newOrgEnvGetCmd(state),
		newOrgEnvSetCmd(state),
		newOrgEnvUnsetCmd(state),
		newOrgEnvImportCmd(state),
		newOrgEnvDeleteCmd(state),
		newOrgEnvironmentsCmd(state),
	)
	return cmd
}

// requireOrg fails before any call that needs an organization context.
func requireOrg(state *AppState) error {
	if strings.TrimSpace(state.OrganizationID) == "" {
		return fmt.Errorf(
			"organization context required: set --organization-id, ECHOPOINT_ORGANIZATION_ID, or log in with a default organization",
		)
	}
	return nil
}

// fetchOrgVariables reads the organization's variable set.
func fetchOrgVariables(state *AppState) (*api.VariableSet, error) {
	resp, err := state.Client.API().GetOrganizationVariablesWithResponse(context.Background(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get organization variables: %w", err)
	}
	if resp.JSON200 == nil {
		return nil, formatAPIError(resp.HTTPResponse, resp.Body)
	}
	return resp.JSON200, nil
}

// layerValues flattens a layer for display. A secret has no value to show, so
// it renders as the placeholder rather than an empty string, which would read
// as "set to nothing".
func layerValues(layer api.VariableLayer) map[string]string {
	values := make(map[string]string, len(layer))
	for name, variable := range layer {
		switch {
		case variable.Secret:
			values[name] = secretPlaceholder
		case variable.Value != nil:
			values[name] = *variable.Value
		default:
			values[name] = ""
		}
	}
	return values
}

// setOrgVariable writes one variable into the base layer or into a named
// environment. The environment has to exist already, so a misspelled name
// fails rather than quietly creating a layer nothing reads.
func setOrgVariable(state *AppState, environment, key, value string, secret bool) error {
	body := api.SetVariableRequest{Value: value}
	if secret {
		body.Secret = &secret
	}

	if environment == "" {
		resp, err := state.Client.API().SetOrganizationVariableWithResponse(
			context.Background(), key, nil, api.SetOrganizationVariableJSONRequestBody(body),
		)
		if err != nil {
			return fmt.Errorf("failed to set %s: %w", key, err)
		}
		if resp.JSON200 == nil {
			return formatAPIError(resp.HTTPResponse, resp.Body)
		}
		return nil
	}

	resp, err := state.Client.API().SetEnvironmentVariableWithResponse(
		context.Background(), environment, key, nil, api.SetEnvironmentVariableJSONRequestBody(body),
	)
	if err != nil {
		return fmt.Errorf("failed to set %s in %s: %w", key, environment, err)
	}
	if resp.JSON200 == nil {
		return formatAPIError(resp.HTTPResponse, resp.Body)
	}
	return nil
}

// deleteOrgVariable removes one variable from the base layer or an environment.
func deleteOrgVariable(state *AppState, environment, key string) error {
	if environment == "" {
		resp, err := state.Client.API().DeleteOrganizationVariableWithResponse(
			context.Background(), key, nil,
		)
		if err != nil {
			return fmt.Errorf("failed to delete %s: %w", key, err)
		}
		if resp.StatusCode() != http.StatusNoContent {
			return formatAPIError(resp.HTTPResponse, resp.Body)
		}
		return nil
	}

	resp, err := state.Client.API().DeleteEnvironmentVariableWithResponse(
		context.Background(), environment, key, nil,
	)
	if err != nil {
		return fmt.Errorf("failed to delete %s from %s: %w", key, environment, err)
	}
	if resp.StatusCode() != http.StatusNoContent {
		return formatAPIError(resp.HTTPResponse, resp.Body)
	}
	return nil
}

// layerTarget names the layer a write landed in, for the line printed after it.
func layerTarget(environment string) string {
	if environment == "" {
		return "base layer"
	}
	return fmt.Sprintf("environment %q", environment)
}

// sortedKeys returns the keys of a string-valued map, sorted ascending.
func sortedKeys(vars map[string]string) []string {
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// printVars renders a flat variable map for table output, sorted by key. By
// default values are hidden (names only) so a credential never lands in
// scrollback or a screenshot by accident; showValues opts into printing the
// actual values. A secret prints as the placeholder either way.
func printVars(w io.Writer, title string, vars map[string]string, showValues bool) {
	if len(vars) == 0 {
		fmt.Fprintln(w, "No variables set")
		return
	}
	keys := sortedKeys(vars)

	if !showValues {
		fmt.Fprintf(w, "%s: %d variable(s) (values hidden, use --show-values to reveal)\n\n", title, len(keys))
		for _, k := range keys {
			fmt.Fprintf(w, "  %s\n", k)
		}
		return
	}

	fmt.Fprintf(w, "%s:\n\n", title)
	for _, k := range keys {
		fmt.Fprintf(w, "  %s=%s\n", k, vars[k])
	}
}

// variableNames is the redacted JSON/YAML shape: names only, never values.
func variableNames(vars map[string]string) []string { return sortedKeys(vars) }

func layerPayload(vars map[string]string, showValues bool) any {
	if showValues {
		return vars
	}
	return variableNames(vars)
}

func newOrgEnvGetCmd(state *AppState) *cobra.Command {
	var (
		environment string
		showValues  bool
	)

	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get organization variables",
		Long: `Get organization variables.

A variable set can hold live credentials, so values are hidden by default and
only names are shown. Pass --show-values to reveal them. A secret has no value
to reveal: a read never returns one.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			if err := requireOrg(state); err != nil {
				return err
			}

			set, err := fetchOrgVariables(state)
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
				printVars(
					os.Stdout,
					fmt.Sprintf("Environment %q variables", environment),
					vars,
					showValues,
				)
				return nil
			}

			base := layerValues(set.Base)
			names := make([]string, 0, len(set.Environments))
			for name := range set.Environments {
				names = append(names, name)
			}
			sort.Strings(names)

			switch state.OutputFormat {
			case output.FormatJSON:
				return output.PrintJSON(os.Stdout, map[string]any{
					"base":           layerPayload(base, showValues),
					environmentsVerb: names,
				})
			case output.FormatYAML:
				return output.PrintYAML(os.Stdout, map[string]any{
					"base":           layerPayload(base, showValues),
					environmentsVerb: names,
				})
			}

			printVars(os.Stdout, "Organization base variables", base, showValues)
			if len(names) > 0 {
				fmt.Printf("\nEnvironments: %s\n", strings.Join(names, ", "))
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&environment, "environment", "e", "", "Target a named environment instead of the base layer")
	cmd.Flags().BoolVar(&showValues, "show-values", false, "Reveal variable values instead of names only")
	return cmd
}

func newOrgEnvSetCmd(state *AppState) *cobra.Command {
	var (
		variables   []string
		environment string
		secret      bool
	)

	cmd := &cobra.Command{
		Use:   "set",
		Short: "Set organization variables",
		Long: `Set organization variables.

Each variable is written on its own; the others are left alone. Use
--environment/-e to target a named environment, which has to exist already.

Pass --secret to encrypt the values at rest. A read never returns a secret, and
execution results, progress events and flow exports replace it with ` + "`***`" + `. A
plain variable can become a secret; the reverse is refused, so delete it and set
it again.

Examples:
  echopoint org env set --var KEY=value
  echopoint org env set --var KEY1=v1 --var KEY2=v2
  echopoint org env set -e prd --var BASE_URL=https://api.example.com
  echopoint org env set --secret --var API_KEY=sk-live-...`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			if err := requireOrg(state); err != nil {
				return err
			}
			if len(variables) == 0 {
				return fmt.Errorf("at least one --var KEY=value is required")
			}

			updates, err := parseVarFlags(variables)
			if err != nil {
				return err
			}

			for _, key := range sortedKeys(updates) {
				if err := setOrgVariable(state, environment, key, updates[key], secret); err != nil {
					return err
				}
			}

			kind := "variable(s)"
			if secret {
				kind = "secret(s)"
			}
			fmt.Printf("Set %d %s in the %s\n", len(updates), kind, layerTarget(environment))
			return nil
		},
	}

	cmd.Flags().StringArrayVar(&variables, "var", nil, "Variable in KEY=value format (repeatable)")
	cmd.Flags().StringVarP(&environment, "environment", "e", "", "Target a named environment instead of the base layer")
	cmd.Flags().BoolVar(&secret, "secret", false, "Store the values as secrets (encrypted, never read back)")
	return cmd
}

func newOrgEnvUnsetCmd(state *AppState) *cobra.Command {
	var environment string

	cmd := &cobra.Command{
		Use:   "unset KEY [KEY...]",
		Short: "Remove organization variables",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			if err := requireOrg(state); err != nil {
				return err
			}

			for _, key := range args {
				if err := deleteOrgVariable(state, environment, key); err != nil {
					return err
				}
			}

			fmt.Printf("Removed %d variable(s) from the %s\n", len(args), layerTarget(environment))
			return nil
		},
	}

	cmd.Flags().StringVarP(&environment, "environment", "e", "", "Target a named environment instead of the base layer")
	return cmd
}

func newOrgEnvImportCmd(state *AppState) *cobra.Command {
	var (
		file        string
		environment string
		secret      bool
	)

	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import organization variables from a file",
		Long: `Import variables from a JSON object ({"KEY":"value"}) or a dotenv (KEY=value) file.

Each variable is written on its own; the others are left alone. Pass --secret to
store every imported value as a secret.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			if err := requireOrg(state); err != nil {
				return err
			}
			if file == "" {
				return fmt.Errorf("--file is required")
			}

			updates, err := parseVarFile(file)
			if err != nil {
				return err
			}
			if len(updates) == 0 {
				return fmt.Errorf("no variables found in %s", file)
			}

			for _, key := range sortedKeys(updates) {
				if err := setOrgVariable(state, environment, key, updates[key], secret); err != nil {
					return err
				}
			}

			fmt.Printf(
				"Imported %d variable(s) into the %s from %s\n",
				len(updates), layerTarget(environment), file,
			)
			return nil
		},
	}

	cmd.Flags().StringVar(&file, "file", "", "Path to a JSON or dotenv file")
	cmd.Flags().StringVarP(&environment, "environment", "e", "", "Target a named environment instead of the base layer")
	cmd.Flags().BoolVar(&secret, "secret", false, "Store the imported values as secrets")
	return cmd
}

func newOrgEnvDeleteCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   deleteVerb,
		Short: "Delete the entire organization variable set",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			if err := requireOrg(state); err != nil {
				return err
			}

			resp, err := state.Client.API().DeleteOrganizationVariablesWithResponse(
				context.Background(), nil,
			)
			if err != nil {
				return fmt.Errorf("failed to delete organization variables: %w", err)
			}
			if resp.StatusCode() != http.StatusNoContent {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}

			fmt.Println("Deleted the organization variable set")
			return nil
		},
	}
	return cmd
}

func newOrgEnvironmentsCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:     environmentsVerb,
		Aliases: []string{"env"},
		Short:   "Manage named organization environments",
	}
	cmd.AddCommand(
		newOrgEnvironmentsListCmd(state),
		newOrgEnvironmentsCreateCmd(state),
		newOrgEnvironmentsDeleteCmd(state),
	)
	return cmd
}

func newOrgEnvironmentsListCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   listVerb,
		Short: "List named environments",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			if err := requireOrg(state); err != nil {
				return err
			}

			resp, err := state.Client.API().ListEnvironmentsWithResponse(context.Background(), nil)
			if err != nil {
				return fmt.Errorf("failed to list environments: %w", err)
			}
			if resp.JSON200 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}

			names := make([]string, 0, len(resp.JSON200.Items))
			for _, environment := range resp.JSON200.Items {
				names = append(names, environment.Name)
			}
			sort.Strings(names)

			switch state.OutputFormat {
			case output.FormatJSON:
				return output.PrintJSON(os.Stdout, names)
			case output.FormatYAML:
				return output.PrintYAML(os.Stdout, names)
			}

			if len(names) == 0 {
				fmt.Println("No environments")
				return nil
			}
			for _, name := range names {
				fmt.Printf("  %s\n", name)
			}
			return nil
		},
	}
}

func newOrgEnvironmentsCreateCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   createVerb + " <name>",
		Short: "Create a named environment",
		Long: `Create a named environment such as dev or prd.

It starts empty, and stays empty until a variable is written into it.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			if err := requireOrg(state); err != nil {
				return err
			}

			resp, err := state.Client.API().CreateEnvironmentWithResponse(
				context.Background(), nil,
				api.CreateEnvironmentJSONRequestBody{Name: args[0]},
			)
			if err != nil {
				return fmt.Errorf("failed to create environment %q: %w", args[0], err)
			}
			if resp.JSON201 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}

			fmt.Printf("Created environment %q\n", resp.JSON201.Name)
			return nil
		},
	}
}

func newOrgEnvironmentsDeleteCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   deleteVerb + " <name>",
		Short: "Delete a named environment and every variable in it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			if err := requireOrg(state); err != nil {
				return err
			}

			resp, err := state.Client.API().DeleteEnvironmentWithResponse(
				context.Background(), args[0], nil,
			)
			if err != nil {
				return fmt.Errorf("failed to delete environment %q: %w", args[0], err)
			}
			if resp.StatusCode() != http.StatusNoContent {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}

			fmt.Printf("Deleted environment %q\n", args[0])
			return nil
		},
	}
}

// parseVarFlags turns repeated KEY=value flags into a map.
func parseVarFlags(flags []string) (map[string]string, error) {
	out := make(map[string]string, len(flags))
	for _, v := range flags {
		parts := strings.SplitN(v, "=", 2)
		if len(parts) != 2 || parts[0] == "" {
			return nil, fmt.Errorf("invalid variable format: %s (expected KEY=value)", v)
		}
		out[parts[0]] = parts[1]
	}
	return out, nil
}

// parseVarFile reads variables from a JSON object or dotenv file.
func parseVarFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}

	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "{") {
		out := make(map[string]string)
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, fmt.Errorf("failed to parse JSON %s: %w", path, err)
		}
		return out, nil
	}

	// dotenv: KEY=value per line, # comments and blanks ignored.
	out := make(map[string]string)
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid line in %s: %q (expected KEY=value)", path, line)
		}
		out[strings.TrimSpace(parts[0])] = strings.Trim(strings.TrimSpace(parts[1]), `"'`)
	}
	return out, nil
}
