package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/client"
	"echopoint-cli/internal/output"
)

const anonymousAnnotation = "echopoint/anonymous"
const annotationEnabled = "true"

// offlineAnnotation marks a command that works on local files and never
// needs credentials.
const offlineAnnotation = "echopoint/offline"

func offline() map[string]string {
	return map[string]string{offlineAnnotation: annotationEnabled}
}

// draftAction routes two local CLI commands; it is not an API/domain enum.
type draftAction int

const (
	draftSave draftAction = iota
	draftValidate
)

func newStatusPageCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:     statusPageCommandName,
		Aliases: []string{"status-pages"},
		Short:   "Configure, publish and verify the organization's public status page",
	}
	cmd.AddCommand(
		newStatusPageViewCmd(state),
		newStatusPageDraftCmd(state, draftSave),
		newStatusPageDraftCmd(state, draftValidate),
		newStatusPagePublishCmd(state),
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
		return output.PrintYAML(cmd.OutOrStdout(), value)
	}
	// Nested configuration is emitted as JSON by default so it can be saved without losing fields.
	return output.PrintJSON(cmd.OutOrStdout(), value)
}

func newStatusPageViewCmd(state *AppState) *cobra.Command {
	return &cobra.Command{Use: viewVerb, Aliases: []string{getVerb},
		Short: "Show the shared draft and its publication versions",
		Example: `  echopoint status-page view
  echopoint status-page view -o yaml`,
		Args: cobra.NoArgs, SilenceUsage: true, SilenceErrors: true, RunE: func(cmd *cobra.Command, _ []string) error {
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

func newStatusPageDraftCmd(state *AppState, action draftAction) *cobra.Command {
	var file string
	verb, short := "save -f <file>", "Save a complete private draft from a JSON file (-f -: stdin)"
	example := `  echopoint status-page save -f status-page.json
  cat status-page.json | echopoint status-page save -f -`
	if action == draftValidate {
		verb, short = "validate -f <file>", "Validate a draft JSON file offline against the API contract (-f -: stdin)"
		example = `  echopoint status-page validate -f status-page.json
  cat status-page.json | echopoint status-page validate -f -`
	}
	cmd := &cobra.Command{
		Use:           verb,
		Short:         short,
		Long:          short + ". For a new page, slug is a 3-48 character prefix; the server adds a unique suffix. For edits and public reads, use the complete slug returned by save/view.",
		Example:       example,
		Args:          fileFlagArgs(0),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			request, err := readStatusPageRequest(cmd, file)
			if err != nil {
				return err
			}
			if action == draftValidate {
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
	addFileFlag(cmd, &file, "Draft JSON file; - reads stdin", "json")
	_ = cmd.MarkFlagRequired("file")
	cmd.ValidArgsFunction = cobra.NoFileCompletions
	if action == draftValidate {
		cmd.Annotations = offline()
	}
	return cmd
}

func newStatusPagePublishCmd(state *AppState) *cobra.Command {
	var draft, intent int64
	cmd := &cobra.Command{
		Use:           "publish",
		Short:         "Publish the saved draft using the versions returned by view/save",
		Example:       `  echopoint status-page publish --expected-draft-version 3 --expected-intent-version 2`,
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
		Example:       `  echopoint status-page unpublish --expected-intent-version 4`,
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
			if err := confirmDestructive(cmd, state, "unpublish", "the status page"); err != nil {
				return err
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
		Example:       `  echopoint status-page binding-options --schedule-id <schedule-id> --flow-id <flow-id>`,
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
			flowID, err := resolveFlowID(cmd.Context(), state, flow)
			if err != nil {
				return fmt.Errorf("--flow-id: %w", err)
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
	_ = cmd.RegisterFlagCompletionFunc("flow-id", completeFlowFlag(state))
	_ = cmd.MarkFlagRequired("schedule-id")
	_ = cmd.MarkFlagRequired("flow-id")
	return cmd
}

func newStatusPagePublicCmd(state *AppState) *cobra.Command {
	var key string
	cmd := &cobra.Command{
		Use:   "public [slug]",
		Short: "Read a published page anonymously by legacy slug or permanent organization key",
		Long:  "Read public status without credentials or organization headers. Use the organization_key returned by status-page view/save with --organization-key, or provide an existing slug.",
		Example: `  echopoint status-page public --organization-key <key>
  echopoint status-page public acme-a1b2c3d4e5f6`,
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		Annotations:   map[string]string{anonymousAnnotation: annotationEnabled},
		RunE: func(cmd *cobra.Command, args []string) error {
			if (key == "" && len(args) != 1) || (key != "" && len(args) != 0) {
				return fmt.Errorf("provide either a slug or --organization-key")
			}
			if key != "" && !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(key) {
				return fmt.Errorf("organization key must contain 40 lowercase hexadecimal characters")
			}
			anonymous, err := client.New(state.Client.BaseURL(), "", "", 30*time.Second)
			if err != nil {
				return err
			}
			if key != "" {
				resp, err := anonymous.API().GetPublicStatusPageByOrganizationWithResponse(cmd.Context(), key)
				if err != nil {
					return err
				}
				return printStatusResponse(cmd, state, resp.JSON200, resp.HTTPResponse, resp.Body)
			}
			resp, err := anonymous.API().GetPublicStatusPageWithResponse(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return printStatusResponse(cmd, state, resp.JSON200, resp.HTTPResponse, resp.Body)
		},
	}
	cmd.Flags().StringVar(&key, "organization-key", "", "Permanent organization key returned by view/save")
	return cmd
}
