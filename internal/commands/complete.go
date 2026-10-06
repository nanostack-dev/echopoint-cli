package commands

import (
	"context"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/config"
)

var completionTimeout = 2 * time.Second

const (
	completionPageSize       = 100
	recentExecutionsComplete = 20
)

type completionFunc = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective)

// completionCandidate is one thing the shell may insert, with the description
// it shows beside it.
type completionCandidate struct {
	value       string
	description string
}

// completeFromAPI answers a completion from one fetch against the API. It gives
// up after completionTimeout and stays silent on any failure: a completion must
// never print an error into the user's prompt.
func completeFromAPI(
	state *AppState,
	cmd *cobra.Command,
	toComplete string,
	fetch func(ctx context.Context, client *api.ClientWithResponses) []completionCandidate,
) ([]string, cobra.ShellCompDirective) {
	ctx, cancel := context.WithTimeout(context.Background(), completionTimeout)
	defer cancel()
	client := completionClient(state, cmd)
	if client == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return formatCompletions(fetch(ctx, client), toComplete), cobra.ShellCompDirectiveNoFileComp
}

func formatCompletions(candidates []completionCandidate, toComplete string) []string {
	var completions []string
	for _, candidate := range candidates {
		if !strings.HasPrefix(candidate.value, toComplete) {
			continue
		}
		if candidate.description == "" {
			completions = append(completions, candidate.value)
			continue
		}
		completions = append(completions, cobra.CompletionWithDesc(candidate.value, candidate.description))
	}
	return completions
}

func completionClient(state *AppState, cmd *cobra.Command) *api.ClientWithResponses {
	// the root's setup does not run for a completion request
	if state.Configure != nil && state.Configure(cmd) != nil {
		return nil
	}
	if requireToken(state) != nil || state.Client == nil {
		return nil
	}
	return state.Client.API()
}

func noCompletion() ([]string, cobra.ShellCompDirective) {
	return nil, cobra.ShellCompDirectiveNoFileComp
}

// completeFlowArgs completes the flow ids a command takes as its first
// arguments: leading of them, or every argument when leading is negative. A
// flow shows as <id>\t<name> · <folder>.
func completeFlowArgs(state *AppState, leading int) completionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if leading >= 0 && len(args) >= leading {
			return noCompletion()
		}
		return completeFromAPI(state, cmd, toComplete, fetchFlowCandidates)
	}
}

func fetchFlowCandidates(ctx context.Context, client *api.ClientWithResponses) []completionCandidate {
	resp, err := client.ListFlowsWithResponse(ctx, &api.ListFlowsParams{Limit: api.LimitParameter(completionPageSize)})
	if err != nil || resp.JSON200 == nil {
		return nil
	}
	folders := newFolderIndex(nil)
	if foldersResp, err := client.ListFlowFoldersWithResponse(ctx, nil); err == nil && foldersResp.JSON200 != nil {
		folders = newFolderIndex(foldersResp.JSON200.Items)
	}
	candidates := make([]completionCandidate, 0, len(resp.JSON200.Items))
	for _, flow := range resp.JSON200.Items {
		description := oneLine(flow.Name)
		if flow.FolderId != nil {
			if path := folders.path(*flow.FolderId); path != "" {
				description += " · " + path
			}
		}
		candidates = append(candidates, completionCandidate{flow.Id.String(), description})
	}
	return candidates
}

// completeExecutionArgs completes a flow, then an execution of the flow already
// typed: its recent executions, as <id>\t<status> · <when>.
func completeExecutionArgs(state *AppState) completionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		switch len(args) {
		case 0:
			return completeFromAPI(state, cmd, toComplete, fetchFlowCandidates)
		case 1:
			return completeFromAPI(state, cmd, toComplete,
				func(ctx context.Context, client *api.ClientWithResponses) []completionCandidate {
					flowID, err := resolveFlowID(ctx, state, args[0])
					if err != nil {
						return nil
					}
					return fetchExecutionCandidates(ctx, client, flowID)
				})
		}
		return noCompletion()
	}
}

func fetchExecutionCandidates(
	ctx context.Context, client *api.ClientWithResponses, flowID uuid.UUID,
) []completionCandidate {
	resp, err := client.ListFlowExecutionsWithResponse(ctx, flowID, &api.ListFlowExecutionsParams{
		Limit: api.LimitParameter(recentExecutionsComplete),
	})
	if err != nil || resp.JSON200 == nil {
		return nil
	}
	candidates := make([]completionCandidate, 0, len(resp.JSON200.Items))
	for _, execution := range resp.JSON200.Items {
		candidates = append(candidates, completionCandidate{
			execution.Id.String(), string(execution.Status) + " · " + formatWhen(execution.StartedAt),
		})
	}
	return candidates
}

// completeCollectionArgs completes the id of a collection as <id>\t<name>.
func completeCollectionArgs(state *AppState) completionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return noCompletion()
		}
		return completeFromAPI(state, cmd, toComplete,
			func(ctx context.Context, client *api.ClientWithResponses) []completionCandidate {
				resp, err := client.ListCollectionsWithResponse(ctx, &api.ListCollectionsParams{
					Limit: api.LimitParameter(completionPageSize),
				})
				if err != nil || resp.JSON200 == nil {
					return nil
				}
				candidates := make([]completionCandidate, 0, len(resp.JSON200.Items))
				for _, collection := range resp.JSON200.Items {
					candidates = append(
						candidates,
						completionCandidate{collection.Id.String(), oneLine(collection.Name)},
					)
				}
				return candidates
			})
	}
}

// completeFolders completes the paths of the flow folders, plus any reserved
// destination the flag also accepts, such as "root". A folder whose path does not
// resolve back to it alone, because a name holds a "/" or a sibling shares its
// name, completes as its id, with the path as the description.
func completeFolders(state *AppState, reserved ...string) completionFunc {
	return func(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return completeFromAPI(state, cmd, toComplete,
			func(ctx context.Context, client *api.ClientWithResponses) []completionCandidate {
				resp, err := client.ListFlowFoldersWithResponse(ctx, nil)
				if err != nil || resp.JSON200 == nil {
					return nil
				}
				var candidates []completionCandidate
				for _, name := range reserved {
					candidates = append(candidates, completionCandidate{value: name})
				}
				folders := newFolderIndex(resp.JSON200.Items)
				folders.walk(uuid.Nil, 0, func(folder api.FlowFolder, _ int) {
					path := folders.path(folder.Id)
					if resolved, err := folders.resolve(
						path,
					); err != nil || resolved != folder.Id ||
						isReserved(path, reserved) {
						candidates = append(candidates, completionCandidate{folder.Id.String(), path})
						return
					}
					candidates = append(candidates, completionCandidate{value: path})
				})
				return candidates
			})
	}
}

func isReserved(path string, reserved []string) bool {
	return slices.ContainsFunc(reserved, func(name string) bool { return strings.EqualFold(name, path) })
}

func completeFolderArg(state *AppState) completionFunc {
	folders := completeFolders(state)
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return noCompletion()
		}
		return folders(cmd, args, toComplete)
	}
}

// completeEnvironments completes the names of the organization's environments.
func completeEnvironments(state *AppState) completionFunc {
	return func(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return completeFromAPI(state, cmd, toComplete,
			func(ctx context.Context, client *api.ClientWithResponses) []completionCandidate {
				resp, err := client.ListEnvironmentsWithResponse(ctx, nil)
				if err != nil || resp.JSON200 == nil {
					return nil
				}
				candidates := make([]completionCandidate, 0, len(resp.JSON200.Items))
				for _, environment := range resp.JSON200.Items {
					candidates = append(candidates, completionCandidate{value: environment.Name})
				}
				return candidates
			})
	}
}

func completeEnvironmentArg(state *AppState) completionFunc {
	environments := completeEnvironments(state)
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return noCompletion()
		}
		return environments(cmd, args, toComplete)
	}
}

// completeProfiles completes the profile names in the config, as <name>\t<API URL>.
// The default profile is left out where it cannot be the answer.
func completeProfiles(includeDefault bool, positional bool) completionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if positional && len(args) > 0 {
			return noCompletion()
		}
		store, err := loadProfileStore(cmd)
		if err != nil {
			return noCompletion()
		}
		var candidates []completionCandidate
		if includeDefault {
			candidates = append(candidates, profileCandidate(&store, config.DefaultProfile))
		}
		for _, name := range store.ProfileNames() {
			candidates = append(candidates, profileCandidate(&store, name))
		}
		return formatCompletions(candidates, toComplete), cobra.ShellCompDirectiveNoFileComp
	}
}

func profileCandidate(store *config.Store, name string) completionCandidate {
	resolved, err := store.Resolve(name)
	if err != nil {
		return completionCandidate{value: name}
	}
	return completionCandidate{name, resolved.API.BaseURL}
}

// loadProfileStore reads the config a command run would: the --config typed so
// far, else ECHOPOINT_CONFIG, else the user's own.
func loadProfileStore(cmd *cobra.Command) (config.Store, error) {
	path, _ := cmd.Flags().GetString("config")
	if path == "" {
		path = os.Getenv("ECHOPOINT_CONFIG")
	}
	if path == "" {
		store, _, err := config.LoadStore()
		return store, err
	}
	store, _, err := config.LoadStoreFrom(path)
	return store, err
}

func registerEnvironmentFlagCompletion(state *AppState, cmd *cobra.Command) {
	_ = cmd.RegisterFlagCompletionFunc("environment", completeEnvironments(state))
}

func registerFolderFlagCompletion(state *AppState, cmd *cobra.Command, flag string, reserved ...string) {
	_ = cmd.RegisterFlagCompletionFunc(flag, completeFolders(state, reserved...))
}

func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
