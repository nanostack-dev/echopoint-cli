package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/client"
)

// cursorSearchTools are the searches that page by keyset cursor, by tool name, with
// the route each one posts to.
var cursorSearchTools = map[string]string{
	"search_flow_folders":                             "/flows/folders/search",
	"search_collections":                              "/collections/search",
	"search_collection_folders":                       "/collections/{id}/folders/search",
	"search_collection_requests":                      "/collections/{id}/requests/search",
	"search_open_api_sync_history":                    "/collections/{id}/openapi-sync/history/search",
	"search_collection_request_open_api_sync_history": "/collections/{collectionId}/requests/{requestId}/openapi-sync-history/search",
	"search_flow_versions":                            "/flows/{id}/versions/search",
	"search_executions":                               "/executions/search",
	"search_node_execution_history":                   "/flows/{flowId}/nodes/{nodeId}/executions/search",
	"search_flow_schedules":                           "/monitors/search",
	"search_flow_schedule_runs":                       "/monitors/{monitorId}/runs/search",
	"search_specs":                                    "/specs/search",
	"search_spec_versions":                            "/specs/{slug}/versions/search",
	"search_environments":                             "/organization/environments/search",
	"search_organization_members":                     "/members/search",
}

// deprecatedListTools are the lists the API keeps serving; their tools remain.
var deprecatedListTools = []string{
	"list_webhooks", "list_flows", "list_flow_folders", "list_environments", "list_collections",
	"list_collection_folders", "list_collection_requests", "list_open_api_sync_history",
	"list_collection_request_open_api_sync_history", "list_flow_versions", "list_executions",
	"list_flow_executions", "get_node_execution_history", "list_flow_schedules",
	"list_flow_schedule_runs", "list_specs", "list_spec_versions", "list_organization_members",
}

// dangerListsAndSearches are the Anchor-backed operations the contract marks x-ai-danger
// (API keys and invitations), so neither their lists nor their searches are tools.
var dangerListsAndSearches = []string{
	"list_api_keys", "search_api_keys",
	"list_organization_invitations", "search_organization_invitations",
	"list_my_invitations", "search_my_invitations",
}

func catalogByName(t *testing.T) map[string]toolDef {
	t.Helper()
	tools, err := buildCatalog(api.OpenAPISpec)
	if err != nil {
		t.Fatalf("buildCatalog (a body field that shares a parameter's name is an error): %v", err)
	}
	byName := make(map[string]toolDef, len(tools))
	for _, td := range tools {
		byName[td.Name] = td
	}
	return byName
}

func TestTheCatalogExposesEveryCursorSearchAndKeepsTheDeprecatedLists(t *testing.T) {
	byName := catalogByName(t)

	for name, path := range cursorSearchTools {
		td, ok := byName[name]
		if !ok {
			t.Errorf("missing search tool %s", name)
			continue
		}
		if td.Method != http.MethodPost || td.PathTemplate != path {
			t.Errorf("%s = %s %s, want POST %s", name, td.Method, td.PathTemplate, path)
		}
	}
	for _, name := range deprecatedListTools {
		if _, ok := byName[name]; !ok {
			t.Errorf("the deprecated list tool %s is gone", name)
		}
	}
}

func TestTheDeprecatedListToolsPointAgentsAtTheirSearch(t *testing.T) {
	byName := catalogByName(t)

	for _, name := range deprecatedListTools {
		td, ok := byName[name]
		if !ok {
			t.Errorf("the deprecated list tool %s is gone", name)
			continue
		}
		if !strings.HasPrefix(td.Description, "Deprecated: use search_") ||
			!strings.Contains(td.Description, " instead, which pages with a cursor.") {
			t.Errorf(
				"%s description = %q, want it to start with %q",
				name, td.Description, "Deprecated: use <search_tool> instead, which pages with a cursor.",
			)
		}
	}
}

func TestTheDangerousAnchorListsAndSearchesAreNotTools(t *testing.T) {
	byName := catalogByName(t)

	for _, name := range dangerListsAndSearches {
		if _, ok := byName[name]; ok {
			t.Errorf("%s is an x-ai-danger operation and must not be an MCP tool", name)
		}
	}
}

type searchToolSchema struct {
	Type       string                     `json:"type"`
	Required   []string                   `json:"required"`
	Properties map[string]json.RawMessage `json:"properties"`
}

func TestEverySearchToolTakesItsPaginationIncludingTheCursor(t *testing.T) {
	byName := catalogByName(t)

	for name := range cursorSearchTools {
		t.Run(name, func(t *testing.T) {
			td := byName[name]
			var schema searchToolSchema
			if err := json.Unmarshal(td.InputSchema, &schema); err != nil || schema.Type != "object" {
				t.Fatalf("schema %s: %v", td.InputSchema, err)
			}

			var pagination struct {
				Type       string `json:"type"`
				Properties map[string]struct {
					Type string `json:"type"`
				} `json:"properties"`
			}
			if err := json.Unmarshal(schema.Properties["pagination"], &pagination); err != nil {
				t.Fatalf("pagination: %v", err)
			}
			if pagination.Type != "object" {
				t.Errorf("pagination is %q, want an object", pagination.Type)
			}
			for field, kind := range map[string]string{"limit": "integer", "offset": "integer", "cursor": "string"} {
				if pagination.Properties[field].Type != kind {
					t.Errorf("pagination.%s = %q, want %s", field, pagination.Properties[field].Type, kind)
				}
			}

			// pagination travels in the body, never as a parameter, and the path
			// parameters are the only required arguments
			if td.Locations["pagination"] != locBody {
				t.Errorf("pagination location = %q, want body", td.Locations["pagination"])
			}
			for argument, location := range td.Locations {
				if slices.Contains(schema.Required, argument) != (location == locPath) {
					t.Errorf("argument %s (%s) required = %v", argument, location, schema.Required)
				}
				if location == locQuery {
					t.Errorf("search tool %s has the query argument %s", name, argument)
				}
			}
		})
	}
}

// pathArgument is a value that fits the path parameter called name.
func pathArgument(name string) string {
	switch name {
	case "nodeId", "slug":
		return "n-1"
	}
	return "8fc078c1-d8e6-4059-9e6f-b69fb52b7b17"
}

func TestEverySearchToolSendsTheCursorAndReturnsTheNextOne(t *testing.T) {
	byName := catalogByName(t)

	for name, template := range cursorSearchTools {
		t.Run(name, func(t *testing.T) {
			td := byName[name]
			arguments := map[string]any{
				"pagination": map[string]any{"limit": 2, "cursor": "cursor-1"},
			}
			wantPath := template
			for argument, location := range td.Locations {
				if location == locPath {
					arguments[argument] = pathArgument(argument)
					wantPath = strings.ReplaceAll(wantPath, "{"+argument+"}", pathArgument(argument))
				}
			}

			var gotPath, gotMethod string
			var gotBody map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath, gotMethod = r.URL.Path, r.Method
				_ = json.NewDecoder(r.Body).Decode(&gotBody)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"items":[],"count":0,"total":5,"next_cursor":"cursor-2"}`)
			}))
			defer server.Close()
			cli, err := client.NewWithAPIKey(server.URL, "test-key", "org_test", time.Second)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(arguments)
			if err != nil {
				t.Fatal(err)
			}

			result, err := makeHandler(cli, td)(t.Context(), &mcpsdk.CallToolRequest{
				Params: &mcpsdk.CallToolParamsRaw{Name: name, Arguments: raw},
			})

			if err != nil || result.IsError {
				t.Fatalf("result %+v, err %v", result, err)
			}
			if gotMethod != http.MethodPost || gotPath != wantPath {
				t.Errorf("request = %s %s, want POST %s", gotMethod, gotPath, wantPath)
			}
			pagination, _ := gotBody["pagination"].(map[string]any)
			if pagination["cursor"] != "cursor-1" || pagination["limit"] != float64(2) || len(gotBody) != 1 {
				t.Errorf("body = %v, want only the pagination with its cursor", gotBody)
			}
			text, _ := result.Content[0].(*mcpsdk.TextContent)
			if text == nil || !strings.Contains(text.Text, `"next_cursor": "cursor-2"`) {
				t.Errorf("the agent is not given next_cursor: %+v", result.Content)
			}
		})
	}
}

func TestAnAgentWalksASearchByCursorToItsEnd(t *testing.T) {
	td := catalogByName(t)["search_collections"]
	pages := map[string]string{
		"":        `{"items":[{"id":"a"},{"id":"b"}],"count":2,"total":3,"next_cursor":"after-b"}`,
		"after-b": `{"items":[{"id":"c"}],"count":1,"total":3}`,
	}
	var cursors []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Pagination struct {
				Cursor string `json:"cursor"`
			} `json:"pagination"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		cursors = append(cursors, body.Pagination.Cursor)
		_, _ = io.WriteString(w, pages[body.Pagination.Cursor])
	}))
	defer server.Close()
	cli, err := client.NewWithAPIKey(server.URL, "test-key", "org_test", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	handler := makeHandler(cli, td)

	var seen []string
	arguments := `{"pagination":{"limit":2}}`
	for range 5 {
		result, err := handler(t.Context(), &mcpsdk.CallToolRequest{
			Params: &mcpsdk.CallToolParamsRaw{Name: td.Name, Arguments: json.RawMessage(arguments)},
		})
		if err != nil || result.IsError {
			t.Fatalf("result %+v, err %v", result, err)
		}
		var page struct {
			Items      []struct{ ID string } `json:"items"`
			NextCursor string                `json:"next_cursor"`
		}
		if err := json.Unmarshal([]byte(result.Content[0].(*mcpsdk.TextContent).Text), &page); err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			seen = append(seen, item.ID)
		}
		if page.NextCursor == "" {
			break
		}
		arguments = `{"pagination":{"limit":2,"cursor":"` + page.NextCursor + `"}}`
	}

	if !slices.Equal(seen, []string{"a", "b", "c"}) || !slices.Equal(cursors, []string{"", "after-b"}) {
		t.Errorf("seen %v with cursors %q", seen, cursors)
	}
}
