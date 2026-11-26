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

	"github.com/spf13/cobra"
)

// secretPlaceholder stands where a secret's value would be printed. A read
// never returns one, so there is nothing else to show.
const secretPlaceholder = "<secret>"

// newOrgCmd creates the top-level "org" command group: the organization's own
// settings and its organization-scoped resources.
func newOrgCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "org",
		Short: "Manage organization-scoped resources",
	}
	cmd.AddCommand(newOrgViewCmd(state), newOrgRenameCmd(state), newOrgEnvCmd(state))
	return cmd
}

// orgScope is the organization's variable set and its shared environments.
var orgScope = variableScope{
	noun:    "organization",
	command: "org env",
	read: func(ctx context.Context, state *AppState) (*api.VariableSet, error) {
		resp, err := state.Client.API().GetOrganizationVariablesWithResponse(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to get organization variables: %w", err)
		}
		if resp.JSON200 == nil {
			return nil, formatAPIError(resp.HTTPResponse, resp.Body)
		}
		return resp.JSON200, nil
	},
	write: func(ctx context.Context, state *AppState, environment, key string, body api.SetVariableRequest) error {
		if environment == "" {
			resp, err := state.Client.API().SetOrganizationVariableWithResponse(ctx, key, nil, body)
			if err != nil {
				return fmt.Errorf("failed to set %s: %w", key, err)
			}
			return expectStatus(resp.HTTPResponse, resp.Body, http.StatusOK)
		}
		resp, err := state.Client.API().SetEnvironmentVariableWithResponse(ctx, environment, key, nil, body)
		if err != nil {
			return fmt.Errorf("failed to set %s in %s: %w", key, environment, err)
		}
		return expectStatus(resp.HTTPResponse, resp.Body, http.StatusOK)
	},
	remove: func(ctx context.Context, state *AppState, environment, key string) error {
		if environment == "" {
			resp, err := state.Client.API().DeleteOrganizationVariableWithResponse(ctx, key, nil)
			if err != nil {
				return fmt.Errorf("failed to delete %s: %w", key, err)
			}
			return expectStatus(resp.HTTPResponse, resp.Body, http.StatusNoContent)
		}
		resp, err := state.Client.API().DeleteEnvironmentVariableWithResponse(ctx, environment, key, nil)
		if err != nil {
			return fmt.Errorf("failed to delete %s from %s: %w", key, environment, err)
		}
		return expectStatus(resp.HTTPResponse, resp.Body, http.StatusNoContent)
	},
	search: searchEnvironments,
	createEnvironment: func(ctx context.Context, state *AppState, name string) (*api.Environment, error) {
		resp, err := state.Client.API().
			CreateEnvironmentWithResponse(ctx, nil, api.CreateEnvironmentJSONRequestBody{Name: name})
		if err != nil {
			return nil, fmt.Errorf("failed to create environment %q: %w", name, err)
		}
		if resp.JSON201 == nil {
			return nil, formatAPIError(resp.HTTPResponse, resp.Body)
		}
		return resp.JSON201, nil
	},
	deleteEnvironment: func(ctx context.Context, state *AppState, name string) error {
		resp, err := state.Client.API().DeleteEnvironmentWithResponse(ctx, name, nil)
		if err != nil {
			return fmt.Errorf("failed to delete environment %q: %w", name, err)
		}
		return expectStatus(resp.HTTPResponse, resp.Body, http.StatusNoContent)
	},
	environmentsLong: `Manage the organization's shared environments.

Every workspace sees a shared environment and can hold its own values for it.
A workspace can also have environments of its own ('workspace env environments');
they are not listed here.`,
	deleteEnvironmentShort: "Delete a shared environment and the organization's values in it",
	deleteEnvironmentLong: `Delete a shared environment and the organization's values in it.

The base layer is untouched. A workspace that still holds values for the
environment blocks the delete with ENVIRONMENT_IN_USE: those workspaces delete
their values first ('workspace env unset -e <name>').`,
}

func newOrgEnvCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   envCommandName,
		Short: "Manage organization variables",
		Long: `Manage organization variables.

A variable set holds two kinds of layer:
  - base variables, loaded for every flow execution
  - named environments (e.g. dev, staging, prd) selected at launch time

The organization's environments are shared: every workspace sees them. A
workspace's own values override the organization's ('workspace env').

A variable can be stored as a secret. A secret is encrypted at rest, is never
returned by a read, and is replaced by ` + "`***`" + ` in execution results, progress
events and flow exports. A plain variable can become a secret; the reverse is
refused, so delete it and set it again.`,
	}
	cmd.AddCommand(
		orgScope.newViewCmd(state),
		orgScope.newSetCmd(state),
		orgScope.newUnsetCmd(state),
		orgScope.newImportCmd(state),
		newOrgEnvDeleteCmd(state),
		orgScope.newEnvironmentsCmd(state),
	)
	return cmd
}

// requireOrg fails before any call that needs an organization context.
func requireOrg(state *AppState) error {
	if strings.TrimSpace(state.OrganizationID) == "" {
		return fmt.Errorf(
			"organization context required: set --org, ECHOPOINT_ORGANIZATION_ID, or log in with a default organization",
		)
	}
	return nil
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

func newOrgEnvDeleteCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   deleteVerb,
		Short: "Delete every organization variable",
		Long: `Delete every organization variable: the base layer and the organization's
values in every environment.

The shared environments stay, empty, and so do the values workspaces hold for
them. Delete an environment with 'org env environments delete'.`,
		Example: `  echopoint org env delete
  echopoint org env delete --yes`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := orgScope.require(state); err != nil {
				return err
			}
			if err := confirmDestructive(cmd, state, "delete", "every organization variable"); err != nil {
				return err
			}

			resp, err := state.Client.API().DeleteOrganizationVariablesWithResponse(cmd.Context(), nil)
			if err != nil {
				return fmt.Errorf("failed to delete organization variables: %w", err)
			}
			if err := expectStatus(resp.HTTPResponse, resp.Body, http.StatusNoContent); err != nil {
				return err
			}

			fmt.Println("Deleted every organization variable; the environments stay")
			return nil
		},
	}
	return quietOnError(cmd)
}

// searchEnvironments reads one page of the organization's shared environments.
func searchEnvironments(state *AppState) pageFetcher[api.Environment] {
	return func(ctx context.Context, pagination *api.PaginationRequest) (searchPage[api.Environment], error) {
		resp, err := state.Client.API().SearchEnvironmentsWithResponse(
			ctx, nil, api.EnvironmentSearchRequest{Pagination: pagination},
		)
		if err != nil {
			return searchPage[api.Environment]{}, err
		}
		if resp.JSON200 == nil {
			return searchPage[api.Environment]{}, formatAPIError(resp.HTTPResponse, resp.Body)
		}
		return newSearchPage(resp.JSON200.Items, resp.JSON200.Total, resp.JSON200.NextCursor), nil
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
