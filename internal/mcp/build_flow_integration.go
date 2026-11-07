package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"sort"

	"echopoint-cli/internal/client"
)

// Integration nodes (Echopoint ADR-0024). A build_flow node with an
// "integration" object is a request node that calls an integration operation,
// or a webhook_wait node that waits for an integration event. Launch fills the
// call from the integration, so the author gives only the item and its inputs.
const (
	webhookWaitKind = "webhook_wait"
	itemOperation   = "operation"
	itemEvent       = "event"
	choiceRefused   = "refused"
	itemsPageLimit  = 100
)

// flatIntegration names the integration item a node uses.
type flatIntegration struct {
	ID        string         `json:"id"`
	Operation string         `json:"operation,omitempty"`
	Event     string         `json:"event,omitempty"`
	Inputs    map[string]any `json:"inputs,omitempty"`
}

// catalogItem is the part of an integration item build_flow checks a node
// against.
type catalogItem struct {
	Kind      string            `json:"kind"`
	Key       string            `json:"key"`
	Choice    string            `json:"choice"`
	Operation *catalogOperation `json:"operation,omitempty"`
}

// catalogOperation is the call an operation item describes.
type catalogOperation struct {
	Method  string        `json:"method"`
	Path    string        `json:"path"`
	Params  []catalogName `json:"params"`
	Outputs []catalogName `json:"outputs"`
}

// catalogName is a named operation param or output. An output's path is the
// JSONPath launch extracts it with.
type catalogName struct {
	Name string `json:"name"`
	Path string `json:"path,omitempty"`
}

// integrationCatalog holds, per integration id, its items by kind and key. A
// nil entry means the integration does not exist.
type integrationCatalog map[string]map[string]catalogItem

func catalogKey(kind, key string) string { return kind + ":" + key }

func (c integrationCatalog) item(ref flatIntegration) (catalogItem, bool) {
	kind, key := itemOperation, ref.Operation
	if ref.Event != "" {
		kind, key = itemEvent, ref.Event
	}
	found, ok := c[ref.ID][catalogKey(kind, key)]
	return found, ok
}

// integrationIDs lists the distinct integrations the flow uses, sorted.
func integrationIDs(f flatFlow) []string {
	var ids []string
	for _, n := range f.Nodes {
		if n.Integration != nil && n.Integration.ID != "" && !slices.Contains(ids, n.Integration.ID) {
			ids = append(ids, n.Integration.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

// fetchIntegrationCatalog reads every item of each integration the flow uses,
// following the item search cursor. A 404 records the integration as unknown.
func fetchIntegrationCatalog(ctx context.Context, cli *client.Client, f flatFlow) (integrationCatalog, error) {
	catalog := integrationCatalog{}
	for _, id := range integrationIDs(f) {
		items, err := fetchIntegrationItems(ctx, cli, id)
		if err != nil {
			return nil, err
		}
		catalog[id] = items
	}
	return catalog, nil
}

func fetchIntegrationItems(ctx context.Context, cli *client.Client, id string) (map[string]catalogItem, error) {
	path := "/integrations/" + url.PathEscape(id) + "/items/search"
	items := map[string]catalogItem{}
	cursor := ""
	for {
		pagination := map[string]any{"limit": itemsPageLimit}
		if cursor != "" {
			pagination["cursor"] = cursor
		}
		body, err := json.Marshal(map[string]any{"pagination": pagination})
		if err != nil {
			return nil, err
		}
		status, respBody, err := cli.Do(ctx, "POST", path, nil, body)
		if err != nil {
			return nil, fmt.Errorf("read integration %q items: %w", id, err)
		}
		if status == 404 {
			return nil, nil
		}
		if status >= 400 {
			return nil, fmt.Errorf("read integration %q items: HTTP %d\n%s", id, status, formatJSON(respBody))
		}
		var page struct {
			Items      []catalogItem `json:"items"`
			NextCursor string        `json:"next_cursor"`
		}
		if err := json.Unmarshal(respBody, &page); err != nil {
			return nil, fmt.Errorf("decode integration %q items: %w", id, err)
		}
		for _, item := range page.Items {
			items[catalogKey(item.Kind, item.Key)] = item
		}
		if page.NextCursor == "" {
			return items, nil
		}
		cursor = page.NextCursor
	}
}

// validateIntegrationNode checks an integration node's own fields and its item
// against the catalog. The catalog check names the tool that fixes each gap.
func validateIntegrationNode(n flatNode, catalog integrationCatalog) []string {
	ref := *n.Integration
	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf("integration node %q: "+format, append([]any{n.ID}, args...)...))
	}
	if n.Kind != "" && n.Kind != requestKind {
		add("kind must be empty: the integration sets the node type")
	}
	if n.Method != "" || n.URL != "" || n.Body != "" || len(n.Headers) > 0 {
		add("remove method, url, body and headers: launch fills them from the integration")
	}
	if ref.ID == "" {
		add("integration.id is required (see search_integrations)")
		return problems
	}
	if (ref.Operation == "") == (ref.Event == "") {
		add("set exactly one of integration.operation and integration.event")
		return problems
	}
	if ref.Event != "" {
		if len(ref.Inputs) > 0 {
			add("an event takes no inputs")
		}
		for j, a := range n.Asserts {
			if a.Status != 0 {
				add("assert #%d: an event has no status; assert on its payload path", j)
			}
		}
	}
	_, itemProblems := checkCatalogItem(ref, catalog, mcpHints)
	for _, p := range itemProblems {
		add("%s", p)
	}
	return problems
}

// itemHints name the tools that fix a catalog problem, which differ between
// the MCP server and the CLI.
type itemHints struct {
	integrations string
	items        string
	accept       string
}

var mcpHints = itemHints{
	integrations: "see search_integrations",
	items:        "see search_integration_items",
	accept:       "accept it with set_integration_choices",
}

// checkCatalogItem finds the node's item in the workspace catalog and checks
// that the workspace offers it and that it takes the node's inputs.
func checkCatalogItem(ref flatIntegration, catalog integrationCatalog, hints itemHints) (catalogItem, []string) {
	var problems []string
	items, known := catalog[ref.ID]
	if !known || items == nil {
		return catalogItem{}, []string{fmt.Sprintf("integration %q does not exist (%s)", ref.ID, hints.integrations)}
	}
	item, ok := catalog.item(ref)
	if !ok {
		key := ref.Operation + ref.Event
		return catalogItem{}, []string{
			fmt.Sprintf("integration %q has no %s %q (%s)", ref.ID, itemKind(ref), key, hints.items),
		}
	}
	if item.Choice == choiceRefused {
		problems = append(problems,
			fmt.Sprintf("%s %q is refused in this workspace: %s", itemKind(ref), item.Key, hints.accept))
	}
	if item.Operation != nil {
		names := make([]string, 0, len(ref.Inputs))
		for name := range ref.Inputs {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if !slices.Contains(item.Operation.Params, catalogName{Name: name}) {
				problems = append(problems, fmt.Sprintf("%s takes no input %q", item.Key, name))
			}
		}
	}
	return item, problems
}

func itemKind(ref flatIntegration) string {
	if ref.Event != "" {
		return itemEvent
	}
	return itemOperation
}

// integrationOutputs are the outputs launch generates for an operation node,
// so other nodes may reference them.
func integrationOutputs(n flatNode, catalog integrationCatalog) []string {
	item, ok := catalog.item(*n.Integration)
	if !ok || item.Operation == nil {
		return nil
	}
	names := make([]string, 0, len(item.Operation.Outputs))
	for _, o := range item.Operation.Outputs {
		names = append(names, o.Name)
	}
	return names
}

// buildIntegrationNode fills the node type and data of an integration node. An
// operation node stores the operation's method and path: they only describe
// the call, launch replaces them.
func buildIntegrationNode(n flatNode, catalog integrationCatalog, node map[string]any) {
	ref := map[string]any{"id": n.Integration.ID}
	if len(n.Integration.Inputs) > 0 {
		ref["inputs"] = n.Integration.Inputs
	}
	if n.Integration.Event != "" {
		ref[itemEvent] = n.Integration.Event
		data := map[string]any{"integration": ref}
		if n.TimeoutMS > 0 {
			data["timeout_ms"] = n.TimeoutMS
		}
		node[keyType] = webhookWaitKind
		node["data"] = data
		return
	}
	ref[itemOperation] = n.Integration.Operation
	data := map[string]any{"integration": ref}
	if item, ok := catalog.item(*n.Integration); ok && item.Operation != nil {
		data["method"] = item.Operation.Method
		data["url"] = item.Operation.Path
	}
	node[keyType] = requestKind
	node["data"] = data
}
