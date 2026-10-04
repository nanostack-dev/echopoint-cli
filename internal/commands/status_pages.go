package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/client"
	"echopoint-cli/internal/output"
)

const anonymousAnnotation = "echopoint/anonymous"
const annotationEnabled = "true"

func newStatusPagesCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status-pages",
		Short: "Configure, publish and verify the organization's public status page",
	}
	cmd.AddCommand(
		newStatusPageGetCmd(state),
		newStatusPageSaveCmd(state, false),
		newStatusPageSaveCmd(state, true),
		newStatusPagePublishCmd(
			state,
		),
		newStatusPageUnpublishCmd(state),
		newStatusPageBindingCmd(state),
		newStatusPagePublicCmd(state),
	)
	return cmd
}

func printStatusResponse[T any](
	cmd *cobra.Command,
	state *AppState,
	value *T,
	response *http.Response,
	body []byte,
) error {
	if value == nil {
		return formatAPIError(response, body)
	}
	if state.OutputFormat == output.FormatYAML {
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		var document any
		if err := yaml.Unmarshal(data, &document); err != nil {
			return err
		}
		return output.PrintYAML(cmd.OutOrStdout(), document)
	}
	// Nested configuration is emitted as JSON by default so it can be saved without losing fields.
	return output.PrintJSON(cmd.OutOrStdout(), value)
}

func newStatusPageGetCmd(state *AppState) *cobra.Command {
	return &cobra.Command{Use: "get", Short: "Read the shared draft and its publication versions", Args: cobra.NoArgs,
		SilenceUsage: true, SilenceErrors: true, RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			resp, err := state.Client.API().GetStatusPageWithResponse(cmd.Context(), nil)
			if err != nil {
				return err
			}
			return printStatusResponse(cmd, state, resp.JSON200, resp.HTTPResponse, resp.Body)
		}}
}

func readStatusPageRequest(cmd *cobra.Command, path string) (api.SaveStatusPageRequest, error) {
	var request api.SaveStatusPageRequest
	reader := cmd.InOrStdin()
	if path != "-" {
		file, err := os.Open(path)
		if err != nil {
			return request, err
		}
		defer file.Close()
		reader = file
	}
	data, err := io.ReadAll(io.LimitReader(reader, 1_048_577))
	if err != nil {
		return request, err
	}
	if len(data) > 1_048_576 {
		return request, fmt.Errorf("status page input exceeds 1 MiB")
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return request, fmt.Errorf("invalid status page JSON: %w", err)
	}
	spec, err := openapi3.NewLoader().LoadFromData(api.OpenAPISpec)
	if err != nil {
		return request, fmt.Errorf("load status page schema: %w", err)
	}
	if err := spec.Components.Schemas["SaveStatusPageRequest"].Value.VisitJSON(value); err != nil {
		return request, fmt.Errorf("invalid status page request: %w", err)
	}
	if err := json.Unmarshal(data, &request); err != nil {
		return request, err
	}
	if request.ExpectedDraftVersion == 0 && len(request.Slug) > 48 {
		return request, fmt.Errorf("new page address prefix must be at most 48 characters")
	}
	return request, nil
}

func newStatusPageSaveCmd(state *AppState, validateOnly bool) *cobra.Command {
	verb, short := "save <file|->", "Save a complete private draft from JSON; use - for stdin"
	if validateOnly {
		verb, short = "validate <file|->", "Validate draft JSON offline against the API contract"
	}
	cmd := &cobra.Command{
		Use:           verb,
		Short:         short,
		Long:          short + ". For a new page, slug is a 3-48 character prefix; the server adds a unique suffix. For edits and public reads, use the complete slug returned by save/get.",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			request, err := readStatusPageRequest(cmd, args[0])
			if err != nil {
				return err
			}
			if validateOnly {
				result := struct {
					Valid bool   `json:"valid"`
					Scope string `json:"scope"`
				}{true, "schema; server checks binding and policy on save/publish"}
				return printStatusResponse(cmd, state, &result, nil, nil)
			}
			if err := requireToken(state); err != nil {
				return err
			}
			resp, err := state.Client.API().SaveStatusPageWithResponse(cmd.Context(), nil, request)
			if err != nil {
				return err
			}
			return printStatusResponse(cmd, state, resp.JSON200, resp.HTTPResponse, resp.Body)
		},
	}
	if validateOnly {
		cmd.Annotations = offline()
	}
	return cmd
}

func newStatusPagePublishCmd(state *AppState) *cobra.Command {
	var draft, intent int64
	cmd := &cobra.Command{
		Use:           "publish",
		Short:         "Publish the saved draft using the versions returned by get/save",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			if draft < 0 || intent < 0 {
				return fmt.Errorf("expected versions must be nonnegative")
			}
			resp, err := state.Client.API().
				PublishStatusPageWithResponse(cmd.Context(), nil, api.PublishStatusPageRequest{
					ExpectedDraftVersion: draft, ExpectedIntentVersion: intent,
				})
			if err != nil {
				return err
			}
			return printStatusResponse(cmd, state, resp.JSON200, resp.HTTPResponse, resp.Body)
		},
	}
	cmd.Flags().Int64Var(&draft, "expected-draft-version", 0, "Draft version to publish")
	cmd.Flags().Int64Var(&intent, "expected-intent-version", 0, "Current publication intent version")
	_ = cmd.MarkFlagRequired("expected-draft-version")
	_ = cmd.MarkFlagRequired("expected-intent-version")
	return cmd
}

func newStatusPageUnpublishCmd(state *AppState) *cobra.Command {
	var intent int64
	cmd := &cobra.Command{
		Use:           "unpublish",
		Short:         "Withdraw the public page using its current intent version",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			if intent < 0 {
				return fmt.Errorf("expected intent version must be nonnegative")
			}
			resp, err := state.Client.API().
				UnpublishStatusPageWithResponse(cmd.Context(), nil, api.UnpublishStatusPageRequest{ExpectedIntentVersion: intent})
			if err != nil {
				return err
			}
			return printStatusResponse(cmd, state, resp.JSON200, resp.HTTPResponse, resp.Body)
		},
	}
	cmd.Flags().Int64Var(&intent, "expected-intent-version", 0, "Current publication intent version")
	_ = cmd.MarkFlagRequired("expected-intent-version")
	return cmd
}

func newStatusPageBindingCmd(state *AppState) *cobra.Command {
	var schedule, flow string
	cmd := &cobra.Command{
		Use:           "binding-options",
		Short:         "Resolve a monitor's published flow and available checks",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			scheduleID, err := uuid.Parse(schedule)
			if err != nil {
				return fmt.Errorf("invalid schedule ID: %w", err)
			}
			flowID, err := uuid.Parse(flow)
			if err != nil {
				return fmt.Errorf("invalid flow ID: %w", err)
			}
			resp, err := state.Client.API().
				GetStatusPageBindingOptionsWithResponse(cmd.Context(), &api.GetStatusPageBindingOptionsParams{
					ScheduleId: scheduleID, FlowId: flowID,
				})
			if err != nil {
				return err
			}
			return printStatusResponse(cmd, state, resp.JSON200, resp.HTTPResponse, resp.Body)
		},
	}
	cmd.Flags().StringVar(&schedule, "schedule-id", "", "Enabled monitor ID")
	cmd.Flags().StringVar(&flow, "flow-id", "", "Flow selected by the monitor")
	_ = cmd.MarkFlagRequired("schedule-id")
	_ = cmd.MarkFlagRequired("flow-id")
	return cmd
}

func newStatusPagePublicCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   "public <slug>",
		Short: "Read the published projection anonymously, without credentials or organization headers",
		Args: cobra.ExactArgs(
			1,
		),
		SilenceUsage:  true,
		SilenceErrors: true,
		Annotations:   map[string]string{anonymousAnnotation: annotationEnabled},
		RunE: func(cmd *cobra.Command, args []string) error {
			anonymous, err := client.New(state.Client.BaseURL(), "", "", 30*time.Second)
			if err != nil {
				return err
			}
			resp, err := anonymous.API().GetPublicStatusPageWithResponse(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return printStatusResponse(cmd, state, resp.JSON200, resp.HTTPResponse, resp.Body)
		},
	}
}
