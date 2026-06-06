package commands

import (
	"context"
	"fmt"
	"os"
	"strings"

	"echopoint-cli/internal/api"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

const flowTagPageSize = 100

func newFlowsTagCmd(state *AppState) *cobra.Command {
	var (
		addTags    []string
		removeTags []string
		query      string
		matchTags  []string
		matchMode  string
	)

	cmd := &cobra.Command{
		Use:   "tag [<flow-id>...]",
		Short: "Add or remove tags on flows",
		Long: `Add or remove tags on flows.

Select flows either by passing flow IDs, or by a search filter (the same search
that backs the flow list). A search filter is required for search-based
selection — tagging every flow in the organization is intentionally not
supported. Tags are merged with each flow's existing tags (add) or removed from
them; no other flow fields are changed. Tags are normalized (lowercased) and
de-duplicated server-side.

Examples:
  # tag specific flows
  echopoint flows tag <flow-id> <flow-id> --add anchor

  # tag every flow matched by a search filter
  echopoint flows tag --query anchor --add anchor
  echopoint flows tag --match-tag staging --match-mode any --add anchor

  # remove a tag
  echopoint flows tag <flow-id> --remove deprecated`,
		RunE: func(_ *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			if len(addTags) == 0 && len(removeTags) == 0 {
				return fmt.Errorf("provide at least one --add or --remove tag")
			}
			hasSearchFilter := query != "" || len(matchTags) > 0
			if len(args) > 0 && hasSearchFilter {
				return fmt.Errorf("flow IDs cannot be combined with --query/--match-tag")
			}
			if len(args) == 0 && !hasSearchFilter {
				return fmt.Errorf(
					"select flows to tag: pass flow IDs or a search filter (--query/--match-tag); " +
						"tagging every flow in the organization is not supported",
				)
			}
			if matchMode != "" && matchMode != string(api.Any) && matchMode != string(api.All) {
				return fmt.Errorf("invalid --match-mode %q; must be %q or %q", matchMode, api.Any, api.All)
			}

			ctx := context.Background()

			flowIDs := args
			if len(flowIDs) == 0 {
				ids, err := searchFlowIDs(ctx, state, query, matchTags, matchMode)
				if err != nil {
					return err
				}
				flowIDs = ids
				fmt.Fprintf(os.Stdout, "Selected %d flow(s) via search…\n", len(flowIDs))
			}

			var updated, unchanged, failed int
			for _, raw := range flowIDs {
				id, parseErr := uuid.Parse(raw)
				if parseErr != nil {
					fmt.Fprintf(os.Stderr, "skip %q: invalid flow id\n", raw)
					failed++
					continue
				}

				changed, applyErr := applyFlowTags(ctx, state, id, addTags, removeTags)
				if applyErr != nil {
					fmt.Fprintf(os.Stderr, "flow %s: %v\n", id, applyErr)
					failed++
					continue
				}
				if changed {
					updated++
					fmt.Fprintf(os.Stdout, "updated %s\n", id)
				} else {
					unchanged++
				}
			}

			fmt.Fprintf(os.Stdout, "Done. updated: %d, unchanged: %d, failed: %d\n", updated, unchanged, failed)
			if failed > 0 {
				return fmt.Errorf("%d flow(s) failed to update", failed)
			}
			return nil
		},
	}

	cmd.Flags().StringArrayVar(&addTags, "add", nil, "Tag to add (repeatable)")
	cmd.Flags().StringArrayVar(&removeTags, "remove", nil, "Tag to remove (repeatable)")
	cmd.Flags().StringVar(&query, "query", "", "Select flows by full-text search instead of IDs")
	cmd.Flags().StringArrayVar(&matchTags, "match-tag", nil, "Select flows that have this tag (repeatable)")
	cmd.Flags().StringVar(&matchMode, "match-mode", "", `Tag match mode for --match-tag: "any" (default) or "all"`)
	return cmd
}

// searchFlowIDs resolves the flows to tag through POST /flows/search (the same
// search that backs the flow list), paging through all matches. With no filter
// it returns every flow in the organization.
func searchFlowIDs(
	ctx context.Context,
	state *AppState,
	query string,
	matchTags []string,
	matchMode string,
) ([]string, error) {
	params := &api.SearchFlowsParams{XOrganizationID: state.OrganizationID}

	var ids []string
	var offset int32
	for {
		limit := int32(flowTagPageSize)
		pageOffset := offset
		body := api.FlowSearchRequest{
			Pagination: &api.PaginationRequest{Limit: &limit, Offset: &pageOffset},
		}
		if query != "" {
			body.FullTextSearch = &query
		}
		if len(matchTags) > 0 {
			body.Tags = &matchTags
			if matchMode != "" {
				mode := api.TagMatchMode(matchMode)
				body.TagMatchMode = &mode
			}
		}

		resp, err := state.Client.API().SearchFlowsWithResponse(ctx, params, body)
		if err != nil {
			return nil, err
		}
		if resp.JSON200 == nil {
			return nil, formatAPIError(resp.HTTPResponse, resp.Body)
		}
		for _, item := range resp.JSON200.Items {
			ids = append(ids, item.Id.String())
		}
		if len(resp.JSON200.Items) < flowTagPageSize || int64(len(ids)) >= resp.JSON200.Total {
			break
		}
		offset += flowTagPageSize
	}
	return ids, nil
}

// applyFlowTags fetches the flow, merges the add/remove tags into its current
// tags, and updates it only when the set changed. Returns whether it changed.
func applyFlowTags(
	ctx context.Context,
	state *AppState,
	id uuid.UUID,
	add, remove []string,
) (bool, error) {
	getResp, err := state.Client.API().GetFlowWithResponse(ctx, id, nil)
	if err != nil {
		return false, err
	}
	if getResp.JSON200 == nil {
		return false, formatAPIError(getResp.HTTPResponse, getResp.Body)
	}

	next, changed := mergeTags(getResp.JSON200.Tags, add, remove)
	if !changed {
		return false, nil
	}

	updResp, err := state.Client.API().UpdateFlowWithResponse(ctx, id, nil, api.UpdateFlowRequest{
		Tags: &next,
	})
	if err != nil {
		return false, err
	}
	if updResp.JSON200 == nil {
		return false, formatAPIError(updResp.HTTPResponse, updResp.Body)
	}
	return true, nil
}

// mergeTags returns the tag set after adding `add` and removing `remove`, and
// whether it differs from `current`. Inputs are lowercased/trimmed to match the
// canonical stored form so change detection is accurate; the backend re-normalizes
// and sorts on write.
func mergeTags(current, add, remove []string) ([]string, bool) {
	removeSet := make(map[string]struct{}, len(remove))
	for _, tag := range remove {
		removeSet[normalizeTagInput(tag)] = struct{}{}
	}

	present := make(map[string]struct{}, len(current)+len(add))
	result := make([]string, 0, len(current)+len(add))

	appendTag := func(tag string) {
		if tag == "" {
			return
		}
		if _, drop := removeSet[tag]; drop {
			return
		}
		if _, dup := present[tag]; dup {
			return
		}
		present[tag] = struct{}{}
		result = append(result, tag)
	}

	for _, tag := range current {
		appendTag(normalizeTagInput(tag))
	}
	for _, tag := range add {
		appendTag(normalizeTagInput(tag))
	}

	return result, !sameStringSet(current, result)
}

func normalizeTagInput(tag string) string {
	return strings.ToLower(strings.TrimSpace(tag))
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]struct{}, len(a))
	for _, item := range a {
		set[item] = struct{}{}
	}
	for _, item := range b {
		if _, ok := set[item]; !ok {
			return false
		}
	}
	return true
}
