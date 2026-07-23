package commands

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/output"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

const (
	// folderPathSeparator joins folder names into a path such as "Anchor/Identity".
	// A folder whose name contains the separator can only be addressed by id.
	folderPathSeparator = "/"

	// uncategorizedRef is the reserved destination that leaves flows without a folder.
	uncategorizedRef = "uncategorized"

	// rootRef is the reserved destination that puts a folder at the tree root.
	rootRef = "root"

	// folderSearchPageSize pages the flow search that backs folder flow counts.
	folderSearchPageSize = 100

	// maxFolderDepth bounds tree walks so a malformed tree cannot loop forever.
	maxFolderDepth = 32
)

// newFlowFolderCmd creates the folder subcommand for flows.
func newFlowFolderCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "folder",
		Short: "Manage flow folders",
		Long: `Manage the organization's flow folder tree.

Folders are addressed either by id or by a "/"-separated path of folder names
from the tree root, so commands read the way the Flow Library looks:

  echopoint flows folder create "Anchor/Identity"
  echopoint flows folder list
  echopoint flows move --to "Anchor/Identity" --match-tag anchor

Path segments are matched case-insensitively; an ambiguous name must be
addressed by id instead.`,
	}

	cmd.AddCommand(
		newFlowFolderListCmd(state),
		newFlowFolderCreateCmd(state),
		newFlowFolderRenameCmd(state),
		newFlowFolderMoveCmd(state),
		newFlowFolderDeleteCmd(state),
	)

	return cmd
}

func newFlowFolderListCmd(state *AppState) *cobra.Command {
	var withCounts bool

	cmd := &cobra.Command{
		Use:   listVerb,
		Short: "List flow folders as a tree",
		Long: `List the organization's flow folders as an indented tree.

The trailing "(uncategorized)" row counts flows that sit outside every folder.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			ctx := context.Background()
			folders, err := fetchFlowFolders(ctx, state)
			if err != nil {
				return err
			}

			switch state.OutputFormat {
			case output.FormatJSON:
				return output.PrintJSON(os.Stdout, folders)
			case output.FormatYAML:
				return output.PrintYAML(os.Stdout, folders)
			}

			var (
				counts        map[uuid.UUID]int
				uncategorized int
			)
			if withCounts {
				counts, uncategorized, err = flowCountsByFolder(ctx, state)
				if err != nil {
					return err
				}
			}

			idx := newFolderIndex(folders)
			rows := make([][]string, 0, len(folders)+1)
			idx.walk(uuid.Nil, 0, func(folder api.FlowFolder, depth int) {
				rows = append(rows, []string{
					strings.Repeat("  ", depth) + folder.Name,
					folder.Id.String(),
					folderCountCell(withCounts, counts[folder.Id]),
				})
			})
			rows = append(rows, []string{"(uncategorized)", "", folderCountCell(withCounts, uncategorized)})

			fmt.Fprintf(os.Stdout, "Folders: %d\n", len(folders))
			return output.PrintTable([]string{columnName, columnID, "Flows"}, rows)
		},
	}

	cmd.Flags().BoolVar(&withCounts, "counts", true, "Include the number of flows in each folder")
	return cmd
}

func newFlowFolderCreateCmd(state *AppState) *cobra.Command {
	var parentRef string

	cmd := &cobra.Command{
		Use:   "create <path>",
		Short: "Create a flow folder, creating missing parents along the path",
		Args:  cobra.ExactArgs(1),
		Long: `Create a flow folder.

The argument is a "/"-separated path; every missing segment is created and every
existing segment is reused, so the command is safe to re-run.

Examples:
  echopoint flows folder create "Anchor"
  echopoint flows folder create "Anchor/Identity/Roles"
  echopoint flows folder create "Identity" --parent Anchor`,
		RunE: func(_ *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			segments := splitFolderPath(args[0])
			if len(segments) == 0 {
				return fmt.Errorf("provide a folder name or path")
			}

			ctx := context.Background()
			idx, err := loadFolderIndex(ctx, state)
			if err != nil {
				return err
			}

			parent := uuid.Nil
			if parentRef != "" {
				parent, err = idx.resolve(parentRef)
				if err != nil {
					return err
				}
			}

			for _, segment := range segments {
				id, matches := idx.lookupChild(parent, segment)
				if matches > 1 {
					return fmt.Errorf(
						"%d folders named %q under %s; rename one or use the folder id",
						matches, segment, idx.describe(parent),
					)
				}
				if matches == 1 {
					fmt.Fprintf(os.Stdout, "exists  %s\n", idx.path(id))
					parent = id
					continue
				}

				created, createErr := createFlowFolder(ctx, state, segment, parent)
				if createErr != nil {
					return createErr
				}
				idx.add(*created)
				fmt.Fprintf(os.Stdout, "created %s  %s\n", idx.path(created.Id), created.Id)
				parent = created.Id
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&parentRef, "parent", "", "Existing folder the path is created under (id or path)")
	return cmd
}

func newFlowFolderRenameCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rename <folder> <new-name>",
		Short: "Rename a flow folder",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			name := strings.TrimSpace(args[1])
			if name == "" {
				return fmt.Errorf("provide a new folder name")
			}

			ctx := context.Background()
			idx, err := loadFolderIndex(ctx, state)
			if err != nil {
				return err
			}
			id, err := idx.resolve(args[0])
			if err != nil {
				return err
			}

			resp, err := state.Client.API().UpdateFlowFolderWithResponse(
				ctx, id, nil, api.UpdateFlowFolderRequest{Name: &name},
			)
			if err != nil {
				return err
			}
			if resp.JSON200 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}

			idx.add(*resp.JSON200)
			fmt.Fprintf(os.Stdout, "renamed %s  %s\n", idx.path(id), id)
			return nil
		},
	}

	return cmd
}

func newFlowFolderMoveCmd(state *AppState) *cobra.Command {
	var parentRef string

	cmd := &cobra.Command{
		Use:   "move <folder>",
		Short: "Move a flow folder under a different parent",
		Args:  cobra.ExactArgs(1),
		Long: `Move a folder — and everything under it — beneath a different parent.

Pass --to root to move the folder back to the tree root. The server rejects a
move that would put a folder inside itself or exceed the folder depth limit.

Examples:
  echopoint flows folder move "Identity" --to "Anchor"
  echopoint flows folder move "Anchor/Identity" --to root`,
		RunE: func(_ *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			ctx := context.Background()
			idx, err := loadFolderIndex(ctx, state)
			if err != nil {
				return err
			}
			id, err := idx.resolve(args[0])
			if err != nil {
				return err
			}

			var parentID *uuid.UUID
			if !isRootRef(parentRef) {
				parent, resolveErr := idx.resolve(parentRef)
				if resolveErr != nil {
					return resolveErr
				}
				parentID = &parent
			}

			resp, err := state.Client.API().MoveFlowFolderWithResponse(
				ctx, id, nil, api.MoveFlowFolderRequest{ParentId: parentID},
			)
			if err != nil {
				return err
			}
			if resp.JSON200 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}

			idx.add(*resp.JSON200)
			fmt.Fprintf(os.Stdout, "moved %s  %s\n", idx.path(id), id)
			return nil
		},
	}

	cmd.Flags().StringVar(&parentRef, "to", "", `Destination parent folder (id or path), or "root"`)
	_ = cmd.MarkFlagRequired("to")
	return cmd
}

func newFlowFolderDeleteCmd(state *AppState) *cobra.Command {
	var (
		deleteFlows bool
		confirmed   bool
	)

	cmd := &cobra.Command{
		Use:   "delete <folder>",
		Short: "Delete a flow folder and its descendants",
		Args:  cobra.ExactArgs(1),
		Long: `Delete a folder and every folder under it.

By default the flows inside the deleted subtree survive and become
uncategorized. With --delete-flows they are permanently deleted along with their
execution history; that is irreversible, so it also requires --yes.`,
		RunE: func(_ *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			if deleteFlows && !confirmed {
				return fmt.Errorf(
					"--delete-flows permanently deletes every flow in the subtree with its execution history; " +
						"pass --yes to confirm",
				)
			}

			ctx := context.Background()
			idx, err := loadFolderIndex(ctx, state)
			if err != nil {
				return err
			}
			id, err := idx.resolve(args[0])
			if err != nil {
				return err
			}
			path := idx.path(id)

			params := &api.DeleteFlowFolderParams{
				XOrganizationID: state.OrganizationID,
				DeleteFlows:     &deleteFlows,
			}
			resp, err := state.Client.API().DeleteFlowFolderWithResponse(ctx, id, params)
			if err != nil {
				return err
			}
			if resp.JSON200 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}

			result := resp.JSON200
			fmt.Fprintf(os.Stdout, "deleted %s\n", path)
			fmt.Fprintf(os.Stdout, "folders removed: %d\n", result.DeletedFolders)
			fmt.Fprintf(os.Stdout, "flows uncategorized: %d\n", result.UncategorizedFlows)
			fmt.Fprintf(os.Stdout, "flows deleted: %d\n", result.DeletedFlows)
			return nil
		},
	}

	cmd.Flags().BoolVar(&deleteFlows, "delete-flows", false, "Also permanently delete every flow in the subtree")
	cmd.Flags().BoolVar(&confirmed, "yes", false, "Confirm the irreversible --delete-flows deletion")
	return cmd
}

// newFlowsMoveCmd moves flows between folders. It lives with the folder
// commands because the destination is a folder reference.
func newFlowsMoveCmd(state *AppState) *cobra.Command {
	var (
		destination string
		create      bool
		query       string
		matchTags   []string
		matchMode   string
	)

	cmd := &cobra.Command{
		Use:   "move [<flow-id>...]",
		Short: "Move flows into a folder",
		Long: `Move flows into a folder in a single server-side transaction.

Select the flows either by passing flow IDs, or by a search filter (the same
search that backs the flow list), in which case every matching flow moves — not
just the first page. A filter is required for search-based selection: moving
every flow in the organization is intentionally not supported.

Examples:
  # move specific flows
  echopoint flows move <flow-id> <flow-id> --to "Anchor/Identity"

  # move every flow carrying a tag, creating the destination if needed
  echopoint flows move --match-tag anchor --to "Anchor" --create

  # pull flows back out of every folder
  echopoint flows move <flow-id> --to uncategorized`,
		RunE: func(_ *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			hasSearchFilter := query != "" || len(matchTags) > 0
			if len(args) > 0 && hasSearchFilter {
				return fmt.Errorf("flow IDs cannot be combined with --query/--match-tag")
			}
			if len(args) == 0 && !hasSearchFilter {
				return fmt.Errorf(
					"select flows to move: pass flow IDs or a search filter (--query/--match-tag); " +
						"moving every flow in the organization is not supported",
				)
			}
			if matchMode != "" && matchMode != string(api.Any) && matchMode != string(api.All) {
				return fmt.Errorf("invalid --match-mode %q; must be %q or %q", matchMode, api.Any, api.All)
			}

			ctx := context.Background()
			folderID, err := resolveMoveDestination(ctx, state, destination, create)
			if err != nil {
				return err
			}

			body := api.BulkMoveFlowsRequest{FolderId: folderID}
			if err := applyMoveSelector(&body, args, query, matchTags, matchMode); err != nil {
				return err
			}

			resp, err := state.Client.API().BulkMoveFlowsWithResponse(ctx, nil, body)
			if err != nil {
				return err
			}
			if resp.JSON200 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}

			fmt.Fprintf(
				os.Stdout,
				"Moved %d flow(s) to %s\n",
				resp.JSON200.MovedFlows,
				describeDestination(destination),
			)
			return nil
		},
	}

	cmd.Flags().StringVar(&destination, "to", "", `Destination folder (id or path), or "uncategorized"`)
	cmd.Flags().BoolVar(&create, "create", false, "Create the destination folder path when it does not exist")
	cmd.Flags().StringVar(&query, "query", "", "Select flows by full-text search instead of IDs")
	cmd.Flags().StringArrayVar(&matchTags, "match-tag", nil, "Select flows that have this tag (repeatable)")
	cmd.Flags().StringVar(&matchMode, "match-mode", "", `Tag match mode for --match-tag: "any" (default) or "all"`)
	_ = cmd.MarkFlagRequired("to")
	return cmd
}

// applyMoveSelector fills in the half of the bulk-move body that selects the
// flows: an explicit id set when ids were passed, otherwise the search filter.
// The two are mutually exclusive server-side.
func applyMoveSelector(
	body *api.BulkMoveFlowsRequest,
	flowIDs []string,
	query string,
	matchTags []string,
	matchMode string,
) error {
	if len(flowIDs) > 0 {
		ids := make([]uuid.UUID, 0, len(flowIDs))
		for _, raw := range flowIDs {
			id, err := uuid.Parse(raw)
			if err != nil {
				return fmt.Errorf("invalid flow id %q", raw)
			}
			ids = append(ids, id)
		}
		body.FlowIds = &ids
		return nil
	}

	filter := api.FlowSearchRequest{}
	if query != "" {
		filter.FullTextSearch = &query
	}
	if len(matchTags) > 0 {
		filter.Tags = &matchTags
		if matchMode != "" {
			mode := api.TagMatchMode(matchMode)
			filter.TagMatchMode = &mode
		}
	}
	body.Filter = &filter
	return nil
}

// resolveMoveDestination turns the --to reference into the folder id the bulk
// move should write, or nil for the uncategorized bucket. With create it
// materializes a missing path instead of failing.
func resolveMoveDestination(
	ctx context.Context,
	state *AppState,
	destination string,
	create bool,
) (*uuid.UUID, error) {
	if isUncategorizedRef(destination) {
		return nil, nil
	}

	idx, err := loadFolderIndex(ctx, state)
	if err != nil {
		return nil, err
	}

	if !create {
		id, resolveErr := idx.resolve(destination)
		if resolveErr != nil {
			return nil, resolveErr
		}
		return &id, nil
	}

	segments := splitFolderPath(destination)
	if len(segments) == 0 {
		return nil, fmt.Errorf("provide a destination folder")
	}

	parent := uuid.Nil
	for _, segment := range segments {
		id, matches := idx.lookupChild(parent, segment)
		if matches > 1 {
			return nil, fmt.Errorf(
				"%d folders named %q under %s; use the folder id",
				matches, segment, idx.describe(parent),
			)
		}
		if matches == 1 {
			parent = id
			continue
		}
		created, createErr := createFlowFolder(ctx, state, segment, parent)
		if createErr != nil {
			return nil, createErr
		}
		idx.add(*created)
		fmt.Fprintf(os.Stdout, "created %s  %s\n", idx.path(created.Id), created.Id)
		parent = created.Id
	}
	return &parent, nil
}

func describeDestination(destination string) string {
	if isUncategorizedRef(destination) {
		return "the uncategorized bucket"
	}
	return fmt.Sprintf("%q", strings.Trim(strings.TrimSpace(destination), folderPathSeparator))
}

func isUncategorizedRef(ref string) bool {
	return strings.EqualFold(strings.TrimSpace(ref), uncategorizedRef)
}

func isRootRef(ref string) bool {
	trimmed := strings.TrimSpace(ref)
	return trimmed == "" || trimmed == folderPathSeparator || strings.EqualFold(trimmed, rootRef)
}

// splitFolderPath splits a "/"-separated folder path into its non-empty,
// trimmed segments.
func splitFolderPath(path string) []string {
	segments := make([]string, 0, 4)
	for segment := range strings.SplitSeq(path, folderPathSeparator) {
		if trimmed := strings.TrimSpace(segment); trimmed != "" {
			segments = append(segments, trimmed)
		}
	}
	return segments
}

func folderCountCell(withCounts bool, count int) string {
	if !withCounts {
		return ""
	}
	return strconv.Itoa(count)
}

func fetchFlowFolders(ctx context.Context, state *AppState) ([]api.FlowFolder, error) {
	resp, err := state.Client.API().ListFlowFoldersWithResponse(ctx, nil)
	if err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		return nil, formatAPIError(resp.HTTPResponse, resp.Body)
	}
	return resp.JSON200.Items, nil
}

func loadFolderIndex(ctx context.Context, state *AppState) (*folderIndex, error) {
	folders, err := fetchFlowFolders(ctx, state)
	if err != nil {
		return nil, err
	}
	return newFolderIndex(folders), nil
}

func createFlowFolder(ctx context.Context, state *AppState, name string, parent uuid.UUID) (*api.FlowFolder, error) {
	body := api.CreateFlowFolderRequest{Name: name}
	if parent != uuid.Nil {
		body.ParentId = &parent
	}

	resp, err := state.Client.API().CreateFlowFolderWithResponse(ctx, nil, body)
	if err != nil {
		return nil, err
	}
	if resp.JSON201 == nil {
		return nil, formatAPIError(resp.HTTPResponse, resp.Body)
	}
	return resp.JSON201, nil
}

// flowCountsByFolder counts the organization's flows per folder, returning the
// per-folder counts and the number of flows with no folder at all.
func flowCountsByFolder(ctx context.Context, state *AppState) (map[uuid.UUID]int, int, error) {
	counts := make(map[uuid.UUID]int)
	uncategorized := 0

	var (
		seen   int
		offset int32
	)
	for {
		limit := int32(folderSearchPageSize)
		pageOffset := offset
		resp, err := state.Client.API().SearchFlowsWithResponse(ctx, nil, api.FlowSearchRequest{
			Pagination: &api.PaginationRequest{Limit: &limit, Offset: &pageOffset},
		})
		if err != nil {
			return nil, 0, err
		}
		if resp.JSON200 == nil {
			return nil, 0, formatAPIError(resp.HTTPResponse, resp.Body)
		}

		for _, item := range resp.JSON200.Items {
			if item.FolderId == nil {
				uncategorized++
				continue
			}
			counts[*item.FolderId]++
		}

		seen += len(resp.JSON200.Items)
		if len(resp.JSON200.Items) < folderSearchPageSize || int64(seen) >= resp.JSON200.Total {
			break
		}
		offset += folderSearchPageSize
	}

	return counts, uncategorized, nil
}

// folderIndex is an in-memory view of the organization's flow folder tree.
type folderIndex struct {
	byID     map[uuid.UUID]api.FlowFolder
	children map[uuid.UUID][]api.FlowFolder // keyed by parent id; uuid.Nil holds the roots
}

func newFolderIndex(items []api.FlowFolder) *folderIndex {
	idx := &folderIndex{
		byID:     make(map[uuid.UUID]api.FlowFolder, len(items)),
		children: make(map[uuid.UUID][]api.FlowFolder),
	}
	for _, folder := range items {
		idx.add(folder)
	}
	return idx
}

// add inserts or replaces a folder, keeping each sibling list sorted by name.
func (idx *folderIndex) add(folder api.FlowFolder) {
	if previous, ok := idx.byID[folder.Id]; ok {
		idx.detach(previous)
	}
	idx.byID[folder.Id] = folder

	parent := folderParent(folder)
	siblings := append(idx.children[parent], folder)
	sort.Slice(siblings, func(i, j int) bool { return siblings[i].Name < siblings[j].Name })
	idx.children[parent] = siblings
}

func (idx *folderIndex) detach(folder api.FlowFolder) {
	parent := folderParent(folder)
	siblings := idx.children[parent]
	for i := range siblings {
		if siblings[i].Id == folder.Id {
			idx.children[parent] = append(siblings[:i], siblings[i+1:]...)
			return
		}
	}
}

// resolve turns a folder reference — a folder id, or a "/"-separated path of
// folder names from the tree root — into a folder id.
func (idx *folderIndex) resolve(ref string) (uuid.UUID, error) {
	trimmed := strings.Trim(strings.TrimSpace(ref), folderPathSeparator)
	if trimmed == "" {
		return uuid.Nil, fmt.Errorf("provide a folder id or path")
	}

	if id, err := uuid.Parse(trimmed); err == nil {
		if _, ok := idx.byID[id]; !ok {
			return uuid.Nil, fmt.Errorf("no folder with id %s in this organization", id)
		}
		return id, nil
	}

	parent := uuid.Nil
	for _, segment := range splitFolderPath(trimmed) {
		id, matches := idx.lookupChild(parent, segment)
		switch {
		case matches == 0:
			return uuid.Nil, fmt.Errorf("no folder named %q under %s", segment, idx.describe(parent))
		case matches > 1:
			return uuid.Nil, fmt.Errorf(
				"%d folders named %q under %s; use the folder id",
				matches, segment, idx.describe(parent),
			)
		}
		parent = id
	}
	return parent, nil
}

// lookupChild returns the id of parent's child named name and how many children
// carry that name; the id is only meaningful when exactly one matched.
func (idx *folderIndex) lookupChild(parent uuid.UUID, name string) (uuid.UUID, int) {
	wanted := strings.TrimSpace(name)
	var (
		found   uuid.UUID
		matches int
	)
	for _, folder := range idx.children[parent] {
		if strings.EqualFold(folder.Name, wanted) {
			found = folder.Id
			matches++
		}
	}
	return found, matches
}

// path renders a folder's full path from the tree root.
func (idx *folderIndex) path(id uuid.UUID) string {
	var segments []string
	seen := make(map[uuid.UUID]struct{})
	for current := id; current != uuid.Nil; {
		if _, loop := seen[current]; loop {
			break
		}
		seen[current] = struct{}{}

		folder, ok := idx.byID[current]
		if !ok {
			break
		}
		segments = append([]string{folder.Name}, segments...)
		current = folderParent(folder)
	}
	return strings.Join(segments, folderPathSeparator)
}

func (idx *folderIndex) describe(id uuid.UUID) string {
	if id == uuid.Nil {
		return "the root"
	}
	return fmt.Sprintf("%q", idx.path(id))
}

// walk visits the subtree under parent depth-first, in sibling name order.
func (idx *folderIndex) walk(parent uuid.UUID, depth int, visit func(api.FlowFolder, int)) {
	if depth > maxFolderDepth {
		return
	}
	for _, folder := range idx.children[parent] {
		visit(folder, depth)
		idx.walk(folder.Id, depth+1, visit)
	}
}

func folderParent(folder api.FlowFolder) uuid.UUID {
	if folder.ParentId == nil {
		return uuid.Nil
	}
	return *folder.ParentId
}
