package commands

import (
	"context"

	"github.com/spf13/cobra"

	"echopoint-cli/internal/api"
)

func completeSpecSlugs(state *AppState) completionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return noCompletion()
		}
		return completeFromAPI(state, cmd, toComplete,
			func(ctx context.Context, client *api.ClientWithResponses) []completionCandidate {
				resp, err := client.ListSpecsWithResponse(
					ctx,
					&api.ListSpecsParams{Limit: api.LimitParameter(specPageSize)},
				)
				if err != nil || resp.JSON200 == nil {
					return nil
				}
				candidates := make([]completionCandidate, 0, len(resp.JSON200.Items))
				for _, spec := range resp.JSON200.Items {
					candidates = append(candidates, completionCandidate{
						spec.Slug, "Live " + spec.Live.Version + " · " + oneLine(spec.Title),
					})
				}
				return candidates
			})
	}
}

func registerSpecVersionCompletion(state *AppState, cmd *cobra.Command, flag string) {
	_ = cmd.RegisterFlagCompletionFunc(flag, completeSpecVersions(state))
}

func completeSpecVersions(state *AppState) completionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return noCompletion()
		}
		return completeFromAPI(state, cmd, toComplete,
			func(ctx context.Context, client *api.ClientWithResponses) []completionCandidate {
				resp, err := client.ListSpecVersionsWithResponse(ctx, args[0], &api.ListSpecVersionsParams{
					Limit: api.LimitParameter(specPageSize),
				})
				if err != nil || resp.JSON200 == nil {
					return nil
				}
				candidates := make([]completionCandidate, 0, len(resp.JSON200.Items))
				for i, item := range resp.JSON200.Items {
					description := string(item.Bump) + " · " + formatWhen(item.CreatedAt)
					if i == 0 {
						description = "Live · " + description
					}
					candidates = append(candidates, completionCandidate{item.Version, description})
				}
				return candidates
			})
	}
}
