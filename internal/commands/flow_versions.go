package commands

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"echopoint-cli/internal/api"
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
	var limit, offset int
	cmd := quietOnError(&cobra.Command{
		Use: "list <flow-id>", Short: "List published versions of a flow",
		Example: `  echopoint flow version list <flow-id> --limit 20 -o json`, Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			if err := validateAPIPagination(limit, offset); err != nil {
				return err
			}
			id, err := resolveFlowID(cmd.Context(), state, args[0])
			if err != nil {
				return err
			}
			resp, err := state.Client.API().ListFlowVersionsWithResponse(cmd.Context(), id, &api.ListFlowVersionsParams{
				Limit: api.LimitParameter(limit), Offset: api.OffsetParameter(offset),
			})
			if err != nil {
				return err
			}
			return printStatusResponse(cmd, state, resp.JSON200, resp.HTTPResponse, resp.Body)
		},
	})
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum results (1-100)")
	cmd.Flags().IntVar(&offset, "offset", 0, "Results to skip")
	return cmd
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
			func(ctx context.Context, cli *api.ClientWithResponses) []completionCandidate {
				resp, err := cli.ListFlowVersionsWithResponse(
					ctx,
					id,
					&api.ListFlowVersionsParams{Limit: completionPageSize},
				)
				if err != nil || resp.JSON200 == nil {
					return nil
				}
				candidates := make([]completionCandidate, 0, len(resp.JSON200.Items))
				for _, version := range resp.JSON200.Items {
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

func validateAPIPagination(limit, offset int) error {
	if limit < 1 || limit > 100 {
		return fmt.Errorf("--limit must be between 1 and 100")
	}
	if offset < 0 {
		return fmt.Errorf("--offset must be nonnegative")
	}
	return nil
}
