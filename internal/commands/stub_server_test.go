package commands

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/output"
)

type stubRequest struct {
	method string
	path   string
	query  string
	header http.Header
	body   []byte
}

type stubRoute struct {
	status int
	body   any
}

// stubHandler answers a request itself, given the body it carried, for a route whose
// answer depends on the request, such as a page of a cursor search.
type stubHandler func(w http.ResponseWriter, body []byte)

// apiStub answers the API with the responses registered per "METHOD path" and
// records every request, so a test can tell what a command sent and what it did not.
type apiStub struct {
	mu       sync.Mutex
	routes   map[string]stubRoute
	handlers map[string]stubHandler
	requests []stubRequest
	server   *httptest.Server
}

func newAPIStub(t *testing.T) *apiStub {
	t.Helper()
	stub := &apiStub{routes: map[string]stubRoute{}, handlers: map[string]stubHandler{}}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		stub.mu.Lock()
		stub.requests = append(stub.requests, stubRequest{
			method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, header: r.Header.Clone(), body: body,
		})
		route, ok := stub.routes[r.Method+" "+r.URL.Path]
		handler := stub.handlers[r.Method+" "+r.URL.Path]
		stub.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if handler != nil {
			handler(w, body)
			return
		}
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(
				w,
				`{"errors":[{"code":"NOT_FOUND","message":"no stub for `+r.Method+" "+r.URL.Path+`"}]}`,
			)
			return
		}
		w.WriteHeader(route.status)
		if route.body != nil {
			_ = json.NewEncoder(w).Encode(route.body)
		}
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func (stub *apiStub) on(method, path string, status int, body any) *apiStub {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.routes[method+" "+path] = stubRoute{status: status, body: body}
	return stub
}

// onHandler answers "METHOD path" with the handler.
func (stub *apiStub) onHandler(method, path string, handler stubHandler) *apiStub {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.handlers[method+" "+path] = handler
	return stub
}

// onSearch answers POST path with one page holding every item and no next_cursor.
func (stub *apiStub) onSearch(path string, items ...any) *apiStub {
	if items == nil {
		items = []any{}
	}
	return stub.on(http.MethodPost, path, http.StatusOK,
		map[string]any{"count": len(items), "total": len(items), "items": items})
}

// flowSearchPath is where the flow search is served.
const flowSearchPath = "/flows/search"

// onFlows answers the flow search, which every CLI listing reads, and the deprecated
// GET /flows, which the MCP list_flows tool still calls, with the flows.
func (stub *apiStub) onFlows(flows ...api.Flow) *apiStub {
	items := make([]any, 0, len(flows))
	for _, flow := range flows {
		items = append(items, searchItemOf(flow))
	}
	stub.onSearch(flowSearchPath, items...)
	return stub.on(http.MethodGet, "/flows", http.StatusOK,
		api.FlowListResponse{Count: len(flows), Total: int64(len(flows)), Items: flows})
}

// onFolders answers the flow folder search with the folders.
func (stub *apiStub) onFolders(folders ...api.FlowFolder) *apiStub {
	items := make([]any, 0, len(folders))
	for _, folder := range folders {
		items = append(items, folder)
	}
	return stub.onSearch("/flows/folders/search", items...)
}

// searchItemOf is the slim row the flow search returns for a flow.
func searchItemOf(flow api.Flow) api.FlowSearchItem {
	return api.FlowSearchItem{
		Id: flow.Id, Name: flow.Name, FolderId: flow.FolderId, Role: flow.Role, Tags: flow.Tags,
		Version: flow.Version, CreatedAt: flow.CreatedAt, UpdatedAt: flow.UpdatedAt,
	}
}

func (stub *apiStub) requestsTo(method, path string) []stubRequest {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	var matching []stubRequest
	for _, request := range stub.requests {
		if request.method == method && request.path == path {
			matching = append(matching, request)
		}
	}
	return matching
}

func (stub *apiStub) requestCount() int {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	return len(stub.requests)
}

func (stub *apiStub) state(t *testing.T) *AppState {
	t.Helper()
	return makeState(t, "test-key", "", stub.server.URL)
}

func (stub *apiStub) tableState(t *testing.T) *AppState {
	t.Helper()
	state := stub.state(t)
	state.OutputFormat = output.FormatTable
	return state
}

func sampleFlow(id uuid.UUID, name string, folder *uuid.UUID) api.Flow {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	return api.Flow{
		Id: id, Name: name, FolderId: folder,
		Version: "1.0", Tags: []string{}, Role: api.FlowRoleStandalone,
		CreatedAt: now, UpdatedAt: now,
		FlowDefinition: api.FlowDefinition{Nodes: []api.FlowNode{}, Edges: []api.FlowEdge{}},
	}
}

func sampleFolder(id uuid.UUID, name string, parent *uuid.UUID) api.FlowFolder {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	return api.FlowFolder{Id: id, Name: name, ParentId: parent, CreatedAt: now, UpdatedAt: now}
}

func idFor(last string) uuid.UUID {
	return uuid.MustParse("550e8400-e29b-41d4-a716-" + strings.Repeat("0", 12-len(last)) + last)
}

// execute runs a command with the given arguments and stdin and returns what it
// wrote to its own stdout and stderr.
func execute(t *testing.T, cmd *cobra.Command, stdin string, args ...string) (string, string, error) {
	t.Helper()
	var out, errOut strings.Builder
	cmd.SetArgs(args)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}
