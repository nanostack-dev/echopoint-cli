package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/output"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

const flowPluralAlias = "flows"

func newFlowCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:     flowCommandName,
		Aliases: []string{flowPluralAlias},
		Short:   "Create, edit and run flows",
		Long: `Create, edit and run flows.

A <flow-id> is the flow's id, which 'echopoint flow list' shows and Tab completes.

A flow runs on Cloud (EchoPoint runs it), on a Self-hosted runner (a long-lived
runner you operate), or on an Ephemeral runner (a short-lived runner the caller
operates). Two commands start one:
  flow run      this CLI is the Ephemeral runner; it waits and exits with the result
  flow launch   EchoPoint runs it on Cloud or a Self-hosted runner; it returns at once`,
	}

	cmd.AddCommand(
		newFlowListCmd(state),
		newFlowViewCmd(state),
		newFlowCreateCmd(state),
		newFlowUpdateCmd(state),
		newFlowDeleteCmd(state),
		newFlowPublishCmd(state),
		newFlowVersionCmd(state),
		newFlowRunCmd(state),
		newFlowLaunchCmd(state),
		newFlowExecutionCmd(state),
		newFlowTagCmd(state),
		newFlowMoveCmd(state),
		newFlowFolderCmd(state),
		newFlowNodeCmd(state),
		newFlowEdgeCmd(state),
		newFlowEnvCmd(state),
		newFlowValidateCmd(state),
	)
	completeFlowPlaceholders(state, cmd)

	return cmd
}

// completeFlowPlaceholders gives every command whose first argument is a <flow-id>
// the completion of flows, so a command added with that placeholder gets it.
func completeFlowPlaceholders(state *AppState, cmd *cobra.Command) {
	for _, child := range cmd.Commands() {
		completeFlowPlaceholders(state, child)
	}
	if cmd.ValidArgsFunction != nil {
		return
	}
	if words := strings.Fields(cmd.Use); len(words) > 1 && words[1] == "<flow-id>" {
		cmd.ValidArgsFunction = completeFlowArgs(state, 1)
	}
}

func newFlowLaunchCmd(state *AppState) *cobra.Command {
	var runnerType string
	var environmentKey string

	cmd := &cobra.Command{
		Use:   "launch <flow-id>",
		Short: "Ask EchoPoint to run a flow on Cloud or a Self-hosted runner, without waiting",
		Long: `Ask EchoPoint to run a flow, print the execution id, and return without waiting.

The flow runs on Cloud by default, or on a Self-hosted runner with --runner
self_hosted. Follow it with 'echopoint flow execution view'.

To run the flow from this CLI as an Ephemeral runner and wait for the result,
use 'echopoint flow run'.`,
		Example: `  echopoint flow launch <flow-id>
  echopoint flow launch <flow-id> --runner self_hosted -e prd`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			flowID, err := resolveFlowID(context.Background(), state, args[0])
			if err != nil {
				return err
			}

			requestBody, err := launchFlowRequestBody(runnerType, environmentKey)
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
				return output.PrintJSON(cmd.OutOrStdout(), resp.JSON202)
			case output.FormatYAML:
				return output.PrintYAML(cmd.OutOrStdout(), resp.JSON202)
			default:
				fmt.Fprintf(cmd.OutOrStdout(), "Execution ID: %s\n", resp.JSON202.Execution.Id)
				fmt.Fprintf(cmd.OutOrStdout(), "Status: %s\n", resp.JSON202.Execution.Status)
				if resp.JSON202.Execution.RunnerType != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "Runner Type: %s\n", *resp.JSON202.Execution.RunnerType)
				}
				return nil
			}
		},
	}

	cmd.Flags().StringVar(&runnerType, "runner", defaultRunnerType, "Where EchoPoint runs it: cloud or self_hosted")
	cmd.Flags().StringVarP(&environmentKey, "environment", "e", "",
		"Environment to overlay before flow variables (e.g. dev)")
	_ = cmd.RegisterFlagCompletionFunc("runner", staticCompletion(string(api.Cloud), string(api.SelfHosted)))
	registerEnvironmentFlagCompletion(state, cmd)
	return cmd
}

func staticCompletion(values ...string) completionFunc {
	return func(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		candidates := make([]completionCandidate, 0, len(values))
		for _, value := range values {
			candidates = append(candidates, completionCandidate{value: value})
		}
		return formatCompletions(candidates, toComplete), cobra.ShellCompDirectiveNoFileComp
	}
}

func newFlowExecutionCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "execution",
		Short: "Inspect the executions of a flow",
	}

	cmd.AddCommand(
		newFlowExecutionViewCmd(state),
		newFlowExecutionListCmd(state),
	)

	return cmd
}

func newFlowExecutionViewCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "view <flow-id> <execution-id>",
		Aliases: []string{getVerb},
		Short:   "Show one execution of a flow",
		Example: `  echopoint flow execution view <flow-id> <execution-id>
  echopoint flow execution view <flow-id> <execution-id> -o json`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			flowID, err := resolveFlowID(context.Background(), state, args[0])
			if err != nil {
				return err
			}
			executionID, err := parseExecutionID(args[1])
			if err != nil {
				return err
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

	cmd.ValidArgsFunction = completeExecutionArgs(state)
	return cmd
}

func newFlowExecutionListCmd(state *AppState) *cobra.Command {
	var limit int32 = 20
	var offset int32

	cmd := &cobra.Command{
		Use:   "list <flow-id>",
		Short: "List the executions of a flow",
		Example: `  echopoint flow execution list <flow-id>
  echopoint flow execution list <flow-id> --limit 100`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			flowID, err := resolveFlowID(context.Background(), state, args[0])
			if err != nil {
				return err
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
						formatWhen(execution.StartedAt),
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

func parseExecutionID(arg string) (uuid.UUID, error) {
	id, err := uuid.Parse(arg)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid execution id %q: %w", arg, err)
	}
	return id, nil
}

// defaultRunnerType is the runner backend used when none is specified.
const defaultRunnerType = string(api.Cloud)

func launchFlowRequestBody(runnerType, environmentKey string) ([]byte, error) {
	normalized := strings.TrimSpace(strings.ToLower(runnerType))
	if normalized == "" {
		normalized = defaultRunnerType
	}
	if normalized != string(api.Cloud) && normalized != string(api.SelfHosted) && normalized != string(api.Ephemeral) {
		return nil, fmt.Errorf("invalid runner type %q", runnerType)
	}

	payload := api.LaunchFlowRequest{RunnerType: new(api.RunnerType(normalized))}
	if env := strings.TrimSpace(environmentKey); env != "" {
		payload.EnvironmentKey = &env
	}
	return json.Marshal(payload)
}

func runnerTypeDisplay(runnerType *api.RunnerType) string {
	if runnerType == nil {
		return defaultRunnerType
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
	fmt.Fprintf(os.Stdout, "Started: %s\n", formatWhen(execution.StartedAt))
	if execution.CompletedAt != nil {
		fmt.Fprintf(os.Stdout, "Completed: %s\n", execution.CompletedAt.Format(time.RFC3339))
	}
	if execution.ErrorMessage != nil {
		fmt.Fprintf(os.Stdout, "Error: %s\n", *execution.ErrorMessage)
	}
	return nil
}

// Column headers shared by more than one table renderer.
const (
	columnID   = "ID"
	columnName = "Name"
)

// flowListColumns are the columns every `flow list` variant renders, whether
// the rows come from the list endpoint or from a folder-scoped search.
var flowListColumns = []string{"NAME", "ID", "UPDATED"}

func newFlowListCmd(state *AppState) *cobra.Command {
	var limit int32 = 20
	var offset int32
	var folderRef string
	var uncategorized bool

	cmd := &cobra.Command{
		Use:   listVerb,
		Short: "List flows",
		Long: `List flows.

The ID column is what a <flow-id> argument takes.

With --folder or --uncategorized the listing is scoped to one branch of the flow
folder tree; --folder includes the folder's descendants.`,
		Example: `  echopoint flow list
  echopoint flow list --folder "Anchor/Identity" -o json
  echopoint flow list --uncategorized`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			if folderRef != "" || uncategorized {
				return listFlowsInFolder(state, folderRef, uncategorized, limit, offset)
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
					rows = append(rows, []string{flow.Name, flow.Id.String(), formatWhen(flow.UpdatedAt)})
				}
				fmt.Fprintf(os.Stdout, "Total: %d\n", resp.JSON200.Total)
				return output.PrintTable(flowListColumns, rows)
			}
		},
	}

	cmd.Flags().Int32Var(&limit, "limit", 20, "Number of results to return")
	cmd.Flags().Int32Var(&offset, "offset", 0, "Offset for pagination")
	cmd.Flags().StringVar(&folderRef, "folder", "", "Only list flows in this folder and its descendants (id or path)")
	cmd.Flags().BoolVar(&uncategorized, "uncategorized", false, "Only list flows that are in no folder")
	cmd.ValidArgsFunction = cobra.NoFileCompletions
	registerFolderFlagCompletion(state, cmd, "folder")

	return cmd
}

// listFlowsInFolder backs the folder-scoped variants of `flow list`. Folder
// scoping lives on flow search rather than the plain list endpoint, so the two
// paths render the same columns from different responses.
func listFlowsInFolder(state *AppState, folderRef string, uncategorized bool, limit, offset int32) error {
	if folderRef != "" && uncategorized {
		return fmt.Errorf("--folder cannot be combined with --uncategorized")
	}

	ctx := context.Background()
	body := api.FlowSearchRequest{
		Pagination: &api.PaginationRequest{Limit: &limit, Offset: &offset},
	}
	if uncategorized {
		body.Uncategorized = &uncategorized
	} else {
		idx, err := loadFolderIndex(ctx, state)
		if err != nil {
			return err
		}
		folderID, err := idx.resolve(folderRef)
		if err != nil {
			return err
		}
		body.FolderId = &folderID
	}

	resp, err := state.Client.API().SearchFlowsWithResponse(ctx, nil, body)
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
			rows = append(rows, []string{flow.Name, flow.Id.String(), formatWhen(flow.UpdatedAt)})
		}
		fmt.Fprintf(os.Stdout, "Total: %d\n", resp.JSON200.Total)
		return output.PrintTable(flowListColumns, rows)
	}
}

func newFlowViewCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "view <flow-id>",
		Aliases: []string{getVerb, showVerb},
		Short:   "Show a flow: a summary, or with -o json or yaml the whole flow",
		Long: `Show a flow: its name, id,
version, dates, and how many nodes and edges it has. With -o json or -o yaml it
shows the whole flow, definition included.`,
		Example: `  echopoint flow view <flow-id>
  echopoint flow view <flow-id> -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			id, err := resolveFlowID(cmd.Context(), state, args[0])
			if err != nil {
				return err
			}

			resp, err := state.Client.API().GetFlowWithResponse(cmd.Context(), id, nil)
			if err != nil {
				return err
			}
			if resp.JSON200 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}

			if printed, err := printStructured(cmd.OutOrStdout(), state.OutputFormat, resp.JSON200); printed {
				return err
			}
			printFlowSummary(cmd.OutOrStdout(), *resp.JSON200)
			return nil
		},
	}

	return cmd
}

func printFlowSummary(w io.Writer, flow api.Flow) {
	fmt.Fprintf(w, "\nName: %s\n", flow.Name)
	fmt.Fprintf(w, "ID: %s\n", flow.Id)
	if flow.Description != nil {
		fmt.Fprintf(w, "Description: %s\n", *flow.Description)
	}
	fmt.Fprintf(w, "Version: %s\n", flow.Version)
	fmt.Fprintf(w, "Created: %s\n", flow.CreatedAt)
	fmt.Fprintf(w, "Updated: %s\n", flow.UpdatedAt)

	fmt.Fprintf(w, "\nStructure:\n")
	fmt.Fprintf(w, "  Nodes: %d\n", len(flow.FlowDefinition.Nodes))
	fmt.Fprintf(w, "  Edges: %d\n", len(flow.FlowDefinition.Edges))
	fmt.Fprintln(w)
}

func newFlowCreateCmd(state *AppState) *cobra.Command {
	var file, name string

	cmd := &cobra.Command{
		Use:   "create (--name <name> | -f <file>)",
		Short: "Create a flow: an empty one with --name, or one from a JSON file with -f",
		Long: `Create a flow. Pass exactly one of:
  --name <name>   an empty flow, to build with 'echopoint flow node add'
  -f <file>       a flow from a CreateFlowRequest JSON file`,
		Example: `  echopoint flow create --name "Checkout"
  echopoint flow create -f flow.json`,
		Args: fileFlagArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			req, err := buildCreateFlowRequest(cmd, name, file)
			if err != nil {
				return err
			}

			resp, err := state.Client.API().CreateFlowWithResponse(cmd.Context(), nil, req)
			if err != nil {
				return fmt.Errorf("request failed: %w", err)
			}

			if state.Debug {
				fmt.Fprintf(os.Stderr, "[DEBUG] Response Status: %d\n", resp.StatusCode())
				fmt.Fprintf(os.Stderr, "[DEBUG] Response Body: %s\n", string(resp.Body))
			}

			if resp.JSON201 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}
			return printSavedFlow(cmd, state, resp.JSON201)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Name of the empty flow to create")
	addFileFlag(cmd, &file, "CreateFlowRequest JSON file to create the flow from", "json")
	cmd.ValidArgsFunction = cobra.NoFileCompletions
	return quietOnError(cmd)
}

func printSavedFlow(cmd *cobra.Command, state *AppState, flow *api.Flow) error {
	if printed, err := printStructured(cmd.OutOrStdout(), state.OutputFormat, flow); printed {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "ID: %s\n", flow.Id)
	fmt.Fprintf(cmd.OutOrStdout(), "Name: %s\n", flow.Name)
	return nil
}

func buildCreateFlowRequest(cmd *cobra.Command, name, file string) (api.CreateFlowRequest, error) {
	named, fromFile := cmd.Flags().Changed("name"), cmd.Flags().Changed("file")
	switch {
	case named && fromFile:
		return api.CreateFlowRequest{}, errors.New("pass either --name or -f, not both")
	case !named && !fromFile:
		return api.CreateFlowRequest{}, errors.New("pass --name for an empty flow, or -f for a flow from a JSON file")
	case fromFile:
		var req api.CreateFlowRequest
		if err := loadJSONFile(file, &req); err != nil {
			return req, err
		}
		return req, nil
	}
	if strings.TrimSpace(name) == "" {
		return api.CreateFlowRequest{}, errors.New("--name must not be empty")
	}
	return api.CreateFlowRequest{
		Name: name,
		FlowDefinition: api.FlowDefinition{
			Nodes: []api.FlowNode{},
			Edges: []api.FlowEdge{},
		},
	}, nil
}

func newFlowUpdateCmd(state *AppState) *cobra.Command {
	var file, name, description string

	cmd := &cobra.Command{
		Use:   "update <flow-id> [-f <file>]",
		Short: "Update a flow from a JSON file, or change its name and description with flags",
		Long: `Update a flow. Pass exactly one of:
  -f <file>                    an UpdateFlowRequest JSON file
  --name and --description     the fields to change; the others stay`,
		Example: `  echopoint flow update <flow-id> -f flow.json
  echopoint flow update <flow-id> --name "Checkout smoke test"
  echopoint flow update <flow-id> --description "Runs after every deploy"`,
		Args: fileFlagArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			req, err := buildUpdateFlowRequest(cmd, file, name, description)
			if err != nil {
				return err
			}

			id, err := resolveFlowID(cmd.Context(), state, args[0])
			if err != nil {
				return err
			}

			resp, err := state.Client.API().UpdateFlowWithResponse(cmd.Context(), id, nil, req)
			if err != nil {
				return err
			}
			if resp.JSON200 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}
			return printSavedFlow(cmd, state, resp.JSON200)
		},
	}

	addFileFlag(cmd, &file, "UpdateFlowRequest JSON file", "json")
	cmd.Flags().StringVar(&name, "name", "", "New name of the flow")
	cmd.Flags().StringVar(&description, "description", "", "New description of the flow")
	return quietOnError(cmd)
}

func buildUpdateFlowRequest(cmd *cobra.Command, file, name, description string) (api.UpdateFlowRequest, error) {
	flags := cmd.Flags()
	fromFile := flags.Changed("file")
	flagged := flags.Changed("name") || flags.Changed("description")
	var req api.UpdateFlowRequest
	switch {
	case fromFile && flagged:
		return req, errors.New("pass either -f or --name and --description, not both")
	case !fromFile && !flagged:
		return req, errors.New("pass -f, or at least one of --name and --description")
	case fromFile:
		return req, loadJSONFile(file, &req)
	}
	if flags.Changed("name") {
		if strings.TrimSpace(name) == "" {
			return req, errors.New("--name must not be empty")
		}
		req.Name = &name
	}
	if flags.Changed("description") {
		req.Description = &description
	}
	return req, nil
}

func newFlowDeleteCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <flow-id>",
		Short: "Delete a flow",
		Example: `  echopoint flow delete <flow-id>
  echopoint flow delete <flow-id> --yes`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			id, err := resolveFlowID(cmd.Context(), state, args[0])
			if err != nil {
				return err
			}
			if err := confirmDestructive(cmd, state, "delete", "flow "+id.String()); err != nil {
				return err
			}

			resp, err := state.Client.API().DeleteFlowWithResponse(cmd.Context(), id, nil)
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

	return quietOnError(cmd)
}
