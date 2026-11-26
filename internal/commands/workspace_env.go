package commands

import (
	"context"
	"fmt"
	"net/http"

	"echopoint-cli/internal/api"

	"github.com/spf13/cobra"
)

// workspaceScope is the selected workspace's variable set and the
// environments it sees: the shared ones and its own (Echopoint ADR-0043,
// ADR-0044). --workspace selects the workspace; without it the API uses the
// organization's oldest workspace.
var workspaceScope = variableScope{
	noun:      "workspace",
	command:   "workspace env",
	showScope: true,
	read: func(ctx context.Context, state *AppState) (*api.VariableSet, error) {
		resp, err := state.Client.API().GetWorkspaceVariablesWithResponse(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to get workspace variables: %w", err)
		}
		if resp.JSON200 == nil {
			return nil, formatAPIError(resp.HTTPResponse, resp.Body)
		}
		return resp.JSON200, nil
	},
	write: func(ctx context.Context, state *AppState, environment, key string, body api.SetVariableRequest) error {
		if environment == "" {
			resp, err := state.Client.API().SetWorkspaceVariableWithResponse(ctx, key, nil, body)
			if err != nil {
				return fmt.Errorf("failed to set %s: %w", key, err)
			}
			return expectStatus(resp.HTTPResponse, resp.Body, http.StatusOK)
		}
		resp, err := state.Client.API().SetWorkspaceEnvironmentVariableWithResponse(ctx, environment, key, nil, body)
		if err != nil {
			return fmt.Errorf("failed to set %s in %s: %w", key, environment, err)
		}
		return expectStatus(resp.HTTPResponse, resp.Body, http.StatusOK)
	},
	remove: func(ctx context.Context, state *AppState, environment, key string) error {
		if environment == "" {
			resp, err := state.Client.API().DeleteWorkspaceVariableWithResponse(ctx, key, nil)
			if err != nil {
				return fmt.Errorf("failed to delete %s: %w", key, err)
			}
			return expectStatus(resp.HTTPResponse, resp.Body, http.StatusNoContent)
		}
		resp, err := state.Client.API().DeleteWorkspaceEnvironmentVariableWithResponse(ctx, environment, key, nil)
		if err != nil {
			return fmt.Errorf("failed to delete %s from %s: %w", key, environment, err)
		}
		return expectStatus(resp.HTTPResponse, resp.Body, http.StatusNoContent)
	},
	search: searchWorkspaceEnvironments,
	createEnvironment: func(ctx context.Context, state *AppState, name string) (*api.Environment, error) {
		resp, err := state.Client.API().CreateWorkspaceEnvironmentWithResponse(
			ctx, nil, api.CreateWorkspaceEnvironmentJSONRequestBody{Name: name},
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create environment %q: %w", name, err)
		}
		if resp.JSON201 == nil {
			return nil, formatAPIError(resp.HTTPResponse, resp.Body)
		}
		return resp.JSON201, nil
	},
	deleteEnvironment: func(ctx context.Context, state *AppState, name string) error {
		resp, err := state.Client.API().DeleteWorkspaceEnvironmentWithResponse(ctx, name, nil)
		if err != nil {
			return fmt.Errorf("failed to delete environment %q: %w", name, err)
		}
		return expectStatus(resp.HTTPResponse, resp.Body, http.StatusNoContent)
	},
	environmentsLong: `Manage the environments the selected workspace sees.

The list holds the organization's shared environments and the workspace's own
ones, each with its scope. 'create' and 'delete' act on the workspace's own
environments only: a shared environment is managed with 'org env environments'.`,
	deleteEnvironmentShort: "Delete an environment of the workspace and every value in it",
	deleteEnvironmentLong: `Delete an environment of the workspace and every value in it.

The workspace base layer is untouched. A shared environment cannot be deleted
here (404): remove the workspace's values for it with
'workspace env unset -e <name> KEY...'.`,
}

// newWorkspaceEnvCmd manages the selected workspace's variables. They
// override the organization's for the flows of that workspace.
func newWorkspaceEnvCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   envCommandName,
		Short: "Manage workspace variables",
		Long: `Manage the variables of the workspace that --workspace selects (without it,
the organization's oldest workspace).

A workspace variable set holds base variables and values for each environment
the workspace sees: the organization's shared environments and its own. A run
resolves, from lowest to highest: organization base, organization environment,
workspace base, workspace environment, flow variables.

A variable can be stored as a secret. A secret is encrypted at rest, is never
returned by a read, and is replaced by ` + "`***`" + ` in execution results, progress
events and flow exports. A plain variable can become a secret; the reverse is
refused, so delete it and set it again.`,
	}
	cmd.AddCommand(
		workspaceScope.newViewCmd(state),
		workspaceScope.newSetCmd(state),
		workspaceScope.newUnsetCmd(state),
		workspaceScope.newImportCmd(state),
		workspaceScope.newEnvironmentsCmd(state),
	)
	return cmd
}

// searchWorkspaceEnvironments reads one page of the environments the selected
// workspace sees, shared and its own.
func searchWorkspaceEnvironments(state *AppState) pageFetcher[api.Environment] {
	return func(ctx context.Context, pagination *api.PaginationRequest) (searchPage[api.Environment], error) {
		resp, err := state.Client.API().SearchWorkspaceEnvironmentsWithResponse(
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
