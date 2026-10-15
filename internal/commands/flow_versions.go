package commands

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/output"
)

func newFlowPublishCmd(state *AppState) *cobra.Command {
	return quietOnError(&cobra.Command{
		Use: "publish <flow-id>", Short: "Snapshot a flow as an immutable published version",
		Example: `  echopoint flow publish <flow-id> -o json`, Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			id, err := resolveFlowID(cmd.Context(), state, args[0])
			if err != nil {
				return err
			}
			resp, err := state.Client.API().PublishFlowWithResponse(cmd.Context(), id, nil)
			if err != nil {
				return err
			}
			return printStatusResponse(cmd, state, resp.JSON201, resp.HTTPResponse, resp.Body)
		},
	})
}

func newFlowVersionCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:     versionCommandName,
		Aliases: []string{"versions"},
		Short:   "Discover immutable published flow versions",
	}
	cmd.AddCommand(newFlowVersionListCmd(state), newFlowVersionViewCmd(state))
	return cmd
}

func newFlowVersionListCmd(state *AppState) *cobra.Command {
	var pages pageFlags
	cmd := quietOnError(&cobra.Command{
		Use: "list <flow-id>", Short: "List published versions of a flow, newest first",
		Long: `List the published versions of a flow, newest first, as JSON (or YAML with -o yaml).

A page ends with next_cursor, which --cursor takes to continue the listing; --all
follows it to the oldest version.`,
		Example: `  echopoint flow version list <flow-id> --limit 20 -o json
  echopoint flow version list <flow-id> --cursor <next_cursor>
  echopoint flow version list <flow-id> --all`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			if err := pages.validate(); err != nil {
				return err
			}
			id, err := resolveFlowID(cmd.Context(), state, args[0])
			if err != nil {
				return err
			}
			page, err := fetchSearch(cmd.Context(), cmd, &pages, searchFlowVersions(state, id))
			if err != nil {
				return err
			}
			if state.OutputFormat == output.FormatYAML {
				return output.PrintYAML(cmd.OutOrStdout(), page)
			}
			// Published versions are nested documents: they print as JSON unless YAML is asked for.
			if err := output.PrintJSON(cmd.OutOrStdout(), page); err != nil {
				return err
			}
			printContinueHint(cmd.ErrOrStderr(), page.NextCursor)
			return nil
		},
	})
	pages.bind(cmd, defaultListLimit)
	return cmd
}

// searchFlowVersions reads one page of the published versions of a flow, newest first.
func searchFlowVersions(state *AppState, flowID uuid.UUID) pageFetcher[api.FlowVersion] {
	return func(ctx context.Context, pagination *api.PaginationRequest) (searchPage[api.FlowVersion], error) {
		resp, err := state.Client.API().SearchFlowVersionsWithResponse(
			ctx, flowID, nil, api.FlowVersionSearchRequest{Pagination: pagination},
		)
		if err != nil {
			return searchPage[api.FlowVersion]{}, err
		}
		if resp.JSON200 == nil {
			return searchPage[api.FlowVersion]{}, formatAPIError(resp.HTTPResponse, resp.Body)
		}
		return newSearchPage(resp.JSON200.Items, resp.JSON200.Total, resp.JSON200.NextCursor), nil
	}
}

func newFlowVersionViewCmd(state *AppState) *cobra.Command {
	return quietOnError(&cobra.Command{
		Use:               "view <flow-id> <version-id>",
		Aliases:           []string{getVerb, showVerb},
		Short:             "Show a published version and its assertion definitions",
		Example:           `  echopoint flow version view <flow-id> <version-id> -o json`,
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeFlowVersionArgs(state),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			id, err := resolveFlowID(cmd.Context(), state, args[0])
			if err != nil {
				return err
			}
			version, err := uuid.Parse(args[1])
			if err != nil {
				return fmt.Errorf("invalid version ID: %w", err)
			}
			resp, err := state.Client.API().GetFlowVersionWithResponse(cmd.Context(), id, version, nil)
			if err != nil {
				return err
			}
			return printStatusResponse(cmd, state, resp.JSON200, resp.HTTPResponse, resp.Body)
		},
	})
}

func completeFlowVersionArgs(state *AppState) completionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return completeFlowArgs(state, 1)(cmd, args, toComplete)
		}
		if len(args) != 1 {
			return noCompletion()
		}
		id, err := resolveFlowID(cmd.Context(), state, args[0])
		if err != nil {
			return noCompletion()
		}
		return completeFromAPI(
			state,
			cmd,
			toComplete,
			func(ctx context.Context, _ *api.ClientWithResponses) []completionCandidate {
				versions := walkSome(ctx, completionMaxPages, searchFlowVersions(state, id))
				candidates := make([]completionCandidate, 0, len(versions))
				for _, version := range versions {
					candidates = append(
						candidates,
						completionCandidate{version.Id.String(), "published " + formatWhen(version.PublishedAt)},
					)
				}
				return candidates
			},
		)
	}
}
