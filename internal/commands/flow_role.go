package commands

import (
	"fmt"

	"echopoint-cli/internal/api"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// flowRoleResult is what `flow role` prints, in every output format.
type flowRoleResult struct {
	ID   uuid.UUID    `json:"id"`
	Role api.FlowRole `json:"role"`
}

func newFlowRoleCmd(state *AppState) *cobra.Command {
	var set string

	cmd := &cobra.Command{
		Use:   "role <flow-id>",
		Short: "Show a flow's role, or set it with --set",
		Long: `Show a flow's role, or change it with --set.

A flow is either standalone or a module. A Module flow is a reusable building
block that other flows call through a module node: it can still be launched
directly to test it, but a schedule skips it when its tags match the flow,
unless the schedule sets ignore_modules to false. A Standalone flow is a test of
its own, and is the default.

--set changes only the role; every other field of the flow stays.`,
		Example: `  echopoint flow role <flow-id>
  echopoint flow role <flow-id> -o json
  echopoint flow role <flow-id> --set module
  echopoint flow role <flow-id> --set standalone`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			var role api.FlowRole
			setting := cmd.Flags().Changed("set")
			if setting {
				parsed, err := parseFlowRole(set)
				if err != nil {
					return err
				}
				role = parsed
			}

			id, err := resolveFlowID(cmd.Context(), state, args[0])
			if err != nil {
				return err
			}

			var current api.FlowRole
			if setting {
				current, err = setFlowRole(cmd, state, id, role)
			} else {
				current, err = getFlowRole(cmd, state, id)
			}
			if err != nil {
				return err
			}

			result := flowRoleResult{ID: id, Role: current}
			if printed, err := printStructured(cmd.OutOrStdout(), state.OutputFormat, result); printed {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), current)
			return nil
		},
	}

	cmd.Flags().StringVar(&set, "set", "", "New role: standalone or module")
	_ = cmd.RegisterFlagCompletionFunc("set", staticCompletion(
		string(api.FlowRoleStandalone), string(api.FlowRoleModule)))
	return quietOnError(cmd)
}

func parseFlowRole(value string) (api.FlowRole, error) {
	role := api.FlowRole(value)
	if !role.Valid() {
		return "", fmt.Errorf("invalid role %q; must be %q or %q", value, api.FlowRoleStandalone, api.FlowRoleModule)
	}
	return role, nil
}

func setFlowRole(cmd *cobra.Command, state *AppState, id uuid.UUID, role api.FlowRole) (api.FlowRole, error) {
	resp, err := state.Client.API().UpdateFlowWithResponse(cmd.Context(), id, nil, api.UpdateFlowRequest{Role: &role})
	if err != nil {
		return "", err
	}
	if resp.JSON200 == nil {
		return "", formatAPIError(resp.HTTPResponse, resp.Body)
	}
	return resp.JSON200.Role, nil
}

func getFlowRole(cmd *cobra.Command, state *AppState, id uuid.UUID) (api.FlowRole, error) {
	resp, err := state.Client.API().GetFlowWithResponse(cmd.Context(), id, nil)
	if err != nil {
		return "", err
	}
	if resp.JSON200 == nil {
		return "", formatAPIError(resp.HTTPResponse, resp.Body)
	}
	return resp.JSON200.Role, nil
}
