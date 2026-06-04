package commands

import (
	"testing"

	"echopoint-cli/internal/api"
)

func requestNodeWithURL(id, url string) api.FlowNode {
	var n api.FlowNode
	n.FromRequestFlowNode(api.RequestFlowNode{
		Id:   id,
		Type: nodeTypeRequest,
		Data: api.RequestNodeData{Method: "GET", Url: url},
	})
	return n
}

func TestReachable(t *testing.T) {
	adj := map[string][]string{
		"a": {"b"},
		"b": {"c"},
	}
	if !reachable("a", "c", adj) {
		t.Error("expected a to reach c")
	}
	if reachable("c", "a", adj) {
		t.Error("expected c not to reach a")
	}
}

func TestFindCycle(t *testing.T) {
	acyclic := map[string][]string{"a": {"b"}, "b": {"c"}}
	if c := findCycle(map[string]bool{"a": true, "b": true, "c": true}, acyclic); c != "" {
		t.Errorf("expected no cycle, got %q", c)
	}

	cyclic := map[string][]string{"a": {"b"}, "b": {"c"}, "c": {"a"}}
	if c := findCycle(map[string]bool{"a": true, "b": true, "c": true}, cyclic); c == "" {
		t.Error("expected a cycle to be detected")
	}
}

func TestValidateReferences(t *testing.T) {
	nodes := []api.FlowNode{
		requestNodeWithURL("create-product", "https://x/products"),
		requestNodeWithURL("get-product", "https://x/products/{{create-product.productId}}"),
	}

	ids := map[string]bool{"create-product": true, "get-product": true}

	// With the edge create-product -> get-product, the reference resolves.
	adjOK := map[string][]string{"create-product": {"get-product"}}
	if got := validateReferences(nodes, ids, adjOK); len(got) != 0 {
		t.Errorf("expected no problems, got %v", got)
	}

	// Without the edge, the reference is unreachable.
	if got := validateReferences(nodes, ids, nil); len(got) != 1 {
		t.Errorf("expected 1 unreachable-reference problem, got %v", got)
	}
}
