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
			func(ctx context.Context, _ *api.ClientWithResponses) []completionCandidate {
				specs := walkSome(ctx, completionMaxPages, searchSpecs(state))
				candidates := make([]completionCandidate, 0, len(specs))
				for _, spec := range specs {
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
			func(ctx context.Context, _ *api.ClientWithResponses) []completionCandidate {
				versions := walkSome(ctx, completionMaxPages, searchSpecVersions(state, args[0]))
				candidates := make([]completionCandidate, 0, len(versions))
				for i, item := range versions {
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
