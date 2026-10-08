package commands

import (
	"context"
	"fmt"
	"net/http"
	"regexp"

	"github.com/spf13/cobra"

	"echopoint-cli/internal/api"
)

func newProbeCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:     probeCommandName,
		Aliases: []string{"probes"},
		Short:   "Manage durable capability probes and private diagnostic runs",
		Long:    "A probe is a monitor that runs flows selected by tags or explicit IDs, with an environment and health policy. The server snapshots each run's flow sources. Scheduled occurrences measure capabilities; manual runs are private diagnostics and never confirm public health.",
	}
	cmd.AddCommand(
		newProbeListCmd(state),
		newProbeViewCmd(state),
		newProbeWriteCmd(state, false),
		newProbeWriteCmd(state, true),
		newProbeValidateCmd(state),
		newProbeDeleteCmd(state),
		newProbeStateCmd(state, false),
		newProbeStateCmd(state, true),
		newProbeRunCmd(state),
		newProbeExecutionCmd(state),
		newProbeSourceOptionsCmd(state),
		newProbeEstimateCmd(state),
	)
	history := newProbeExecutionListCmd(state)
	history.Use = "history <probe-id>"
	history.Example = "  echopoint probe history <probe-id> --limit 20 -o json"
	cmd.AddCommand(history)
	return cmd
}

func parseProbeID(value, resource string) (string, error) {
	if !regexp.MustCompile(`^[0-9A-Za-z]{27}$`).MatchString(value) {
		return "", fmt.Errorf("%q is not a %s ID (27-character KSUID)", value, resource)
	}
	return value, nil
}

func completeProbeArgs(state *AppState) completionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) != 0 {
			return noCompletion()
		}
		return completeFromAPI(
			state,
			cmd,
			toComplete,
			func(ctx context.Context, cli *api.ClientWithResponses) []completionCandidate {
				resp, err := cli.ListProbesWithResponse(ctx, &api.ListProbesParams{Limit: completionPageSize})
				if err != nil || resp.JSON200 == nil {
					return nil
				}
				candidates := make([]completionCandidate, 0, len(resp.JSON200.Items))
				for _, item := range resp.JSON200.Items {
					candidates = append(candidates, completionCandidate{item.Id, oneLine(item.Config.Name)})
				}
				return candidates
			},
		)
	}
}

func newProbeListCmd(state *AppState) *cobra.Command {
	var limit, offset int
	cmd := quietOnError(&cobra.Command{Use: listVerb, Short: "List saved probes and their current capability health",
		Example: "  echopoint --org <organization-id> probe list --limit 20 -o json", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			if err := validateAPIPagination(limit, offset); err != nil {
				return err
			}
			resp, err := state.Client.API().
				ListProbesWithResponse(cmd.Context(), &api.ListProbesParams{Limit: api.LimitParameter(limit), Offset: api.OffsetParameter(offset)})
			if err != nil {
				return err
			}
			return printStatusResponse(cmd, state, resp.JSON200, resp.HTTPResponse, resp.Body)
		},
	})
	addProbePagination(cmd, &limit, &offset)
	return cmd
}

func newProbeViewCmd(state *AppState) *cobra.Command {
	return quietOnError(
		&cobra.Command{
			Use:               "view <probe-id>",
			Aliases:           []string{getVerb, showVerb},
			Short:             "Show the saved source, policy, revision and capability health",
			Example:           "  echopoint probe view <probe-id> -o json",
			Args:              cobra.ExactArgs(1),
			ValidArgsFunction: completeProbeArgs(state),
			RunE: func(cmd *cobra.Command, args []string) error {
				if err := requireToken(state); err != nil {
					return err
				}
				id, err := parseProbeID(args[0], probeCommandName)
				if err != nil {
					return err
				}
				resp, err := state.Client.API().GetProbeWithResponse(cmd.Context(), id, nil)
				if err != nil {
					return err
				}
				return printStatusResponse(cmd, state, resp.JSON200, resp.HTTPResponse, resp.Body)
			},
		},
	)
}

func newProbeWriteCmd(state *AppState, update bool) *cobra.Command {
	var file, environment string
	var selectors probeSelectorFlags
	var creation probeCreationFlags
	use, short, count := "create", "Create a probe from flow/tag flags or a complete JSON request (-f -: stdin)", 0
	if update {
		use, short, count = "update <probe-id> -f <file>", "Replace source and policy using expected_revision from a complete JSON request", 1
	}
	cmd := quietOnError(&cobra.Command{Use: use, Short: short, Args: fileFlagArgs(count),
		Example: "  echopoint --org <organization-id> probe create -f probe.json --environment production -o json",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !update {
				request, err := readProbeCreation(cmd, state, file, environment, selectors, creation)
				if err != nil {
					return err
				}
				if err := requireToken(state); err != nil {
					return err
				}
				resp, err := state.Client.API().CreateProbeWithResponse(cmd.Context(), nil, request)
				if err != nil {
					return err
				}
				return printStatusResponse(cmd, state, resp.JSON201, resp.HTTPResponse, resp.Body)
			}
			request, err := readProbeRequest[api.UpdateProbeRequest](
				cmd,
				state,
				file,
				"UpdateProbeRequest",
				environment,
				selectors,
			)
			if err != nil {
				return err
			}
			if err := requireToken(state); err != nil {
				return err
			}
			id, err := parseProbeID(args[0], probeCommandName)
			if err != nil {
				return err
			}
			resp, err := state.Client.API().UpdateProbeWithResponse(cmd.Context(), id, nil, request)
			if err != nil {
				return err
			}
			return printStatusResponse(cmd, state, resp.JSON200, resp.HTTPResponse, resp.Body)
		},
	})
	if update {
		cmd.Example = "  echopoint probe update <probe-id> -f update.json --environment production -o json"
		cmd.ValidArgsFunction = completeProbeArgs(state)
	} else {
		cmd.Example = "  echopoint --org <organization-id> probe create --name Checkout --tag production --environment production -o json\n  echopoint probe create --name API --flow-id <flow-id> --interval 60 --paused -o json\n  echopoint probe create -f probe.json -o json"
		cmd.ValidArgsFunction = cobra.NoFileCompletions
		cmd.Flags().StringVar(&creation.name, probeNameField, "", "Probe name (required when creating from flags)")
		cmd.Flags().StringVar(&creation.runner, "runner", "cloud", "Where EchoPoint runs flows: cloud or self_hosted")
		cmd.Flags().IntVar(&creation.interval, "interval", 60, "Requested measurement interval in seconds (minimum 60)")
		cmd.Flags().IntVar(&creation.timeout, "timeout", 30, "Per-flow execution timeout in seconds")
		cmd.Flags().BoolVar(&creation.paused, "paused", false, "Create with scheduled measurements paused")
		_ = cmd.RegisterFlagCompletionFunc("runner", staticCompletion("cloud", "self_hosted"))
	}
	addProbeFileFlags(state, cmd, &file, &environment, update)
	selectors.register(cmd, state)
	return cmd
}

func newProbeValidateCmd(state *AppState) *cobra.Command {
	var file, environment string
	var update bool
	var selectors probeSelectorFlags
	cmd := quietOnError(
		&cobra.Command{
			Use:               "validate -f <file>",
			Short:             "Validate a probe request offline against the API contract",
			Annotations:       offline(),
			Example:           "  echopoint probe validate -f probe.json\n  echopoint probe validate --update -f update.json",
			Args:              fileFlagArgs(0),
			ValidArgsFunction: cobra.NoFileCompletions,
			RunE: func(cmd *cobra.Command, _ []string) error {
				schema := "CreateProbeRequest"
				if update {
					schema = "UpdateProbeRequest"
				}
				if _, err := readProbeRequest[map[string]any](
					cmd,
					state,
					file,
					schema,
					environment,
					selectors,
				); err != nil {
					return err
				}
				result := struct {
					Valid bool   `json:"valid"`
					Scope string `json:"scope"`
				}{true, "schema; server validates selector ownership, environment, execution limits and health policy"}
				return printStatusResponse(cmd, state, &result, nil, nil)
			},
		},
	)
	cmd.Flags().BoolVar(&update, "update", false, "Validate UpdateProbeRequest including expected_revision")
	addProbeFileFlags(state, cmd, &file, &environment, true)
	selectors.register(cmd, state)
	return cmd
}

func addProbeFileFlags(state *AppState, cmd *cobra.Command, file, environment *string, required bool) {
	addFileFlag(cmd, file, "Complete probe request JSON file; - reads stdin", "json")
	if required {
		_ = cmd.MarkFlagRequired("file")
	}
	cmd.Flags().
		StringVarP(environment, "environment", "e", "", "Override config.environment_key; empty uses each flow's saved default")
	registerEnvironmentFlagCompletion(state, cmd)
}

func newProbeDeleteCmd(state *AppState) *cobra.Command {
	var revision int64
	cmd := quietOnError(&cobra.Command{
		Use:               "delete <probe-id>",
		Short:             "Delete a probe using its current revision",
		Example:           "  echopoint probe delete <probe-id> --expected-revision 3 --yes -o json",
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
			if err := confirmDestructive(cmd, state, "delete", "probe "+id); err != nil {
				return err
			}
			resp, err := state.Client.API().
				DeleteProbeWithResponse(cmd.Context(), id, &api.DeleteProbeParams{ExpectedRevision: revision})
			if err != nil {
				return err
			}
			if resp.StatusCode() != http.StatusNoContent {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}
			result := struct {
				ID      string `json:"id"`
				Deleted bool   `json:"deleted"`
			}{id, true}
			return printStatusResponse(cmd, state, &result, nil, nil)
		},
	})
	addProbeRevisionFlag(cmd, &revision)
	return cmd
}

func newProbeStateCmd(state *AppState, resume bool) *cobra.Command {
	var revision int64
	verb := "pause"
	if resume {
		verb = "resume"
	}
	cmd := quietOnError(
		&cobra.Command{
			Use:               verb + " <probe-id>",
			Short:             "Change scheduled measurement state using the current revision",
			Example:           "  echopoint probe " + verb + " <probe-id> --expected-revision 3 -o json",
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
				request := api.ProbeRevisionRequest{ExpectedRevision: revision}
				if resume {
					resp, err := state.Client.API().ResumeProbeWithResponse(cmd.Context(), id, nil, request)
					if err != nil {
						return err
					}
					return printStatusResponse(cmd, state, resp.JSON200, resp.HTTPResponse, resp.Body)
				}
				resp, err := state.Client.API().PauseProbeWithResponse(cmd.Context(), id, nil, request)
				if err != nil {
					return err
				}
				return printStatusResponse(cmd, state, resp.JSON200, resp.HTTPResponse, resp.Body)
			},
		},
	)
	addProbeRevisionFlag(cmd, &revision)
	return cmd
}

func addProbeRevisionFlag(cmd *cobra.Command, revision *int64) {
	cmd.Flags().Int64Var(revision, "expected-revision", 0, "Current saved revision from probe view")
	_ = cmd.MarkFlagRequired("expected-revision")
}

func validateProbeRevision(revision int64) error {
	if revision < 1 {
		return fmt.Errorf("--expected-revision must be positive")
	}
	return nil
}

func addProbePagination(cmd *cobra.Command, limit, offset *int) {
	cmd.Flags().IntVar(limit, "limit", 20, "Maximum results (1-100)")
	cmd.Flags().IntVar(offset, "offset", 0, "Results to skip")
}

func newProbeSourceOptionsCmd(state *AppState) *cobra.Command {
	var flow, environment string
	cmd := quietOnError(
		&cobra.Command{
			Use:     "source-options",
			Short:   "Inspect the current flow source, effective environment and optional assertion checks",
			Example: "  echopoint probe source-options --flow-id <flow-id> --environment production -o json",
			Args:    cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				if err := requireToken(state); err != nil {
					return err
				}
				flowID, err := resolveFlowID(cmd.Context(), state, flow)
				if err != nil {
					return fmt.Errorf("--flow-id: %w", err)
				}
				resp, err := state.Client.API().
					GetProbeSourceOptionsWithResponse(cmd.Context(), &api.GetProbeSourceOptionsParams{FlowId: flowID, EnvironmentKey: &environment})
				if err != nil {
					return err
				}
				return printStatusResponse(cmd, state, resp.JSON200, resp.HTTPResponse, resp.Body)
			},
		},
	)
	cmd.Flags().StringVar(&flow, "flow-id", "", "Flow ID")
	cmd.Flags().
		StringVarP(&environment, "environment", "e", "", "Environment overlay; empty uses the flow's saved default")
	_ = cmd.MarkFlagRequired("flow-id")
	_ = cmd.RegisterFlagCompletionFunc("flow-id", completeFlowFlag(state))
	registerEnvironmentFlagCompletion(state, cmd)
	return cmd
}
