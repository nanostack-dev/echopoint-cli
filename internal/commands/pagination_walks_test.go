package commands

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"echopoint-cli/internal/api"
)

// The code that needs every row of a search, not a page, walks next_cursor.

func TestOrgEnvironmentsListWalksEveryPage(t *testing.T) {
	table := newKeysetTable(tiedRows(230, 9, func(id uuid.UUID, i int) any {
		return api.Environment{Id: id, Name: "env-" + strconv.Itoa(1000+i)}
	})...)
	stub := newAPIStub(t).onHandler(http.MethodPost, "/organization/environments/search", table.handle)
	state := stub.state(t)
	state.OutputFormat = "json"

	out, _, err := execute(t, newOrgCmd(state), "", "env", "environments", "list")

	if err != nil {
		t.Fatal(err)
	}
	var names []string
	if err := json.Unmarshal([]byte(out), &names); err != nil || len(names) != 230 {
		t.Fatalf("%d names, err %v", len(names), err)
	}
	if got := len(table.requests()); got != 3 {
		t.Errorf("%d requests, want 3 pages of up to 100", got)
	}
	requireNoOffset(t, table)
}

func TestFlowFoldersAreReadWhole(t *testing.T) {
	table := newKeysetTable(tiedRows(120, 5, func(id uuid.UUID, i int) any {
		return sampleFolder(id, "Folder "+strconv.Itoa(i), nil)
	})...)
	stub := newAPIStub(t).onHandler(http.MethodPost, "/flows/folders/search", table.handle)

	folders, err := fetchFlowFolders(context.Background(), stub.state(t))

	if err != nil || len(folders) != 120 || len(table.requests()) != 2 {
		t.Fatalf("%d folders in %d requests, err %v", len(folders), len(table.requests()), err)
	}
	requireNoOffset(t, table)
}

func TestFolderFlowCountsWalkEveryFlow(t *testing.T) {
	folder := idFor("10")
	table := newKeysetTable(tiedRows(205, 10, func(id uuid.UUID, i int) any {
		var parent *uuid.UUID
		if i%2 == 0 {
			parent = &folder
		}
		return searchItemOf(sampleFlow(id, "Flow", parent))
	})...)
	stub := newAPIStub(t).onHandler(http.MethodPost, flowSearchPath, table.handle)

	counts, uncategorized, err := flowCountsByFolder(context.Background(), stub.state(t))

	if err != nil || counts[folder] != 103 || uncategorized != 102 {
		t.Fatalf("in folder %d, uncategorized %d, err %v", counts[folder], uncategorized, err)
	}
	requireNoOffset(t, table)
}

func TestFlowTagSelectionWalksEveryMatch(t *testing.T) {
	table := newKeysetTable(flowRows(230, 12)...)
	stub := newAPIStub(t).onHandler(http.MethodPost, flowSearchPath, table.handle)

	ids, err := searchFlowIDs(context.Background(), stub.state(t), "", []string{"prod"}, "all")

	if err != nil || len(ids) != 230 || len(slices.Compact(slices.Sorted(slices.Values(ids)))) != 230 {
		t.Fatalf("%d ids, err %v", len(ids), err)
	}
	sent := bodyOf(t, stub.requestsTo(http.MethodPost, flowSearchPath)[0])
	if tags, _ := sent["tags"].([]any); len(tags) != 1 || sent["tag_match_mode"] != "all" {
		t.Errorf("first request = %v", sent)
	}
	requireNoOffset(t, table)
}

func TestFlowCompletionReadsAtMostCompletionMaxPagesByCursor(t *testing.T) {
	table := newKeysetTable(flowRows(completionMaxPages*100+60, 7)...)
	stub := newAPIStub(t).onHandler(http.MethodPost, flowSearchPath, table.handle)
	stub.onFolders()
	state := stub.state(t)

	got, _ := completeFromAPI(state, &cobra.Command{}, "", fetchFlowCandidates(state))

	if len(got) != completionMaxPages*100 {
		t.Errorf("%d candidates, want the first %d pages", len(got), completionMaxPages)
	}
	if len(table.requests()) != completionMaxPages {
		t.Errorf("%d searches, want %d", len(table.requests()), completionMaxPages)
	}
	requireNoOffset(t, table)
}

func TestACompletionKeepsWhatItReadWhenALaterPageFails(t *testing.T) {
	table := newKeysetTable(flowRows(150, 4)...)
	calls := 0
	stub := newAPIStub(t).onHandler(http.MethodPost, flowSearchPath, func(w http.ResponseWriter, body []byte) {
		calls++
		if calls == 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		table.handle(w, body)
	})
	stub.onFolders()
	state := stub.state(t)

	got, _ := completeFromAPI(state, &cobra.Command{}, "", fetchFlowCandidates(state))

	if len(got) != 100 {
		t.Errorf("%d candidates, want the 100 of the first page", len(got))
	}
}

func TestAWalkGivesUpWhenItsContextEnds(t *testing.T) {
	table := newKeysetTable(flowRows(300, 5)...)
	stub := newAPIStub(t).onHandler(http.MethodPost, flowSearchPath, table.handle)
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()

	_, err := walkAll(ctx, searchFlows(stub.state(t), api.FlowSearchRequest{}))

	if err == nil {
		t.Error("a walk with an ended context did not fail")
	}
}
