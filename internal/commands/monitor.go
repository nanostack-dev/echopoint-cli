package commands

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/output"
)

const (
	columnHealth = "Health"
	columnRunner = "Runner"
	columnStatus = "Status"
	noValue      = "-"
	monitorNoun  = "monitor"
)

func newMonitorCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:     monitorCommandName,
		Aliases: []string{"monitors"},
		Short:   "Run flows on a schedule, measure their health and follow the runs",
		Long: `A monitor runs the flows it selects, by tag or by explicit ID, on a cadence: a cron expression, or an interval of 60 to 86400 seconds when the monitor has a health policy.

A monitor with a health policy also measures health from its scheduled runs, and a status page can publish it. 'monitor run' starts a diagnostic run of such a monitor; a diagnostic run never confirms health.`,
	}
	cmd.AddCommand(
		newMonitorListCmd(state),
		newMonitorViewCmd(state),
		newMonitorCreateCmd(state),
		newMonitorUpdateCmd(state),
		newMonitorDeleteCmd(state),
		newMonitorStateCmd(state, false),
		newMonitorStateCmd(state, true),
		newMonitorRunCmd(state),
		newMonitorRunsCmd(state),
		newMonitorRunViewCmd(state),
		newMonitorEstimateCmd(state),
		newMonitorPreviewTagsCmd(state),
	)
	return cmd
}

func parseMonitorID(value string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return uuid.Nil, fmt.Errorf("%q is not a monitor id; list the monitors with: echopoint monitor list", value)
	}
	return id, nil
}

func parseMonitorRunID(value string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return uuid.Nil, fmt.Errorf(
			"%q is not a run id; list the runs with: echopoint monitor runs <monitor-id>",
			value,
		)
	}
	return id, nil
}

// completeMonitorArgs completes the monitor ids a command takes as its first
// argument, as <id>\t<name>.
func completeMonitorArgs(state *AppState) completionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) != 0 {
			return noCompletion()
		}
		return completeFromAPI(state, cmd, toComplete, fetchMonitorCandidates(state))
	}
}

func fetchMonitorCandidates(state *AppState) func(context.Context, *api.ClientWithResponses) []completionCandidate {
	return func(ctx context.Context, _ *api.ClientWithResponses) []completionCandidate {
		monitors := walkSome(ctx, completionMaxPages, searchMonitors(state, nil))
		candidates := make([]completionCandidate, 0, len(monitors))
		for _, monitor := range monitors {
			candidates = append(candidates, completionCandidate{monitor.Id.String(), oneLine(monitor.Name)})
		}
		return candidates
	}
}

// completeMonitorRunArgs completes a monitor, then one of its recent runs as
// <id>\t<origin> · <status> · <due>.
func completeMonitorRunArgs(state *AppState) completionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		switch len(args) {
		case 0:
			return completeFromAPI(state, cmd, toComplete, fetchMonitorCandidates(state))
		case 1:
			monitorID, err := parseMonitorID(args[0])
			if err != nil {
				return noCompletion()
			}
			return completeFromAPI(state, cmd, toComplete,
				func(ctx context.Context, _ *api.ClientWithResponses) []completionCandidate {
					// the recent runs are the first page: a completion never walks a long history
					limit := int32(recentExecutionsComplete)
					page, err := searchMonitorRuns(state, monitorID)(ctx, &api.PaginationRequest{Limit: &limit})
					if err != nil {
						return nil
					}
					candidates := make([]completionCandidate, 0, len(page.Items))
					for _, run := range page.Items {
						candidates = append(candidates, completionCandidate{
							run.Id.String(),
							fmt.Sprintf("%s · %s · %s", run.Origin, run.Status, formatWhen(run.DueAt)),
						})
					}
					return candidates
				})
		}
		return noCompletion()
	}
}

// printMonitorResult prints value as JSON or YAML, or calls table for the
// default format.
func printMonitorResult(cmd *cobra.Command, state *AppState, value any, table func(w io.Writer) error) error {
	switch state.OutputFormat {
	case output.FormatJSON:
		return output.PrintJSON(cmd.OutOrStdout(), value)
	case output.FormatYAML:
		return output.PrintYAML(cmd.OutOrStdout(), value)
	case output.FormatTable:
	}
	return table(cmd.OutOrStdout())
}

func monitorCadence(monitor api.Monitor) string {
	switch {
	case monitor.IntervalSeconds > 0:
		return "every " + strconv.Itoa(monitor.IntervalSeconds) + "s"
	case monitor.CronExpression != nil && *monitor.CronExpression != "":
		return *monitor.CronExpression
	}
	return noValue
}

func monitorState(monitor api.Monitor) string {
	if monitor.Enabled {
		return "enabled"
	}
	return "paused"
}

func monitorHealthLabel(monitor api.Monitor) string {
	if monitor.Health == nil {
		return noValue
	}
	return string(monitor.Health.State)
}

func monitorSelection(monitor api.Monitor) string {
	if len(monitor.Tags) > 0 {
		return fmt.Sprintf("tags %s (%s)", strings.Join(monitor.Tags, ", "), monitor.TagMatchMode)
	}
	ids := make([]string, 0, len(monitor.FlowIds))
	for _, id := range monitor.FlowIds {
		ids = append(ids, id.String())
	}
	return "flows " + strings.Join(ids, ", ")
}

func formatOptionalWhen(when *time.Time) string {
	if when == nil {
		return noValue
	}
	return formatWhen(*when)
}

func printMonitorTable(w io.Writer, monitor api.Monitor) error {
	lines := [][2]string{
		{"ID", monitor.Id.String()},
		{"Name", monitor.Name},
		{"State", monitorState(monitor)},
		{"Revision", strconv.FormatInt(monitor.Revision, 10)},
		{"Selection", monitorSelection(monitor)},
		{"Cadence", monitorCadence(monitor)},
		{columnRunner, string(monitor.RunnerType)},
		{"Environment", monitor.EnvironmentKey},
		{"Next run", formatOptionalWhen(monitor.NextRunAt)},
	}
	if monitor.HealthPolicy != nil {
		lines = append(lines, [2]string{
			"Health policy",
			fmt.Sprintf(
				"confirm %d, recover %d, fresh %ds",
				monitor.HealthPolicy.ConfirmationRuns,
				monitor.HealthPolicy.RecoveryRuns,
				monitor.HealthPolicy.FreshnessSeconds,
			),
		})
		lines = append(lines, [2]string{columnHealth, monitorHealthLabel(monitor)})
		if monitor.Health != nil && monitor.Health.Reason != "" {
			lines = append(lines, [2]string{"Reason", monitor.Health.Reason})
		}
	}
	for _, line := range lines {
		if _, err := fmt.Fprintf(w, "%s: %s\n", line[0], line[1]); err != nil {
			return err
		}
	}
	return nil
}

func printMonitor(
	cmd *cobra.Command,
	state *AppState,
	monitor *api.Monitor,
	response *http.Response,
	body []byte,
) error {
	if monitor == nil {
		return formatAPIError(response, body)
	}
	return printMonitorResult(cmd, state, monitor, func(w io.Writer) error { return printMonitorTable(w, *monitor) })
}

func newMonitorListCmd(state *AppState) *cobra.Command {
	var pages pageFlags
	var enabled, healthPolicy bool
	cmd := quietOnError(&cobra.Command{
		Use:   listVerb,
		Short: "List monitors with their cadence and current health",
		Long: `List monitors, newest first, with their cadence and current health.

A page ends with the cursor that continues the listing: -o json carries it as
next_cursor, and the table says how to pass it with --cursor. --all follows it to
the last monitor.`,
		Example: `  echopoint monitor list
  echopoint --org <organization-id> monitor list --enabled --has-health-policy -o json
  echopoint monitor list --cursor <next_cursor> -o json
  echopoint monitor list --all`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			if err := pages.validate(); err != nil {
				return err
			}
			var filter *api.MonitorSearchFilter
			if cmd.Flags().Changed("enabled") || cmd.Flags().Changed("has-health-policy") {
				filter = &api.MonitorSearchFilter{}
				if cmd.Flags().Changed("enabled") {
					filter.Enabled = &enabled
				}
				if cmd.Flags().Changed("has-health-policy") {
					filter.HasHealthPolicy = &healthPolicy
				}
			}
			page, err := fetchSearch(cmd.Context(), cmd, &pages, searchMonitors(state, filter))
			if err != nil {
				return err
			}
			return printSearchPage(cmd, state, page, func(w io.Writer) error {
				rows := make([][]string, 0, len(page.Items))
				for _, monitor := range page.Items {
					rows = append(rows, []string{
						monitor.Id.String(), monitor.Name, monitorState(monitor), monitorCadence(monitor),
						string(monitor.RunnerType), monitorHealthLabel(monitor), formatOptionalWhen(monitor.NextRunAt),
					})
				}
				fmt.Fprintf(w, "Total: %d\n", page.Total)
				return output.PrintTableTo(w,
					[]string{columnID, columnName, "State", "Cadence", columnRunner, columnHealth, "Next run"}, rows)
			})
		},
	})
	pages.bind(cmd, defaultListLimit)
	cmd.Flags().BoolVar(&enabled, "enabled", false, "Only enabled monitors; --enabled=false lists the paused ones")
	cmd.Flags().BoolVar(&healthPolicy, "has-health-policy", false,
		"Only monitors with a health policy; --has-health-policy=false lists the others")
	return cmd
}

// searchMonitors reads one page of the organization's monitors, newest first.
func searchMonitors(state *AppState, filter *api.MonitorSearchFilter) pageFetcher[api.Monitor] {
	return func(ctx context.Context, pagination *api.PaginationRequest) (searchPage[api.Monitor], error) {
		resp, err := state.Client.API().SearchFlowSchedulesWithResponse(
			ctx, nil, api.MonitorSearchRequest{Filter: filter, Pagination: pagination},
		)
		if err != nil {
			return searchPage[api.Monitor]{}, err
		}
		if resp.JSON200 == nil {
			return searchPage[api.Monitor]{}, formatAPIError(resp.HTTPResponse, resp.Body)
		}
		return newSearchPage(resp.JSON200.Items, resp.JSON200.Total, resp.JSON200.NextCursor), nil
	}
}

func newMonitorViewCmd(state *AppState) *cobra.Command {
	return quietOnError(&cobra.Command{
		Use:     "view <monitor-id>",
		Aliases: []string{getVerb, showVerb},
		Short:   "Show a monitor's selection, cadence, health policy and current health",
		Example: `  echopoint monitor view <monitor-id>
  echopoint monitor view <monitor-id> -o yaml`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeMonitorArgs(state),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			id, err := parseMonitorID(args[0])
			if err != nil {
				return err
			}
			resp, err := state.Client.API().GetFlowScheduleWithResponse(cmd.Context(), id, nil)
			if err != nil {
				return err
			}
			return printMonitor(cmd, state, resp.JSON200, resp.HTTPResponse, resp.Body)
		},
	})
}

func newMonitorDeleteCmd(state *AppState) *cobra.Command {
	return quietOnError(&cobra.Command{
		Use:   "delete <monitor-id>",
		Short: "Delete a monitor and stop its scheduled runs",
		Example: `  echopoint monitor delete <monitor-id>
  echopoint monitor delete <monitor-id> --yes`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeMonitorArgs(state),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			id, err := parseMonitorID(args[0])
			if err != nil {
				return err
			}
			if err := confirmDestructive(cmd, state, "delete", monitorNoun+" "+id.String()); err != nil {
				return err
			}
			resp, err := state.Client.API().DeleteFlowScheduleWithResponse(cmd.Context(), id, nil)
			if err != nil {
				return err
			}
			if resp.StatusCode() != http.StatusNoContent {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}
			result := struct {
				ID      string `json:"id"`
				Deleted bool   `json:"deleted"`
			}{id.String(), true}
			return printMonitorResult(cmd, state, &result, func(w io.Writer) error {
				_, err := fmt.Fprintln(w, "Monitor deleted.")
				return err
			})
		},
	})
}

func newMonitorStateCmd(state *AppState, resume bool) *cobra.Command {
	verb, short := "pause", "Pause a monitor: it stops running and its health becomes not monitored"
	if resume {
		verb, short = "resume", "Resume a paused monitor on its next due time"
	}
	return quietOnError(&cobra.Command{
		Use:               verb + " <monitor-id>",
		Short:             short,
		Example:           "  echopoint monitor " + verb + " <monitor-id>",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeMonitorArgs(state),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			id, err := parseMonitorID(args[0])
			if err != nil {
				return err
			}
			if resume {
				resp, err := state.Client.API().ResumeFlowScheduleWithResponse(cmd.Context(), id, nil)
				if err != nil {
					return err
				}
				return printMonitor(cmd, state, resp.JSON200, resp.HTTPResponse, resp.Body)
			}
			resp, err := state.Client.API().PauseFlowScheduleWithResponse(cmd.Context(), id, nil)
			if err != nil {
				return err
			}
			return printMonitor(cmd, state, resp.JSON200, resp.HTTPResponse, resp.Body)
		},
	})
}

func newMonitorRunCmd(state *AppState) *cobra.Command {
	return quietOnError(&cobra.Command{
		Use:   "run <monitor-id>",
		Short: "Start a diagnostic run of a monitor with a health policy",
		Long:  `Start a diagnostic run of the monitor's flow selection and return without waiting. Only a monitor with a health policy has diagnostic runs, and a diagnostic run never confirms health: only scheduled runs do. Follow the run with 'monitor run-view'.`,
		Example: `  echopoint monitor run <monitor-id>
  echopoint monitor run <monitor-id> -o json`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeMonitorArgs(state),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			id, err := parseMonitorID(args[0])
			if err != nil {
				return err
			}
			resp, err := state.Client.API().RunMonitorWithResponse(cmd.Context(), id, nil)
			if err != nil {
				return err
			}
			return printMonitorRun(cmd, state, resp.JSON202, resp.HTTPResponse, resp.Body)
		},
	})
}
