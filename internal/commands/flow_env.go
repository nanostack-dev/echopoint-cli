package commands

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/output"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// newFlowEnvCmd creates the env subcommand for flows
func newFlowEnvCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Manage flow environment variables",
	}

	cmd.AddCommand(
		newFlowEnvGetCmd(state),
		newFlowEnvSetCmd(state),
		newFlowEnvUnsetCmd(state),
		newFlowEnvDeleteCmd(state),
	)

	return cmd
}

// newFlowEnvUnsetCmd removes specific environment variables from a flow.
func newFlowEnvUnsetCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   "unset <flow-id> KEY [KEY...]",
		Short: "Remove flow environment variables",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			flowID, err := uuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid flow ID: %w", err)
			}

			getResp, err := state.Client.API().GetFlowEnvironmentWithResponse(context.Background(), flowID, nil)
			if err != nil {
				return fmt.Errorf("failed to get environment: %w", err)
			}
			if getResp.JSON200 == nil {
				return formatAPIError(getResp.HTTPResponse, getResp.Body)
			}

			vars := make(map[string]string, len(getResp.JSON200.Variables))
			for k, v := range getResp.JSON200.Variables {
				vars[k] = v.Value
			}

			removed := 0
			for _, k := range args[1:] {
				if _, ok := vars[k]; ok {
					delete(vars, k)
					removed++
				}
			}
			if removed == 0 {
				fmt.Println("No matching variables to remove")
				return nil
			}

			req := api.CreateFlowEnvironmentRequest{Variables: vars}
			resp, err := state.Client.API().
				CreateOrUpdateFlowEnvironmentWithResponse(context.Background(), flowID, nil, req)
			if err != nil {
				return fmt.Errorf("failed to update environment: %w", err)
			}
			if resp.JSON200 == nil && resp.JSON201 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}

			fmt.Printf("✓ Removed %d variable(s)\n", removed)
			return nil
		},
	}
}

// newFlowEnvGetCmd gets environment variables for a flow
func newFlowEnvGetCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   "get <flow-id>",
		Short: "Get flow environment variables",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			flowID, err := uuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid flow ID: %w", err)
			}

			resp, err := state.Client.API().GetFlowEnvironmentWithResponse(context.Background(), flowID, nil)
			if err != nil {
				return fmt.Errorf("failed to get environment: %w", err)
			}
			if resp.JSON200 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}

			env := resp.JSON200

			switch state.OutputFormat {
			case output.FormatJSON:
				return output.PrintJSON(os.Stdout, env)
			case output.FormatYAML:
				return output.PrintYAML(os.Stdout, env)
			default:
				if len(env.Variables) == 0 {
					fmt.Println("No environment variables set")
					return nil
				}

				fmt.Printf("Environment variables for flow %s:\n\n", flowID)
				for key, val := range env.Variables {
					fmt.Printf("  %s=%s\n", key, val.Value)
				}
				return nil
			}
		},
	}
}

// newFlowEnvSetCmd sets environment variables for a flow
func newFlowEnvSetCmd(state *AppState) *cobra.Command {
	var variables []string

	cmd := &cobra.Command{
		Use:   "set <flow-id>",
		Short: "Set flow environment variables",
		Args:  cobra.ExactArgs(1),
		Long: `Set environment variables for a flow.

Examples:
  # Set single variable
  echopoint flows env set <flow-id> --var KEY=value

  # Set multiple variables
  echopoint flows env set <flow-id> --var KEY1=value1 --var KEY2=value2

  # Set from JSON file
  echopoint flows env set <flow-id> --file env.json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			flowID, err := uuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid flow ID: %w", err)
			}

			updates, err := parseVarFlags(variables)
			if err != nil {
				return err
			}
			if len(updates) == 0 {
				return fmt.Errorf("no variables provided. Use --var KEY=value")
			}

			// Merge into existing variables (read-modify-write): the API replaces
			// the whole set, so fetch current state and apply only the changes.
			getResp, err := state.Client.API().GetFlowEnvironmentWithResponse(context.Background(), flowID, nil)
			if err != nil {
				return fmt.Errorf("failed to get environment: %w", err)
			}
			if getResp.JSON200 == nil {
				return formatAPIError(getResp.HTTPResponse, getResp.Body)
			}

			vars := make(map[string]string, len(getResp.JSON200.Variables)+len(updates))
			for k, v := range getResp.JSON200.Variables {
				vars[k] = v.Value
			}
			for k, v := range updates {
				vars[k] = v
			}

			req := api.CreateFlowEnvironmentRequest{
				Variables: vars,
			}

			resp, err := state.Client.API().
				CreateOrUpdateFlowEnvironmentWithResponse(context.Background(), flowID, nil, req)
			if err != nil {
				return fmt.Errorf("failed to set environment: %w", err)
			}
			if resp.JSON200 == nil && resp.JSON201 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}

			fmt.Printf("✓ Set %d variable(s)\n", len(updates))
			for key := range updates {
				fmt.Printf("  %s\n", key)
			}

			return nil
		},
	}

	cmd.Flags().
		StringArrayVar(&variables, "var", []string{}, "Environment variable in KEY=value format (can be used multiple times)")

	return cmd
}

// newFlowEnvDeleteCmd deletes environment variables for a flow
func newFlowEnvDeleteCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <flow-id>",
		Short: "Delete all flow environment variables",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			flowID, err := uuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid flow ID: %w", err)
			}

			resp, err := state.Client.API().DeleteFlowEnvironmentWithResponse(context.Background(), flowID, nil)
			if err != nil {
				return fmt.Errorf("failed to delete environment: %w", err)
			}
			if resp.HTTPResponse.StatusCode != http.StatusNoContent {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}

			fmt.Println("✓ Environment variables deleted")

			return nil
		},
	}
}
