package flowfile

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"sort"

	"echopoint-cli/internal/client"
)

// Integration nodes (Echopoint ADR-0024). A node with an "integration" object
// is a request node that calls an integration operation, or a webhook_wait
// node that waits for an integration event. Launch fills the call from the
// integration, so the author gives only the item and its inputs.
const (
	requestKind       = "request"
	keyType           = "type"
	extractorJSONPath = "json_path"
	itemOperation     = "operation"
	itemEvent         = "event"
	choiceRefused     = "refused"
	itemsPageLimit    = 100
)

// flatIntegration names the integration item a node uses.
type flatIntegration struct {
	ID        string         `json:"id"`
	Operation string         `json:"operation,omitempty"`
	Event     string         `json:"event,omitempty"`
	Inputs    map[string]any `json:"inputs,omitempty"`
}

// catalogItem is the part of an integration item the flow check applies a node
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

// itemHints name the commands that fix a catalog problem.
type itemHints struct {
	integrations string
	items        string
	accept       string
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

func formatJSON(b []byte) string {
	if len(b) == 0 {
		return "(empty response)"
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, b, "", "  "); err == nil {
		return pretty.String()
	}
	return string(b)
}
