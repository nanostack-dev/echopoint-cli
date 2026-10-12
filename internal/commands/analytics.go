package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/output"
)

const (
	defaultAnalyticsWindow = api.AnalyticsWindowN7d
	analyticsTimeLayout    = "2006-01-02 15:04"
	analyticsNone          = "-"
	percent                = 100

	columnRuns   = "Runs"
	columnFailed = "Failed"
)

var analyticsWindows = []api.AnalyticsWindow{api.AnalyticsWindowN24h, api.AnalyticsWindowN7d, api.AnalyticsWindowN30d}

func newAnalyticsCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   analyticsCommandName,
		Short: "Read the runs, run time and HTTP calls of the organization's finished runs",
		Long: `Read execution analytics: the organization's finished runs in a window, how
often they failed, how long they took, and the HTTP calls they sent.

A run counts in the window its completion time falls in and reaches analytics a
few seconds after it finishes. Runs whose runner predates HTTP call recording
count as runs but not as measured runs, so their calls are missing from the
call totals.`,
	}
	cmd.AddCommand(
		newAnalyticsSummaryCmd(state),
		newAnalyticsFlowsCmd(state),
		newAnalyticsEndpointsCmd(state),
	)
	return cmd
}

// addWindowFlag registers --window; parseAnalyticsWindow validates it before any request.
func addWindowFlag(cmd *cobra.Command, target *string) {
	cmd.Flags().StringVar(target, "window", string(defaultAnalyticsWindow),
		"How far back to look: 24h (hourly buckets), 7d or 30d (daily buckets)")
	_ = cmd.RegisterFlagCompletionFunc("window",
		func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			values := make([]string, 0, len(analyticsWindows))
			for _, window := range analyticsWindows {
				values = append(values, string(window))
			}
			return values, cobra.ShellCompDirectiveNoFileComp
		})
}

func parseAnalyticsWindow(value string) (*api.AnalyticsWindowQuery, error) {
	window := api.AnalyticsWindow(strings.TrimSpace(value))
	if !window.Valid() {
		return nil, fmt.Errorf("invalid --window %q: use 24h, 7d or 30d", value)
	}
	return &window, nil
}

// printAnalytics prints the API body as received for -o json and -o yaml, so a
// field this CLI does not know yet still reaches scripts, and calls table otherwise.
func printAnalytics(
	cmd *cobra.Command,
	state *AppState,
	ok bool,
	response *http.Response,
	body []byte,
	table func(w io.Writer) error,
) error {
	if !ok {
		return formatAPIError(response, body)
	}
	w := cmd.OutOrStdout()
	switch state.OutputFormat {
	case output.FormatJSON:
		return output.PrintJSON(w, json.RawMessage(body))
	case output.FormatYAML:
		return output.PrintYAML(w, json.RawMessage(body))
	case output.FormatTable:
	}
	return table(w)
}

func newAnalyticsSummaryCmd(state *AppState) *cobra.Command {
	var window string
	cmd := &cobra.Command{
		Use:   "summary",
		Short: "Show the totals of the organization's finished runs and their HTTP calls",
		Example: `  echopoint analytics summary
  echopoint analytics summary --window 24h -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			query, err := parseAnalyticsWindow(window)
			if err != nil {
				return err
			}
			resp, err := state.Client.API().GetExecutionAnalyticsSummaryWithResponse(cmd.Context(),
				&api.GetExecutionAnalyticsSummaryParams{Window: query, XOrganizationID: state.OrganizationID})
			if err != nil {
				return err
			}
			return printAnalytics(cmd, state, resp.JSON200 != nil, resp.HTTPResponse, resp.Body,
				func(w io.Writer) error { return printAnalyticsSummary(w, resp.JSON200) })
		},
	}
	addWindowFlag(cmd, &window)
	return quietOnError(cmd)
}

func newAnalyticsFlowsCmd(state *AppState) *cobra.Command {
	var window string
	cmd := &cobra.Command{
		Use:   "flows",
		Short: "List the flows that finished a run in the window, most runs first",
		Example: `  echopoint analytics flows
  echopoint analytics flows --window 30d -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			query, err := parseAnalyticsWindow(window)
			if err != nil {
				return err
			}
			resp, err := state.Client.API().ListFlowAnalyticsWithResponse(cmd.Context(),
				&api.ListFlowAnalyticsParams{Window: query, XOrganizationID: state.OrganizationID})
			if err != nil {
				return err
			}
			return printAnalytics(cmd, state, resp.JSON200 != nil, resp.HTTPResponse, resp.Body,
				func(w io.Writer) error { return printFlowAnalytics(w, resp.JSON200) })
		},
	}
	addWindowFlag(cmd, &window)
	return quietOnError(cmd)
}

func newAnalyticsEndpointsCmd(state *AppState) *cobra.Command {
	var window, flow string
	cmd := &cobra.Command{
		Use:   "endpoints",
		Short: "List the endpoints the window's runs called, most calls first",
		Long: `List the HTTP calls of the window's runs grouped by method, host and path
template, most calls first. An endpoint is the request's URL template before
resolution, so /users/{{userId}} stays one endpoint whatever the user id. At most
100 endpoints are listed.`,
		Example: `  echopoint analytics endpoints
  echopoint analytics endpoints --window 24h --flow-id <flow-id> -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			query, err := parseAnalyticsWindow(window)
			if err != nil {
				return err
			}
			params := &api.ListEndpointAnalyticsParams{Window: query, XOrganizationID: state.OrganizationID}
			if flow != "" {
				flowID, err := resolveFlowID(cmd.Context(), state, flow)
				if err != nil {
					return fmt.Errorf("--flow-id: %w", err)
				}
				params.FlowId = &flowID
			}
			resp, err := state.Client.API().ListEndpointAnalyticsWithResponse(cmd.Context(), params)
			if err != nil {
				return err
			}
			return printAnalytics(cmd, state, resp.JSON200 != nil, resp.HTTPResponse, resp.Body,
				func(w io.Writer) error { return printEndpointAnalytics(w, resp.JSON200) })
		},
	}
	addWindowFlag(cmd, &window)
	cmd.Flags().StringVar(&flow, "flow-id", "", "Keep only the calls this flow's runs sent")
	_ = cmd.RegisterFlagCompletionFunc("flow-id", completeFlowFlag(state))
	return quietOnError(cmd)
}

func printAnalyticsSummary(w io.Writer, summary *api.ExecutionAnalyticsSummary) error {
	fmt.Fprintln(w, describeWindow(summary.Window, summary.WindowStart, summary.WindowEnd))
	fmt.Fprintln(w)
	err := output.PrintTableTo(w, nil, [][]string{
		{columnRuns, strconv.FormatInt(summary.Runs, 10)},
		{columnFailed, fmt.Sprintf("%d (%s)", summary.FailedRuns, formatRate(summary.FailedRuns, summary.Runs))},
		{"Cancelled", strconv.FormatInt(summary.CancelledRuns, 10)},
		{"Run time", fmt.Sprintf("total %s, avg %s, p95 %s", formatMillis(summary.RunMsTotal),
			formatOptionalMillis(summary.RunMsAverage), formatOptionalMillis(summary.RunMsP95))},
		{"Cloud run time", formatMillis(summary.CloudRunMsTotal)},
		{"Measured runs", strconv.FormatInt(summary.MeasuredRuns, 10)},
		{"HTTP calls", strconv.FormatInt(summary.HttpCalls, 10)},
		{"Failed calls", formatCallFailures(summary.FailedHttpCalls, summary.CallFailures)},
	})
	if err != nil || len(summary.RunFailures) == 0 {
		return err
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Top run error codes")
	rows := make([][]string, 0, len(summary.RunFailures))
	for _, failure := range summary.RunFailures {
		code := "(no code)"
		if failure.ErrorCode != nil {
			code = *failure.ErrorCode
		}
		rows = append(rows, []string{code, strconv.FormatInt(failure.Runs, 10)})
	}
	return output.PrintTableTo(w, []string{"Error code", columnRuns}, rows)
}

func printFlowAnalytics(w io.Writer, list *api.FlowAnalyticsList) error {
	fmt.Fprintln(w, describeWindow(list.Window, list.WindowStart, list.WindowEnd))
	if len(list.Flows) == 0 {
		fmt.Fprintln(w, "No flow finished a run in the window.")
		return nil
	}
	fmt.Fprintln(w)
	rows := make([][]string, 0, len(list.Flows))
	for _, flow := range list.Flows {
		name := flow.FlowName
		if name == "" {
			name = "(deleted)"
		}
		peak := analyticsNone
		if flow.HttpCallPeak != nil {
			peak = strconv.Itoa(int(*flow.HttpCallPeak))
		}
		rows = append(rows, []string{
			flow.FlowId.String(),
			name,
			strconv.FormatInt(flow.Runs, 10),
			strconv.FormatInt(flow.FailedRuns, 10),
			formatMillis(flow.RunMsAverage),
			formatMillis(flow.RunMsP95),
			strconv.FormatInt(flow.HttpCalls, 10),
			peak,
		})
	}
	return output.PrintTableTo(w,
		[]string{columnID, columnName, columnRuns, columnFailed, "Avg", "P95", "Calls", "Peak"}, rows)
}

func printEndpointAnalytics(w io.Writer, list *api.EndpointAnalyticsList) error {
	header := describeWindow(list.Window, list.WindowStart, list.WindowEnd)
	if list.FlowId != nil {
		header += ", flow " + list.FlowId.String()
	}
	fmt.Fprintln(w, header)
	if len(list.Endpoints) == 0 {
		fmt.Fprintln(w, "No HTTP call was recorded in the window.")
		return nil
	}
	fmt.Fprintln(w)
	rows := make([][]string, 0, len(list.Endpoints))
	for _, endpoint := range list.Endpoints {
		p95 := ">10s"
		if endpoint.DurationMsP95AtMost != nil {
			p95 = "<=" + formatMillis(*endpoint.DurationMsP95AtMost)
		}
		rows = append(rows, []string{
			endpoint.Method,
			endpoint.Host,
			endpoint.PathTemplate,
			strconv.FormatInt(endpoint.Calls, 10),
			strconv.FormatInt(endpoint.FailedCalls, 10),
			formatMillis(endpoint.DurationMsAverage),
			formatMillis(endpoint.DurationMsMax),
			p95,
		})
	}
	err := output.PrintTableTo(w,
		[]string{"Method", "Host", "Path", "Calls", columnFailed, "Avg", "Max", "P95"}, rows)
	if err == nil && list.Truncated {
		fmt.Fprintf(w, "\nOnly the %d endpoints with the most calls are listed.\n", len(list.Endpoints))
	}
	return err
}

func describeWindow(window api.AnalyticsWindow, start, end time.Time) string {
	return fmt.Sprintf("Window: %s, %s to %s UTC", window,
		start.UTC().Format(analyticsTimeLayout), end.UTC().Format(analyticsTimeLayout))
}

func formatRate(part, total int64) string {
	if total == 0 {
		return analyticsNone
	}
	return strconv.FormatFloat(float64(part)*percent/float64(total), 'f', 1, 64) + "%"
}

// formatCallFailures lists only the failure classes that occurred.
func formatCallFailures(failed int64, counts api.HTTPCallFailureCounts) string {
	text := strconv.FormatInt(failed, 10)
	var classes []string
	for _, class := range []struct {
		name  string
		count int64
	}{
		{"4xx", counts.Status4xx}, {"5xx", counts.Status5xx}, {"dns", counts.Dns},
		{"connect", counts.Connect}, {"tls", counts.Tls}, {"timeout", counts.Timeout},
		{"no response", counts.NoResponse},
	} {
		if class.count > 0 {
			classes = append(classes, fmt.Sprintf("%s %d", class.name, class.count))
		}
	}
	if len(classes) > 0 {
		text += " (" + strings.Join(classes, ", ") + ")"
	}
	return text
}

func formatOptionalMillis(ms *int64) string {
	if ms == nil {
		return analyticsNone
	}
	return formatMillis(*ms)
}

// formatMillis prints a duration in milliseconds at the precision a reader needs:
// 850ms, 2.1s, 4m12s.
func formatMillis(ms int64) string {
	duration := time.Duration(ms) * time.Millisecond
	switch {
	case duration < time.Second:
		return strconv.FormatInt(ms, 10) + "ms"
	case duration < time.Minute:
		return strconv.FormatFloat(duration.Seconds(), 'f', 1, 64) + "s"
	default:
		return duration.Round(time.Second).String()
	}
}
