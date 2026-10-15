package commands

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/output"
)

// Every collection Echopoint owns is a POST .../search that pages with an opaque
// keyset cursor: a response carries items, total, count and, unless it is the last
// page, next_cursor. This file is the one place the CLI speaks that protocol.
// Commands that list send a page request through fetchSearch; code that needs every
// row walks next_cursor through walkSearch. Neither pages by offset.

const (
	// maxSearchLimit is the largest page the API serves.
	maxSearchLimit = 100
	// defaultListLimit is the page size of a listing command that does not say otherwise.
	defaultListLimit = 20

	flagLimit  = "limit"
	flagCursor = "cursor"
)

// searchPage is one page of a search, or every page a walk collected, whatever
// the item type. Its JSON form is the API's list envelope, so structured output
// carries next_cursor exactly as the API sends it: absent on the last page.
type searchPage[T any] struct {
	Count      int     `json:"count"`
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor,omitempty"`
	Total      int64   `json:"total"`
}

// newSearchPage builds a page from the parts of an API response. Items is never
// null in the output, and Count follows the items the page really holds.
func newSearchPage[T any](items []T, total int64, next *string) searchPage[T] {
	if items == nil {
		items = []T{}
	}
	if next != nil && *next == "" {
		next = nil
	}
	return searchPage[T]{Count: len(items), Items: items, NextCursor: next, Total: total}
}

// pageFetcher reads one page of a search with the given pagination.
type pageFetcher[T any] func(ctx context.Context, pagination *api.PaginationRequest) (searchPage[T], error)

// pageFlags are the flags every listing command shares.
type pageFlags struct {
	limit  int32
	offset int32
	cursor string
	all    bool
}

// bind registers --limit, --offset, --cursor and --all on a listing command.
func (p *pageFlags) bind(cmd *cobra.Command, defaultLimit int32) {
	cmd.Flags().Int32Var(&p.limit, flagLimit, defaultLimit,
		fmt.Sprintf("Number of results per page (1-%d)", maxSearchLimit))
	cmd.Flags().Int32Var(&p.offset, "offset", 0,
		"Skip this many results (page-number style); to continue a listing use --cursor")
	cmd.Flags().StringVar(&p.cursor, flagCursor, "",
		"Continue after the last result of a previous page: the next_cursor it returned")
	cmd.Flags().BoolVar(&p.all, "all", false, "Follow next_cursor and list every page")
}

// validate refuses a request the API would, before any request is sent.
func (p *pageFlags) validate() error {
	if p.limit < 1 || p.limit > maxSearchLimit {
		return fmt.Errorf("--limit must be between 1 and %d", maxSearchLimit)
	}
	if p.offset < 0 {
		return errors.New("--offset must be nonnegative")
	}
	if p.cursor != "" && p.offset != 0 {
		return errors.New(
			"--cursor cannot be combined with --offset: a cursor continues after the last result of a page",
		)
	}
	return nil
}

// first is the pagination of the first request. With --all and no --limit the
// walk takes the largest pages the API serves, to need the fewest requests.
func (p *pageFlags) first(cmd *cobra.Command) *api.PaginationRequest {
	limit := p.limit
	if p.all && !cmd.Flags().Changed(flagLimit) {
		limit = maxSearchLimit
	}
	pagination := &api.PaginationRequest{Limit: &limit}
	if p.offset != 0 {
		offset := p.offset
		pagination.Offset = &offset
	}
	if p.cursor != "" {
		cursor := p.cursor
		pagination.Cursor = &cursor
	}
	return pagination
}

// fetchSearch reads the page the flags select, or with --all every page from there.
func fetchSearch[T any](
	ctx context.Context, cmd *cobra.Command, p *pageFlags, fetch pageFetcher[T],
) (searchPage[T], error) {
	if err := p.validate(); err != nil {
		return searchPage[T]{}, err
	}
	first := p.first(cmd)
	if !p.all {
		return fetch(ctx, first)
	}
	page, _, err := walkSearch(ctx, first, 0, fetch)
	return page, err
}

// walkSearch follows next_cursor from the first request until the search ends, or
// until maxPages pages were read when maxPages is positive. It returns the rows
// of every page it read, with the total of the first page, and whether it stopped
// early with more left, in which case the page's NextCursor continues the search.
// On an error it returns what it read before it.
func walkSearch[T any](
	ctx context.Context, first *api.PaginationRequest, maxPages int, fetch pageFetcher[T],
) (searchPage[T], bool, error) {
	collected := newSearchPage[T](nil, 0, nil)
	request := *first
	seen := map[string]struct{}{}
	if first.Cursor != nil {
		seen[*first.Cursor] = struct{}{}
	}
	for pages := 1; ; pages++ {
		if err := ctx.Err(); err != nil {
			return collected, false, err
		}
		page, err := fetch(ctx, &request)
		if err != nil {
			return collected, false, err
		}
		if pages == 1 {
			collected.Total = page.Total
		}
		collected.Items = append(collected.Items, page.Items...)
		collected.Count = len(collected.Items)
		collected.NextCursor = page.NextCursor
		if page.NextCursor == nil {
			return collected, false, nil
		}
		if _, repeated := seen[*page.NextCursor]; repeated {
			return collected, false, fmt.Errorf(
				"the API returned the same next_cursor twice; stopping after %d pages",
				pages,
			)
		}
		seen[*page.NextCursor] = struct{}{}
		if maxPages > 0 && pages >= maxPages {
			return collected, true, nil
		}
		next := *page.NextCursor
		request = api.PaginationRequest{Limit: first.Limit, Cursor: &next}
	}
}

// walkAll reads every row of a search in the largest pages the API serves.
func walkAll[T any](ctx context.Context, fetch pageFetcher[T]) ([]T, error) {
	limit := int32(maxSearchLimit)
	page, _, err := walkSearch(ctx, &api.PaginationRequest{Limit: &limit}, 0, fetch)
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}

// walkSome reads at most maxPages pages and returns the rows it got, even when a
// later page failed or ran out of time. Completions use it: a partial list beats
// none, and a completion never prints an error.
func walkSome[T any](ctx context.Context, maxPages int, fetch pageFetcher[T]) []T {
	limit := int32(maxSearchLimit)
	page, _, _ := walkSearch(ctx, &api.PaginationRequest{Limit: &limit}, maxPages, fetch)
	return page.Items
}

// printSearchPage prints a page as JSON or YAML, which carry next_cursor, or
// calls table for the default format and then hints how to continue.
func printSearchPage[T any](
	cmd *cobra.Command, state *AppState, page searchPage[T], table func(w io.Writer) error,
) error {
	w := cmd.OutOrStdout()
	switch state.OutputFormat {
	case output.FormatJSON:
		return output.PrintJSON(w, page)
	case output.FormatYAML:
		return output.PrintYAML(w, page)
	case output.FormatTable:
	}
	if err := table(w); err != nil {
		return err
	}
	printContinueHint(cmd.ErrOrStderr(), page.NextCursor)
	return nil
}

// printContinueHint tells a reader of the table how to get the next page. It goes
// to stderr, which keeps the table on stdout parseable.
func printContinueHint(w io.Writer, next *string) {
	if next == nil {
		return
	}
	fmt.Fprintf(w, "More results: add --cursor %s to continue, or --all to list every page.\n", *next)
}
