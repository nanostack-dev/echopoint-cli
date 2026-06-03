package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

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
		newFlowsLaunchCmd(state),
		newFlowsRunCmd(state),
		newFlowsExecutionCmd(state),
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

func newFlowsLaunchCmd(state *AppState) *cobra.Command {
	var runnerType string

	cmd := &cobra.Command{
		Use:   "launch <flow-id>",
		Short: "Launch a flow execution",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			flowID, err := uuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid flow id")
			}

			requestBody, err := launchFlowRequestBody(runnerType)
			if err != nil {
				return err
			}

			resp, err := state.Client.API().LaunchFlowWithBodyWithResponse(
				context.Background(),
				flowID,
				nil,
				"application/json",
				strings.NewReader(string(requestBody)),
			)
			if err != nil {
				return err
			}

			if resp.JSON202 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}

			switch state.OutputFormat {
			case output.FormatJSON:
				return output.PrintJSON(os.Stdout, resp.JSON202)
			case output.FormatYAML:
				return output.PrintYAML(os.Stdout, resp.JSON202)
			default:
				fmt.Fprintf(os.Stdout, "Execution ID: %s\n", resp.JSON202.Execution.Id)
				fmt.Fprintf(os.Stdout, "Status: %s\n", resp.JSON202.Execution.Status)
				if resp.JSON202.Execution.RunnerType != nil {
					fmt.Fprintf(os.Stdout, "Runner Type: %s\n", *resp.JSON202.Execution.RunnerType)
				}
				return nil
			}
		},
	}

	cmd.Flags().StringVar(&runnerType, "runner", "cloud", "Runner backend: cloud or self_hosted")
	return cmd
}

func newFlowsExecutionCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "execution",
		Short: "Inspect flow executions",
	}

	cmd.AddCommand(
		newFlowsExecutionGetCmd(state),
		newFlowsExecutionListCmd(state),
	)

	return cmd
}

func newFlowsExecutionGetCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <flow-id> <execution-id>",
		Short: "Get execution details",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			flowID, err := uuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid flow id")
			}
			executionID, err := uuid.Parse(args[1])
			if err != nil {
				return fmt.Errorf("invalid execution id")
			}

			resp, err := state.Client.API().GetExecutionWithResponse(context.Background(), flowID, executionID, nil)
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
				return printExecutionSummary(*resp.JSON200)
			}
		},
	}

	return cmd
}

func newFlowsExecutionListCmd(state *AppState) *cobra.Command {
	var limit int32 = 20
	var offset int32

	cmd := &cobra.Command{
		Use:   "list <flow-id>",
		Short: "List executions for a flow",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			flowID, err := uuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid flow id")
			}

			resp, err := state.Client.API().
				ListFlowExecutionsWithResponse(context.Background(), flowID, &api.ListFlowExecutionsParams{
					Limit:  api.LimitParameter(limit),
					Offset: api.OffsetParameter(offset),
				})
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
				for _, execution := range resp.JSON200.Items {
					rows = append(rows, []string{
						execution.Id.String(),
						string(execution.Status),
						runnerTypeDisplay(execution.RunnerType),
						execution.StartedAt.Format(time.RFC3339),
					})
				}
				fmt.Fprintf(os.Stdout, "Total: %d\n", resp.JSON200.Total)
				return output.PrintTable([]string{"Execution ID", "Status", "Runner", "Started"}, rows)
			}
		},
	}

	cmd.Flags().Int32Var(&limit, "limit", 20, "Number of results to return")
	cmd.Flags().Int32Var(&offset, "offset", 0, "Offset for pagination")
	return cmd
}

func launchFlowRequestBody(runnerType string) ([]byte, error) {
	normalized := strings.TrimSpace(strings.ToLower(runnerType))
	if normalized == "" {
		normalized = "cloud"
	}
	if normalized != string(api.Cloud) && normalized != string(api.SelfHosted) && normalized != string(api.Ephemeral) {
		return nil, fmt.Errorf("invalid runner type %q", runnerType)
	}

	payload := api.LaunchFlowRequest{RunnerType: runnerTypePtr(api.RunnerType(normalized))}
	return json.Marshal(payload)
}

func runnerTypePtr(value api.RunnerType) *api.RunnerType {
	return &value
}

func runnerTypeDisplay(runnerType *api.RunnerType) string {
	if runnerType == nil {
		return "cloud"
	}
	return string(*runnerType)
}

func printExecutionSummary(execution api.FlowExecution) error {
	fmt.Fprintf(os.Stdout, "Execution ID: %s\n", execution.Id)
	fmt.Fprintf(os.Stdout, "Flow ID: %s\n", execution.FlowId)
	fmt.Fprintf(os.Stdout, "Status: %s\n", execution.Status)
	fmt.Fprintf(os.Stdout, "Runner Type: %s\n", runnerTypeDisplay(execution.RunnerType))
	if execution.EnvironmentKey != nil {
		fmt.Fprintf(os.Stdout, "Environment: %s\n", *execution.EnvironmentKey)
	}
	fmt.Fprintf(os.Stdout, "Started: %s\n", execution.StartedAt.Format(time.RFC3339))
	if execution.CompletedAt != nil {
		fmt.Fprintf(os.Stdout, "Completed: %s\n", execution.CompletedAt.Format(time.RFC3339))
	}
	if execution.ErrorMessage != nil {
		fmt.Fprintf(os.Stdout, "Error: %s\n", *execution.ErrorMessage)
	}
	return nil
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

			resp, err := state.Client.API().GetFlowWithResponse(context.Background(), id, nil)
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

			resp, err := state.Client.API().CreateFlowWithResponse(context.Background(), nil, req)
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

			resp, err := state.Client.API().UpdateFlowWithResponse(context.Background(), id, nil, req)
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

			resp, err := state.Client.API().DeleteFlowWithResponse(context.Background(), id, nil)
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
