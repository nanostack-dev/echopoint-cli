package commands

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/output"
)

func printMonitorRunTable(w io.Writer, run api.MonitorRun) error {
	lines := [][2]string{
		{"ID", run.Id.String()},
		{"Monitor", run.ScheduleId.String()},
		{"Origin", string(run.Origin)},
		{columnStatus, string(run.Status)},
		{"Revision", fmt.Sprint(run.Revision)},
		{"Due", formatWhen(run.DueAt)},
		{columnStarted, formatOptionalWhen(run.StartedAt)},
		{"Completed", formatOptionalWhen(run.CompletedAt)},
		{"Matched flows", fmt.Sprint(run.MatchedFlowCount)},
	}
	if run.Observation != nil {
		lines = append(
			lines,
			[2]string{"Observation", fmt.Sprintf("%s: %s", run.Observation.State, run.Observation.Reason)},
		)
	}
	for _, failure := range run.LaunchFailures {
		lines = append(
			lines,
			[2]string{"Launch failure", fmt.Sprintf("%s %s: %s", failure.FlowId, failure.Code, failure.Reason)},
		)
	}
	for _, line := range lines {
		if _, err := fmt.Fprintf(w, "%s: %s\n", line[0], line[1]); err != nil {
			return err
		}
	}
	return nil
}

func printMonitorRun(
	cmd *cobra.Command, state *AppState, run *api.MonitorRun, response *http.Response, body []byte,
) error {
	if run == nil {
		return formatAPIError(response, body)
	}
	return printMonitorResult(cmd, state, run, func(w io.Writer) error { return printMonitorRunTable(w, *run) })
}

func newMonitorRunsCmd(state *AppState) *cobra.Command {
	var pages pageFlags
	cmd := quietOnError(&cobra.Command{
		Use:   "runs <monitor-id>",
		Short: "List a monitor's scheduled and diagnostic runs, newest first",
		Long: `List a monitor's scheduled and diagnostic runs, newest first.

A page ends with the cursor that continues the listing: -o json carries it as
next_cursor, and the table says how to pass it with --cursor. --all follows it to
the oldest run.`,
		Example: `  echopoint monitor runs <monitor-id>
  echopoint monitor runs <monitor-id> --limit 50 -o json
  echopoint monitor runs <monitor-id> --cursor <next_cursor> -o json`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeMonitorArgs(state),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			if err := pages.validate(); err != nil {
				return err
			}
			id, err := parseMonitorID(args[0])
			if err != nil {
				return err
			}
			page, err := fetchSearch(cmd.Context(), cmd, &pages, searchMonitorRuns(state, id))
			if err != nil {
				return err
			}
			return printSearchPage(cmd, state, page, func(w io.Writer) error {
				rows := make([][]string, 0, len(page.Items))
				for _, run := range page.Items {
					observation := noValue
					if run.Observation != nil {
						observation = string(run.Observation.State)
					}
					rows = append(rows, []string{
						run.Id.String(), string(run.Origin), string(run.Status), formatWhen(run.DueAt),
						fmt.Sprint(run.MatchedFlowCount), observation,
					})
				}
				fmt.Fprintf(w, "Total: %d\n", page.Total)
				return output.PrintTableTo(
					w,
					[]string{columnID, "Origin", columnStatus, "Due", "Flows", "Observation"},
					rows,
				)
			})
		},
	})
	pages.bind(cmd, defaultListLimit)
	return cmd
}

// searchMonitorRuns reads one page of a monitor's runs, newest first.
func searchMonitorRuns(state *AppState, monitorID uuid.UUID) pageFetcher[api.MonitorRun] {
	return func(ctx context.Context, pagination *api.PaginationRequest) (searchPage[api.MonitorRun], error) {
		resp, err := state.Client.API().SearchFlowScheduleRunsWithResponse(
			ctx, monitorID, nil, api.MonitorRunSearchRequest{Pagination: pagination},
		)
		if err != nil {
			return searchPage[api.MonitorRun]{}, err
		}
		if resp.JSON200 == nil {
			return searchPage[api.MonitorRun]{}, formatAPIError(resp.HTTPResponse, resp.Body)
		}
		return newSearchPage(resp.JSON200.Items, resp.JSON200.Total, resp.JSON200.NextCursor), nil
	}
}

func newMonitorRunViewCmd(state *AppState) *cobra.Command {
	return quietOnError(&cobra.Command{
		Use:   "run-view <monitor-id> <run-id>",
		Short: "Show one monitor run and the executions it launched",
		Example: `  echopoint monitor run-view <monitor-id> <run-id>
  echopoint monitor run-view <monitor-id> <run-id> -o json`,
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeMonitorRunArgs(state),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			monitorID, err := parseMonitorID(args[0])
			if err != nil {
				return err
			}
			runID, err := parseMonitorRunID(args[1])
			if err != nil {
				return err
			}
			resp, err := state.Client.API().GetFlowScheduleRunWithResponse(cmd.Context(), monitorID, runID, nil)
			if err != nil {
				return err
			}
			if resp.JSON200 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}
			return printMonitorResult(cmd, state, resp.JSON200, func(w io.Writer) error {
				if err := printMonitorRunTable(w, resp.JSON200.Run); err != nil {
					return err
				}
				rows := make([][]string, 0, len(resp.JSON200.Executions))
				for _, execution := range resp.JSON200.Executions {
					message := noValue
					if execution.ErrorMessage != nil {
						message = oneLine(*execution.ErrorMessage)
					}
					rows = append(rows, []string{
						execution.Id.String(), execution.FlowId.String(), string(execution.Status),
						formatWhen(execution.StartedAt), message,
					})
				}
				fmt.Fprintln(w)
				return output.PrintTableTo(w, []string{columnID, "Flow", columnStatus, columnStarted, "Error"}, rows)
			})
		},
	})
}
