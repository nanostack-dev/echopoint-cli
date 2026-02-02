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

func newFlowsCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "flows",
		Short: "Manage flows",
	}

	cmd.AddCommand(
		newFlowsListCmd(state),
		newFlowsGetCmd(state),
		newFlowsCreateCmd(state),
		newFlowsUpdateCmd(state),
		newFlowsDeleteCmd(state),
		newFlowInteractiveCmd(state),
		newFlowShowCmd(state),
		newFlowNodeCmd(state),
		newFlowEdgeCmd(state),
		newFlowEnvCmd(state),
	)

	return cmd
}

func newFlowsListCmd(state *AppState) *cobra.Command {
	var limit int32 = 20
	var offset int32

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List flows",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			params := &api.ListFlowsParams{
				Limit:  api.LimitParameter(limit),
				Offset: api.OffsetParameter(offset),
			}

			resp, err := state.Client.API().ListFlowsWithResponse(context.Background(), params)
			if err != nil {
				return err
			}

			if resp.JSON200 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}

			switch state.OutputFormat {
			case output.FormatJSON:
				return output.PrintJSON(os.Stdout, resp.JSON200)
			case output.FormatYAML:
				return output.PrintYAML(os.Stdout, resp.JSON200)
			default:
				rows := make([][]string, 0, len(resp.JSON200.Items))
				for _, flow := range resp.JSON200.Items {
					rows = append(rows, []string{flow.Id.String(), flow.Name, flow.UpdatedAt.String()})
				}
				fmt.Fprintf(os.Stdout, "Total: %d\n", resp.JSON200.Total)
				return output.PrintTable([]string{"ID", "Name", "Updated"}, rows)
			}
		},
	}

	cmd.Flags().Int32Var(&limit, "limit", 20, "Number of results to return")
	cmd.Flags().Int32Var(&offset, "offset", 0, "Offset for pagination")

	return cmd
}

func newFlowsGetCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get flow details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			id, err := uuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid flow id")
			}

			resp, err := state.Client.API().GetFlowWithResponse(context.Background(), id)
			if err != nil {
				return err
			}

			if resp.JSON200 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}

			switch state.OutputFormat {
			case output.FormatJSON:
				return output.PrintJSON(os.Stdout, resp.JSON200)
			case output.FormatYAML:
				return output.PrintYAML(os.Stdout, resp.JSON200)
			default:
				fmt.Fprintf(os.Stdout, "ID: %s\n", resp.JSON200.Id)
				fmt.Fprintf(os.Stdout, "Name: %s\n", resp.JSON200.Name)
				fmt.Fprintf(os.Stdout, "Updated: %s\n", resp.JSON200.UpdatedAt)
				fmt.Fprintf(os.Stdout, "Created: %s\n", resp.JSON200.CreatedAt)
				return nil
			}
		},
	}

	return cmd
}

func newFlowsCreateCmd(state *AppState) *cobra.Command {
	var file string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a flow from JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			if file == "" {
				return fmt.Errorf("--file is required")
			}

			var req api.CreateFlowRequest
			if err := loadJSONFile(file, &req); err != nil {
				return err
			}

			resp, err := state.Client.API().CreateFlowWithResponse(context.Background(), req)
			if err != nil {
				return fmt.Errorf("request failed: %w", err)
			}

			// Debug output
			if state.Debug {
				fmt.Fprintf(os.Stderr, "[DEBUG] Response Status: %d\n", resp.StatusCode())
				fmt.Fprintf(os.Stderr, "[DEBUG] Response Body: %s\n", string(resp.Body))
			}

			if resp.JSON201 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}

			switch state.OutputFormat {
			case output.FormatJSON:
				return output.PrintJSON(os.Stdout, resp.JSON201)
			case output.FormatYAML:
				return output.PrintYAML(os.Stdout, resp.JSON201)
			default:
				fmt.Fprintf(os.Stdout, "ID: %s\n", resp.JSON201.Id)
				fmt.Fprintf(os.Stdout, "Name: %s\n", resp.JSON201.Name)
				return nil
			}
		},
	}

	cmd.Flags().StringVar(&file, "file", "", "Path to CreateFlowRequest JSON")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func newFlowsUpdateCmd(state *AppState) *cobra.Command {
	var file string

	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a flow from JSON",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			if file == "" {
				return fmt.Errorf("--file is required")
			}

			id, err := uuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid flow id")
			}

			var req api.UpdateFlowRequest
			if err := loadJSONFile(file, &req); err != nil {
				return err
			}

			resp, err := state.Client.API().UpdateFlowWithResponse(context.Background(), id, req)
			if err != nil {
				return err
			}
			if resp.JSON200 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}

			switch state.OutputFormat {
			case output.FormatJSON:
				return output.PrintJSON(os.Stdout, resp.JSON200)
			case output.FormatYAML:
				return output.PrintYAML(os.Stdout, resp.JSON200)
			default:
				fmt.Fprintf(os.Stdout, "ID: %s\n", resp.JSON200.Id)
				fmt.Fprintf(os.Stdout, "Name: %s\n", resp.JSON200.Name)
				return nil
			}
		},
	}

	cmd.Flags().StringVar(&file, "file", "", "Path to UpdateFlowRequest JSON")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func newFlowsDeleteCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a flow",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			id, err := uuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid flow id")
			}

			resp, err := state.Client.API().DeleteFlowWithResponse(context.Background(), id)
			if err != nil {
				return err
			}
			if resp.HTTPResponse.StatusCode != http.StatusNoContent {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}

			fmt.Fprintln(os.Stdout, "Flow deleted.")
			return nil
		},
	}

	return cmd
}
