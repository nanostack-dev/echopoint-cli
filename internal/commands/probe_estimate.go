package commands

import (
	"strings"

	"github.com/spf13/cobra"

	"echopoint-cli/internal/api"
)

func newProbeEstimateCmd(state *AppState) *cobra.Command {
	var selectors probeSelectorFlags
	var environment, runner string
	var interval int
	cmd := quietOnError(&cobra.Command{
		Use:     "estimate",
		Short:   "Preview matching flows and forecast executions and HTTP requests without creating a probe",
		Long:    "Resolve a tag selector or explicit flow set and forecast its requested cadence. Static request estimates can be incomplete for branches, retries or modules; the response includes estimate notes and remaining monthly executions.",
		Example: "  echopoint --org <organization-id> probe estimate --tag production --interval 60 --environment production -o json\n  echopoint probe estimate --flow-id <flow-id> --flow-id <other-flow-id> -o json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			value := map[string]any{
				"interval_seconds": interval,
				"runner_type":      runner,
				"environment_key":  strings.TrimSpace(environment),
			}
			if err := selectors.apply(cmd, state, value); err != nil {
				return err
			}
			request, err := validateProbeValue[api.ProbeEstimateRequest](value, "ProbeEstimateRequest")
			if err != nil {
				return err
			}
			if err := requireToken(state); err != nil {
				return err
			}
			resp, err := state.Client.API().EstimateProbeWithResponse(cmd.Context(), nil, request)
			if err != nil {
				return err
			}
			return printStatusResponse(cmd, state, resp.JSON200, resp.HTTPResponse, resp.Body)
		},
	})
	selectors.register(cmd, state)
	cmd.Flags().IntVar(&interval, "interval", 60, "Requested interval in seconds (minimum 60)")
	cmd.Flags().StringVar(&runner, "runner", "cloud", "Where EchoPoint runs flows: cloud or self_hosted")
	cmd.Flags().
		StringVarP(&environment, "environment", "e", "", "Environment overlay; empty uses the flow's saved default")
	_ = cmd.RegisterFlagCompletionFunc("runner", staticCompletion("cloud", "self_hosted"))
	registerEnvironmentFlagCompletion(state, cmd)
	return cmd
}
