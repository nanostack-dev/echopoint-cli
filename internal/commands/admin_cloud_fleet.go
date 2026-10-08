package commands

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/output"

	"github.com/spf13/cobra"
)

func newAdminCloudFleetCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cloud-fleet",
		Short: "View and configure shared Cloud execution capacity",
		Long: `Manage Cloud execution capacity shared by all organizations in one deployment.

The daily launch limit counts accepted launches, including queued jobs, over a
rolling 24-hour window. Setting it to zero pauses new Cloud launches. The global
cap limits claimed Cloud jobs. These settings do not alter organization license
quotas or the limits of Self-hosted and Ephemeral runners.`,
	}
	cmd.AddCommand(newAdminCloudFleetViewCmd(state), newAdminCloudFleetUpdateCmd(state))
	return cmd
}

func newAdminCloudFleetViewCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:     viewVerb,
		Aliases: []string{getVerb, showVerb},
		Short:   "Show fleet limits, rolling usage and queued jobs",
		Example: `  echopoint admin cloud-fleet view --profile dev
  echopoint admin cloud-fleet view --profile default -o json`,
		Args: cobra.NoArgs, SilenceUsage: true, SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireAdminSession(state); err != nil {
				return err
			}
			response, err := state.Client.API().GetCloudFleetWithResponse(cmd.Context(), productAdminRequest)
			if err != nil {
				return fmt.Errorf("read Cloud fleet: %w", err)
			}
			return printCloudFleetStatus(cmd, state, response.JSON200, response.HTTPResponse, response.Body)
		},
	}
}

func newAdminCloudFleetUpdateCmd(state *AppState) *cobra.Command {
	var dailyLaunchLimit, globalCap int32
	var expectedRevision int64
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update shared fleet limits using the revision from view",
		Long: `Update both Cloud fleet limits for the explicitly selected profile.

Read the current revision with 'admin cloud-fleet view' and pass it as
--expected-revision. A concurrent edit returns a conflict; read the latest
settings before retrying. Existing launches remain reserved and queued jobs keep
their budget slots. Setting --daily-launch-limit 0 pauses new Cloud launches.`,
		Example: `  echopoint admin cloud-fleet update --profile dev --expected-revision 0 --daily-launch-limit 200 --global-cap 10 -o json
  echopoint admin cloud-fleet update --profile default --expected-revision 3 --daily-launch-limit 0 --global-cap 10`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if dailyLaunchLimit < 0 || globalCap < 1 || expectedRevision < 0 {
				return errors.New(
					"daily launch limit and expected revision must be nonnegative; global cap must be at least 1",
				)
			}
			profile := cmd.Flag("profile")
			if profile == nil || !profile.Changed || strings.TrimSpace(profile.Value.String()) == "" {
				return errors.New(
					"pass --profile explicitly to select the deployment whose shared fleet limits will change",
				)
			}
			if err := requireAdminSession(state); err != nil {
				return err
			}
			response, err := state.Client.API().UpdateCloudFleetWithResponse(
				cmd.Context(), api.UpdateCloudFleetRequest{
					DailyLaunchLimit: dailyLaunchLimit,
					GlobalCap:        globalCap,
					ExpectedRevision: expectedRevision,
				}, productAdminRequest,
			)
			if err != nil {
				return fmt.Errorf("update Cloud fleet: %w", err)
			}
			return printCloudFleetStatus(cmd, state, response.JSON200, response.HTTPResponse, response.Body)
		},
	}
	cmd.Flags().
		Int32Var(&dailyLaunchLimit, "daily-launch-limit", 0, "Accepted Cloud launches per rolling 24 hours (0 pauses new launches)")
	cmd.Flags().
		Int32Var(&globalCap, "global-cap", 0, "Maximum claimed Cloud jobs shared across organizations (at least 1)")
	cmd.Flags().Int64Var(&expectedRevision, "expected-revision", 0, "Current settings revision returned by view")
	_ = cmd.MarkFlagRequired("daily-launch-limit")
	_ = cmd.MarkFlagRequired("global-cap")
	_ = cmd.MarkFlagRequired("expected-revision")
	return cmd
}

func printCloudFleetStatus(
	cmd *cobra.Command,
	state *AppState,
	status *api.CloudFleetStatus,
	response *http.Response,
	body []byte,
) error {
	if status == nil {
		return formatAPIError(response, body)
	}
	switch state.OutputFormat {
	case output.FormatJSON:
		return output.PrintJSON(cmd.OutOrStdout(), status)
	case output.FormatYAML:
		return output.PrintYAML(cmd.OutOrStdout(), status)
	default:
		return output.PrintTableTo(cmd.OutOrStdout(), []string{"METRIC", "VALUE"}, [][]string{
			{"Profile", state.Profile},
			{"API URL", state.Config.API.BaseURL},
			{"Daily launch limit", fmt.Sprint(status.DailyLaunchLimit)},
			{"Launches in last 24 hours", fmt.Sprint(status.LaunchesLast24h)},
			{"Remaining launches", fmt.Sprint(status.LaunchesRemaining)},
			{"Claimed jobs / global cap", fmt.Sprintf("%d / %d", status.ClaimedJobs, status.GlobalCap)},
			{"Queued jobs", fmt.Sprint(status.QueuedJobs)},
			{"New Cloud launches paused", fmt.Sprint(status.Paused)},
			{"Settings source", string(status.SettingsSource)},
			{"Revision", fmt.Sprint(status.Revision)},
			{"Window start", status.WindowStart.Format(time.RFC3339)},
			{"Next launch available", fleetStatusTime(status.NextLaunchAvailableAt)},
			{"Settings updated", fleetStatusTime(status.SettingsUpdatedAt)},
			{"Snapshot generated", status.GeneratedAt.Format(time.RFC3339)},
		})
	}
}

func fleetStatusTime(value *time.Time) string {
	if value == nil {
		return "-"
	}
	return value.Format(time.RFC3339)
}
