package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"os"
	"sort"
	"strings"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/output"

	"github.com/spf13/cobra"
)

// newOrgCmd creates the top-level "org" command group for organization-scoped
// resources. Today it hosts environment management; future org-wide resources
// belong here too.
func newOrgCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "org",
		Short: "Manage organization-scoped resources",
	}

	cmd.AddCommand(newOrgEnvCmd(state))

	return cmd
}

// newOrgEnvCmd manages the organization environment: base variables plus named
// overlays (dev/stg/prd) selected at flow launch via --environment.
func newOrgEnvCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Manage organization environment variables",
		Long: `Manage the organization environment.

The organization environment has two layers:
  - base variables, loaded for every flow execution
  - named overlays (e.g. dev, staging, prod) selected at launch time

Base commands operate on the base layer. Target a named overlay with
--environment/-e; setting into a new name creates that overlay.`,
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

// requireOrg ensures an organization context is available before calling
// organization-scoped endpoints, which require the X-Organization-Id header.
func requireOrg(state *AppState) error {
	if strings.TrimSpace(state.OrganizationID) == "" {
		return fmt.Errorf(
			"organization context required: set --organization-id, ECHOPOINT_ORGANIZATION_ID, or log in with a default organization",
		)
	}
	return nil
}

// fetchOrgEnv loads the current organization environment.
func fetchOrgEnv(state *AppState) (*api.Environment, error) {
	resp, err := state.Client.API().GetOrganizationEnvironmentWithResponse(context.Background(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get organization environment: %w", err)
	}
	if resp.JSON200 == nil {
		return nil, formatAPIError(resp.HTTPResponse, resp.Body)
	}
	return resp.JSON200, nil
}

// orgEnvToInputs flattens an Environment response into mutable string maps for
// read-modify-write upserts (the API has no per-key endpoint).
func orgEnvToInputs(env *api.Environment) (map[string]string, map[string]map[string]string) {
	base := make(map[string]string)
	if env != nil {
		for k, v := range env.Variables {
			base[k] = v.Value
		}
	}

	overlays := make(map[string]map[string]string)
	if env != nil && env.Environments != nil {
		for name, set := range env.Environments {
			vars := make(map[string]string, len(set))
			for k, v := range set {
				vars[k] = v.Value
			}
			overlays[name] = vars
		}
	}

	return base, overlays
}

// upsertOrgEnv sends the merged maps back to the API.
func upsertOrgEnv(state *AppState, base map[string]string, overlays map[string]map[string]string) error {
	varsInput := api.EnvironmentVariablesInput(base)

	envInput := make(api.NamedEnvironmentVariablesInput, len(overlays))
	for name, vars := range overlays {
		envInput[name] = api.EnvironmentVariablesInput(vars)
	}

	req := api.CreateOrUpdateOrganizationEnvironmentJSONRequestBody{
		Variables:    &varsInput,
		Environments: &envInput,
	}

	resp, err := state.Client.API().CreateOrUpdateOrganizationEnvironmentWithResponse(context.Background(), nil, req)
	if err != nil {
		return fmt.Errorf("failed to update organization environment: %w", err)
	}
	if resp.JSON200 == nil && resp.JSON201 == nil {
		return formatAPIError(resp.HTTPResponse, resp.Body)
	}
	return nil
}

// printVars renders a flat variable map for table output, sorted by key.
func printVars(title string, vars map[string]string) {
	if len(vars) == 0 {
		fmt.Println("No environment variables set")
		return
	}
	fmt.Printf("%s:\n\n", title)
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("  %s=%s\n", k, vars[k])
	}
}

func newOrgEnvGetCmd(state *AppState) *cobra.Command {
	var environment string

	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get organization environment variables",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			if err := requireOrg(state); err != nil {
				return err
			}

			env, err := fetchOrgEnv(state)
			if err != nil {
				return err
			}

			base, overlays := orgEnvToInputs(env)

			if environment != "" {
				vars, ok := overlays[environment]
				if !ok {
					return fmt.Errorf("named environment %q not found", environment)
				}
				switch state.OutputFormat {
				case output.FormatJSON:
					return output.PrintJSON(os.Stdout, vars)
				case output.FormatYAML:
					return output.PrintYAML(os.Stdout, vars)
				}
				printVars(fmt.Sprintf("Organization environment %q variables", environment), vars)
				return nil
			}

			switch state.OutputFormat {
			case output.FormatJSON:
				return output.PrintJSON(os.Stdout, env)
			case output.FormatYAML:
				return output.PrintYAML(os.Stdout, env)
			}

			printVars("Organization base variables", base)
			if len(overlays) > 0 {
				names := make([]string, 0, len(overlays))
				for name := range overlays {
					names = append(names, name)
				}
				sort.Strings(names)
				fmt.Printf("\nNamed environments: %s\n", strings.Join(names, ", "))
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&environment, "environment", "e", "", "Target a named overlay instead of the base layer")
	return cmd
}

func newOrgEnvSetCmd(state *AppState) *cobra.Command {
	var (
		variables   []string
		environment string
	)

	cmd := &cobra.Command{
		Use:   "set",
		Short: "Set organization environment variables",
		Long: `Set organization environment variables.

Existing variables are preserved; only the provided keys are added or updated
(read-modify-write). Use --environment/-e to target a named overlay; a new name
creates the overlay.

Examples:
  echopoint org env set --var KEY=value
  echopoint org env set --var KEY1=v1 --var KEY2=v2
  echopoint org env set -e prod --var BASE_URL=https://api.example.com`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			if err := requireOrg(state); err != nil {
				return err
			}

			updates, err := parseVarFlags(variables)
			if err != nil {
				return err
			}
			if len(updates) == 0 {
				return fmt.Errorf("no variables provided. Use --var KEY=value")
			}

			env, err := fetchOrgEnv(state)
			if err != nil {
				return err
			}
			base, overlays := orgEnvToInputs(env)

			target := base
			if environment != "" {
				if overlays[environment] == nil {
					overlays[environment] = make(map[string]string)
				}
				target = overlays[environment]
			}
			maps.Copy(target, updates)

			if err := upsertOrgEnv(state, base, overlays); err != nil {
				return err
			}

			scope := "organization base"
			if environment != "" {
				scope = fmt.Sprintf("environment %q", environment)
			}
			fmt.Printf("✓ Set %d variable(s) on %s\n", len(updates), scope)
			for k := range updates {
				fmt.Printf("  %s\n", k)
			}
			return nil
		},
	}

	cmd.Flags().StringArrayVar(&variables, "var", nil, "Environment variable in KEY=value format (repeatable)")
	cmd.Flags().StringVarP(&environment, "environment", "e", "", "Target a named overlay instead of the base layer")
	return cmd
}

func newOrgEnvUnsetCmd(state *AppState) *cobra.Command {
	var environment string

	cmd := &cobra.Command{
		Use:   "unset KEY [KEY...]",
		Short: "Remove organization environment variables",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			if err := requireOrg(state); err != nil {
				return err
			}

			env, err := fetchOrgEnv(state)
			if err != nil {
				return err
			}
			base, overlays := orgEnvToInputs(env)

			target := base
			if environment != "" {
				target = overlays[environment]
				if target == nil {
					return fmt.Errorf("named environment %q not found", environment)
				}
			}

			removed := 0
			for _, k := range args {
				if _, ok := target[k]; ok {
					delete(target, k)
					removed++
				}
			}
			if removed == 0 {
				fmt.Println("No matching variables to remove")
				return nil
			}

			if err := upsertOrgEnv(state, base, overlays); err != nil {
				return err
			}
			fmt.Printf("✓ Removed %d variable(s)\n", removed)
			return nil
		},
	}

	cmd.Flags().StringVarP(&environment, "environment", "e", "", "Target a named overlay instead of the base layer")
	return cmd
}

func newOrgEnvImportCmd(state *AppState) *cobra.Command {
	var (
		file        string
		environment string
	)

	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import organization environment variables from a file",
		Long: `Import variables from a JSON object ({"KEY":"value"}) or a dotenv (KEY=value) file.

Imported keys are merged into the target layer (existing keys are preserved
unless overwritten).`,
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

			env, err := fetchOrgEnv(state)
			if err != nil {
				return err
			}
			base, overlays := orgEnvToInputs(env)

			target := base
			if environment != "" {
				if overlays[environment] == nil {
					overlays[environment] = make(map[string]string)
				}
				target = overlays[environment]
			}
			maps.Copy(target, updates)

			if err := upsertOrgEnv(state, base, overlays); err != nil {
				return err
			}
			fmt.Printf("✓ Imported %d variable(s) from %s\n", len(updates), file)
			return nil
		},
	}

	cmd.Flags().StringVar(&file, "file", "", "Path to a JSON or dotenv file")
	cmd.Flags().StringVarP(&environment, "environment", "e", "", "Target a named overlay instead of the base layer")
	return cmd
}

func newOrgEnvDeleteCmd(state *AppState) *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete the entire organization environment",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			if err := requireOrg(state); err != nil {
				return err
			}

			if !yes {
				return fmt.Errorf("this deletes ALL org variables and overlays; re-run with --yes")
			}

			resp, err := state.Client.API().DeleteOrganizationEnvironmentWithResponse(context.Background(), nil)
			if err != nil {
				return fmt.Errorf("failed to delete organization environment: %w", err)
			}
			if resp.HTTPResponse.StatusCode != http.StatusNoContent {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}
			fmt.Println("✓ Organization environment deleted")
			return nil
		},
	}

	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation")
	return cmd
}

// newOrgEnvironmentsCmd manages the named overlays as a set.
func newOrgEnvironmentsCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "environments",
		Aliases: []string{"envs"},
		Short:   "Manage named organization environment overlays",
	}

	cmd.AddCommand(
		newOrgEnvironmentsListCmd(state),
		newOrgEnvironmentsDeleteCmd(state),
	)

	return cmd
}

func newOrgEnvironmentsListCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   listVerb,
		Short: "List named environment overlays",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			if err := requireOrg(state); err != nil {
				return err
			}

			env, err := fetchOrgEnv(state)
			if err != nil {
				return err
			}
			_, overlays := orgEnvToInputs(env)

			names := make([]string, 0, len(overlays))
			for name := range overlays {
				names = append(names, name)
			}
			sort.Strings(names)

			switch state.OutputFormat {
			case output.FormatJSON:
				return output.PrintJSON(os.Stdout, names)
			case output.FormatYAML:
				return output.PrintYAML(os.Stdout, names)
			}

			if len(names) == 0 {
				fmt.Println("No named environments")
				return nil
			}
			for _, name := range names {
				fmt.Printf("  %s (%d variables)\n", name, len(overlays[name]))
			}
			return nil
		},
	}
}

func newOrgEnvironmentsDeleteCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a named environment overlay",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			if err := requireOrg(state); err != nil {
				return err
			}

			name := args[0]
			env, err := fetchOrgEnv(state)
			if err != nil {
				return err
			}
			base, overlays := orgEnvToInputs(env)
			if _, ok := overlays[name]; !ok {
				return fmt.Errorf("named environment %q not found", name)
			}
			delete(overlays, name)

			if err := upsertOrgEnv(state, base, overlays); err != nil {
				return err
			}
			fmt.Printf("✓ Deleted named environment %q\n", name)
			return nil
		},
	}
}

// parseVarFlags turns repeated KEY=value flags into a map.
func parseVarFlags(flags []string) (map[string]string, error) {
	out := make(map[string]string, len(flags))
	for _, v := range flags {
		parts := strings.SplitN(v, "=", 2)
		if len(parts) != 2 {
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
