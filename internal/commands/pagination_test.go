package commands

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/output"
)

// flowRows makes n flows named "Flow 0".. whose search keys tie in groups of tie.
func flowRows(n, tie int) []keysetRow {
	return tiedRows(n, tie, func(id uuid.UUID, position int) any {
		return searchItemOf(sampleFlow(id, "Flow "+strconv.Itoa(position), nil))
	})
}

type listedPage struct {
	Count      int              `json:"count"`
	Total      int64            `json:"total"`
	NextCursor *string          `json:"next_cursor"`
	Items      []map[string]any `json:"items"`
}

func (page listedPage) names() []string {
	names := make([]string, 0, len(page.Items))
	for _, item := range page.Items {
		names = append(names, item["name"].(string))
	}
	return names
}

func runFlowList(
	t *testing.T,
	stub *apiStub,
	format output.Format,
	args ...string,
) (listedPage, string, string, error) {
	t.Helper()
	state := stub.state(t)
	state.OutputFormat = format
	stdout, stderr, err := execute(t, newFlowCmd(state), "", append([]string{"list"}, args...)...)
	var page listedPage
	if format == output.FormatJSON && err == nil {
		if decodeErr := json.Unmarshal([]byte(stdout), &page); decodeErr != nil {
			t.Fatalf("stdout is not a page: %v\n%s", decodeErr, stdout)
		}
	}
	return page, stdout, stderr, err
}

func newPaginationStub(t *testing.T, table *keysetTable) *apiStub {
	t.Helper()
	stub := newAPIStub(t)
	stub.onHandler(http.MethodPost, flowSearchPath, table.handle)
	return stub
}

func TestAListedPageCarriesNextCursorAndContinuesFromItAcrossTiedValues(t *testing.T) {
	// 4 flows share each sort value, so the boundary of a page of 3 falls inside a group
	table := newKeysetTable(flowRows(9, 4)...)
	stub := newPaginationStub(t, table)

	first, _, _, err := runFlowList(t, stub, output.FormatJSON, "--limit", "3")
	if err != nil {
		t.Fatal(err)
	}
	if first.NextCursor == nil || first.Total != 9 || first.Count != 3 ||
		!slices.Equal(first.names(), []string{"Flow 0", "Flow 1", "Flow 2"}) {
		t.Fatalf("first page = %+v", first)
	}

	second, _, _, err := runFlowList(t, stub, output.FormatJSON, "--limit", "3", "--cursor", *first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	// Flow 3 ties with Flow 2 on the sort value and begins the second page
	if !slices.Equal(second.names(), []string{"Flow 3", "Flow 4", "Flow 5"}) || second.NextCursor == nil {
		t.Fatalf("second page = %+v", second)
	}

	last, _, _, err := runFlowList(t, stub, output.FormatJSON, "--limit", "3", "--cursor", *second.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(last.names(), []string{"Flow 6", "Flow 7", "Flow 8"}) || last.NextCursor != nil {
		t.Fatalf("last page = %+v", last)
	}
	requireNoOffset(t, table)
}

func TestTheNextCursorIsAbsentFromTheLastPageOfStructuredOutput(t *testing.T) {
	stub := newPaginationStub(t, newKeysetTable(flowRows(2, 1)...))

	for _, format := range []output.Format{output.FormatJSON, output.FormatYAML} {
		_, stdout, _, err := runFlowList(t, stub, format)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(stdout, "next_cursor") {
			t.Errorf("%s: a last page names a next cursor:\n%s", format, stdout)
		}
	}
	more := newPaginationStub(t, newKeysetTable(flowRows(5, 1)...))
	_, stdout, _, err := runFlowList(t, more, output.FormatYAML, "--limit", "2")
	if err != nil || !strings.Contains(stdout, "next_cursor: ") {
		t.Errorf("yaml with more pages lacks next_cursor (err %v):\n%s", err, stdout)
	}
}

func TestTheTableHintsHowToContinueOnStderrOnlyWhileThereIsMore(t *testing.T) {
	table := newKeysetTable(flowRows(5, 1)...)
	stub := newPaginationStub(t, table)

	_, stdout, stderr, err := runFlowList(t, stub, output.FormatTable, "--limit", "3")
	if err != nil {
		t.Fatal(err)
	}
	cursor := strings.TrimSuffix(
		strings.SplitN(strings.TrimPrefix(stderr, "More results: add --cursor "), " ", 2)[0],
		"\n",
	)
	if !strings.HasPrefix(stderr, "More results: add --cursor ") || !strings.Contains(stderr, "--all") || cursor == "" {
		t.Fatalf("stderr = %q", stderr)
	}
	if strings.Contains(stdout, "More results") || !strings.Contains(stdout, "Total: 5") {
		t.Errorf("stdout = %q", stdout)
	}

	// the cursor of the hint continues the listing, to a last page with no hint
	_, stdout, stderr, err = runFlowList(t, stub, output.FormatTable, "--limit", "3", "--cursor", cursor)
	if err != nil || stderr != "" || !strings.Contains(stdout, "Flow 3") || !strings.Contains(stdout, "Flow 4") {
		t.Errorf("last page: err %v, stdout %q, stderr %q", err, stdout, stderr)
	}
}

func TestAllWalksNextCursorToTheEndWithoutRepeatingOrSkippingTiedRows(t *testing.T) {
	table := newKeysetTable(flowRows(11, 4)...)
	// a flow created while the listing is read must not shift the pages still to come
	table.afterPage = func(n int, table *keysetTable) {
		if n == 1 {
			table.insert(
				keysetRow{key: 5000, id: idFor("9999"), item: searchItemOf(sampleFlow(idFor("9999"), "Newcomer", nil))},
			)
		}
	}
	stub := newPaginationStub(t, table)

	page, _, _, err := runFlowList(t, stub, output.FormatJSON, "--all", "--limit", "3")

	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for i := range 11 {
		want = append(want, "Flow "+strconv.Itoa(i))
	}
	if !slices.Equal(page.names(), want) {
		t.Errorf("names = %v, want %v", page.names(), want)
	}
	if page.NextCursor != nil || page.Count != 11 || page.Total != 11 {
		t.Errorf("page = count %d, total %d, next %v", page.Count, page.Total, page.NextCursor)
	}
	if got := len(table.requests()); got != 4 {
		t.Errorf("%d requests, want 4 pages of 3", got)
	}
	requireNoOffset(t, table)
}

func TestAllTakesTheLargestPagesUnlessALimitIsGiven(t *testing.T) {
	table := newKeysetTable(flowRows(250, 7)...)
	stub := newPaginationStub(t, table)

	page, _, _, err := runFlowList(t, stub, output.FormatJSON, "--all")

	if err != nil || len(page.Items) != 250 || len(table.requests()) != 3 {
		t.Fatalf("err %v, %d items in %d requests", err, len(page.Items), len(table.requests()))
	}
	for _, pagination := range table.requests() {
		if pagination["limit"] != float64(100) {
			t.Errorf("pagination = %v, want limit 100", pagination)
		}
	}
}

func TestAllContinuesFromAGivenCursor(t *testing.T) {
	table := newKeysetTable(flowRows(8, 3)...)
	stub := newPaginationStub(t, table)
	first, _, _, err := runFlowList(t, stub, output.FormatJSON, "--limit", "3")
	if err != nil {
		t.Fatal(err)
	}

	rest, _, _, err := runFlowList(t, stub, output.FormatJSON, "--all", "--limit", "3", "--cursor", *first.NextCursor)

	if err != nil || len(rest.Items) != 5 || rest.names()[0] != "Flow 3" || rest.NextCursor != nil {
		t.Errorf("err %v, names %v, next %v", err, rest.names(), rest.NextCursor)
	}
}

func TestOffsetIsStillAcceptedAndSkipsByCount(t *testing.T) {
	table := newKeysetTable(flowRows(6, 2)...)
	stub := newPaginationStub(t, table)

	page, _, _, err := runFlowList(t, stub, output.FormatJSON, "--limit", "2", "--offset", "3")

	if err != nil || !slices.Equal(page.names(), []string{"Flow 3", "Flow 4"}) || page.NextCursor == nil {
		t.Errorf("err %v, page %+v", err, page)
	}
	if sent := table.requests()[0]; sent["offset"] != float64(3) || sent["cursor"] != nil {
		t.Errorf("pagination = %v", sent)
	}
}

func TestPaginationFlagsThatTheAPIWouldRefuseFailBeforeAnyRequest(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"cursor with offset": {[]string{"--cursor", "x", "--offset", "2"}, "--cursor cannot be combined with --offset"},
		"limit zero":         {[]string{"--limit", "0"}, "--limit must be between 1 and 100"},
		"limit too big":      {[]string{"--limit", "101"}, "--limit must be between 1 and 100"},
		"negative offset":    {[]string{"--offset", "-1"}, "--offset must be nonnegative"},
	} {
		t.Run(name, func(t *testing.T) {
			table := newKeysetTable(flowRows(3, 1)...)
			stub := newPaginationStub(t, table)

			_, _, _, err := runFlowList(t, stub, output.FormatJSON, tc.args...)

			if err == nil || !strings.Contains(err.Error(), tc.want) || stub.requestCount() != 0 {
				t.Errorf("err = %v, %d requests", err, stub.requestCount())
			}
		})
	}
}

func TestACursorTheAPIRefusesIsAnError(t *testing.T) {
	stub := newPaginationStub(t, newKeysetTable(flowRows(3, 1)...))

	_, _, _, err := runFlowList(t, stub, output.FormatJSON, "--cursor", "not-a-cursor")

	if err == nil || !strings.Contains(err.Error(), "(400)") ||
		!strings.Contains(err.Error(), "the cursor is not valid") {
		t.Errorf("err = %v", err)
	}
}

func TestAWalkStopsOnACursorThatDoesNotAdvance(t *testing.T) {
	stub := newAPIStub(t)
	stub.onHandler(http.MethodPost, flowSearchPath, func(w http.ResponseWriter, _ []byte) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"count": 1, "total": 9, "items": []any{searchItemOf(sampleFlow(idFor("1"), "Stuck", nil))},
			"next_cursor": "same",
		})
	})

	_, _, _, err := runFlowList(t, stub, output.FormatJSON, "--all")

	if err == nil || !strings.Contains(err.Error(), "same next_cursor twice") || stub.requestCount() > 3 {
		t.Errorf("err = %v after %d requests", err, stub.requestCount())
	}
}

func TestFlowListScopedToAFolderPagesTheSameWay(t *testing.T) {
	folder := idFor("10")
	table := newKeysetTable(flowRows(5, 2)...)
	stub := newPaginationStub(t, table)
	stub.onFolders(sampleFolder(folder, "Anchor", nil))

	page, _, _, err := runFlowList(t, stub, output.FormatJSON, "--folder", "Anchor", "--limit", "2")

	if err != nil || page.NextCursor == nil || len(page.Items) != 2 {
		t.Fatalf("err %v, page %+v", err, page)
	}
	sent := bodyOf(t, stub.requestsTo(http.MethodPost, flowSearchPath)[0])
	if sent["folder_id"] != folder.String() {
		t.Errorf("sent %v", sent)
	}
}

func TestExecutionListSearchesTheFlowsExecutionsByCursor(t *testing.T) {
	flow := idFor("1")
	table := newKeysetTable(tiedRows(5, 3, func(id uuid.UUID, _ int) any {
		return api.FlowExecutionActivity{Id: id, FlowId: flow, Status: "completed"}
	})...)
	stub := newAPIStub(t).onHandler(http.MethodPost, "/executions/search", table.handle)
	state := stub.state(t)
	state.OutputFormat = output.FormatJSON

	out, _, err := execute(t, newFlowCmd(state), "", "execution", "list", flow.String(), "--limit", "2")
	if err != nil {
		t.Fatal(err)
	}
	var page listedPage
	if err := json.Unmarshal([]byte(out), &page); err != nil || page.NextCursor == nil || len(page.Items) != 2 {
		t.Fatalf("page %+v, err %v", page, err)
	}
	out, _, err = execute(
		t,
		newFlowCmd(state),
		"",
		"execution",
		"list",
		flow.String(),
		"--limit",
		"2",
		"--cursor",
		*page.NextCursor,
	)
	if err != nil || !strings.Contains(out, `"count": 2`) {
		t.Fatalf("second page: err %v\n%s", err, out)
	}

	sent := bodyOf(t, stub.requestsTo(http.MethodPost, "/executions/search")[0])
	filter, _ := sent["filter"].(map[string]any)
	if ids, _ := filter["flow_ids"].([]any); len(ids) != 1 || ids[0] != flow.String() {
		t.Errorf("sent %v, want the flow as the filter", sent)
	}
}

func TestCollectionListPagesByCursor(t *testing.T) {
	collections := newKeysetTable(tiedRows(5, 3, func(id uuid.UUID, i int) any {
		return api.Collection{Id: id, Name: fmt.Sprintf("Collection %d", i)}
	})...)
	stub := newAPIStub(t).onHandler(http.MethodPost, "/collections/search", collections.handle)

	state := stub.state(t)
	state.OutputFormat = output.FormatJSON
	out, _, err := execute(t, newCollectionCmd(state), "", "list", "--limit", "2")
	var page listedPage
	if err != nil || json.Unmarshal([]byte(out), &page) != nil || page.NextCursor == nil {
		t.Fatalf("err %v, out %s", err, out)
	}
	out, _, err = execute(t, newCollectionCmd(state), "", "list", "--all", "--limit", "2")
	var all listedPage
	if err != nil || json.Unmarshal([]byte(out), &all) != nil || len(all.Items) != 5 || all.NextCursor != nil {
		t.Fatalf("--all: err %v, page %+v", err, all)
	}
	requireNoOffset(t, collections)
}

func TestSpecListAndVersionsPageByCursorAndLiveIsOnlyOnTheFirstPage(t *testing.T) {
	versions := newKeysetTable(tiedRows(5, 2, func(id uuid.UUID, i int) any {
		return versionOf(fmt.Sprintf("1.%d.0", 5-i), "minor", api.SpecChangeCounts{}, nil)
	})...)
	specs := newKeysetTable(tiedRows(3, 2, func(id uuid.UUID, i int) any {
		return specOf(fmt.Sprintf("spec-%d", i), "Spec", "1.0.0")
	})...)
	stub := newAPIStub(t)
	stub.onHandler(http.MethodPost, "/specs/pets-api/versions/search", versions.handle)
	stub.onHandler(http.MethodPost, "/specs/search", specs.handle)
	state := stub.tableState(t)

	first, stderr, err := runSpecWith(t, state, "versions", "pets-api", "--limit", "2")
	if err != nil || !strings.Contains(first, "1.5.0 (Live)") ||
		!strings.HasPrefix(stderr, "More results: add --cursor ") {
		t.Fatalf("first: err %v\n%s\nstderr %q", err, first, stderr)
	}
	cursor := strings.Fields(strings.TrimPrefix(stderr, "More results: add --cursor "))[0]
	second, _, err := runSpecWith(t, state, "versions", "pets-api", "--limit", "2", "--cursor", cursor)
	if err != nil || strings.Contains(second, "(Live)") || !strings.Contains(second, "1.3.0") {
		t.Fatalf("second: err %v\n%s", err, second)
	}

	state.OutputFormat = output.FormatJSON
	all, _, err := runSpecWith(t, state, "versions", "pets-api", "--all", "--limit", "2")
	var page struct {
		Items []struct{ Version string } `json:"items"`
	}
	if err != nil || json.Unmarshal([]byte(all), &page) != nil || len(page.Items) != 5 {
		t.Fatalf("--all: err %v\n%s", err, all)
	}

	listed, _, err := runSpecWith(t, state, "list", "--all", "--limit", "2")
	if err != nil || json.Unmarshal([]byte(listed), &page) != nil || len(page.Items) != 3 {
		t.Fatalf("spec list --all: err %v\n%s", err, listed)
	}
	requireNoOffset(t, versions)
	requireNoOffset(t, specs)
}

func runSpecWith(t *testing.T, state *AppState, args ...string) (string, string, error) {
	t.Helper()
	return execute(t, newSpecCmd(state), "", args...)
}

func TestFlowVersionListIsJSONWithNextCursor(t *testing.T) {
	flow := idFor("21")
	table := newKeysetTable(tiedRows(4, 2, func(id uuid.UUID, _ int) any {
		return api.FlowVersion{Id: id, FlowId: flow}
	})...)
	stub := newAPIStub(t).onHandler(http.MethodPost, "/flows/"+flow.String()+"/versions/search", table.handle)

	out, stderr, err := execute(t, newFlowCmd(stub.state(t)), "", "version", "list", flow.String(), "--limit", "3")

	var page listedPage
	if err != nil || json.Unmarshal([]byte(out), &page) != nil || page.NextCursor == nil || len(page.Items) != 3 {
		t.Fatalf("err %v, out %s", err, out)
	}
	if !strings.Contains(stderr, "More results") {
		t.Errorf("stderr = %q", stderr)
	}
}
