package commands

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"echopoint-cli/internal/api"
)

func newProbeRunCmd(state *AppState) *cobra.Command {
	var revision int64
	cmd := quietOnError(
		&cobra.Command{
			Use:               "run <probe-id>",
			Short:             "Queue a private diagnostic run; it never contributes to public health",
			Long:              "Queue a diagnostic run of the saved flow selector and return without waiting. This does not create a policy occurrence. Follow its ID with probe execution view; scheduled occurrences alone establish probe health.",
			Example:           "  echopoint probe run <probe-id> --expected-revision 3 -o json",
			Args:              cobra.ExactArgs(1),
			ValidArgsFunction: completeProbeArgs(state),
			RunE: func(cmd *cobra.Command, args []string) error {
				if err := requireToken(state); err != nil {
					return err
				}
				if err := validateProbeRevision(revision); err != nil {
					return err
				}
				id, err := parseProbeID(args[0], probeCommandName)
				if err != nil {
					return err
				}
				resp, err := state.Client.API().
					RunProbeWithResponse(cmd.Context(), id, nil, api.ProbeRevisionRequest{ExpectedRevision: revision})
				if err != nil {
					return err
				}
				return printStatusResponse(cmd, state, resp.JSON202, resp.HTTPResponse, resp.Body)
			},
		},
	)
	addProbeRevisionFlag(cmd, &revision)
	return cmd
}

func completeProbeExecutionArgs(state *AppState) completionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return completeProbeArgs(state)(cmd, args, toComplete)
		}
		if len(args) != 1 {
			return noCompletion()
		}
		id, err := parseProbeID(args[0], probeCommandName)
		if err != nil {
			return noCompletion()
		}
		return completeFromAPI(
			state,
			cmd,
			toComplete,
			func(ctx context.Context, cli *api.ClientWithResponses) []completionCandidate {
				resp, err := cli.ListProbeRunsWithResponse(
					ctx,
					id,
					&api.ListProbeRunsParams{Limit: recentExecutionsComplete},
				)
				if err != nil || resp.JSON200 == nil {
					return nil
				}
				candidates := make([]completionCandidate, 0, len(resp.JSON200.Items))
				for _, run := range resp.JSON200.Items {
					candidates = append(
						candidates,
						completionCandidate{
							run.Id,
							fmt.Sprintf("%s · %s · %s", run.Origin, run.Status, formatWhen(run.DueAt)),
						},
					)
				}
				return candidates
			},
		)
	}
}

func newProbeExecutionCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "execution",
		Aliases: []string{"executions"},
		Short:   "Inspect scheduled and diagnostic probe run history",
	}
	cmd.AddCommand(newProbeExecutionListCmd(state), newProbeExecutionViewCmd(state))
	return cmd
}

func newProbeExecutionListCmd(state *AppState) *cobra.Command {
	var limit, offset int
	cmd := quietOnError(
		&cobra.Command{
			Use:               "list <probe-id>",
			Short:             "List durable probe runs with origin, revision and execution evidence",
			Example:           "  echopoint probe execution list <probe-id> --limit 20 -o json",
			Args:              cobra.ExactArgs(1),
			ValidArgsFunction: completeProbeArgs(state),
			RunE: func(cmd *cobra.Command, args []string) error {
				if err := requireToken(state); err != nil {
					return err
				}
				if err := validateAPIPagination(limit, offset); err != nil {
					return err
				}
				id, err := parseProbeID(args[0], probeCommandName)
				if err != nil {
					return err
				}
				resp, err := state.Client.API().
					ListProbeRunsWithResponse(cmd.Context(), id, &api.ListProbeRunsParams{Limit: api.LimitParameter(limit), Offset: api.OffsetParameter(offset)})
				if err != nil {
					return err
				}
				return printStatusResponse(cmd, state, resp.JSON200, resp.HTTPResponse, resp.Body)
			},
		},
	)
	addProbePagination(cmd, &limit, &offset)
	return cmd
}

func newProbeExecutionViewCmd(state *AppState) *cobra.Command {
	return quietOnError(
		&cobra.Command{
			Use:               "view <probe-id> <run-id>",
			Aliases:           []string{getVerb, showVerb},
			Short:             "Show a private run and its step/assertion evidence",
			Example:           "  echopoint probe execution view <probe-id> <run-id> -o json",
			Args:              cobra.ExactArgs(2),
			ValidArgsFunction: completeProbeExecutionArgs(state),
			RunE: func(cmd *cobra.Command, args []string) error {
				if err := requireToken(state); err != nil {
					return err
				}
				id, err := parseProbeID(args[0], probeCommandName)
				if err != nil {
					return err
				}
				run, err := parseProbeID(args[1], "run")
				if err != nil {
					return err
				}
				resp, err := state.Client.API().GetProbeRunWithResponse(cmd.Context(), id, run, nil)
				if err != nil {
					return err
				}
				return printStatusResponse(cmd, state, resp.JSON200, resp.HTTPResponse, resp.Body)
			},
		},
	)
}
