package commands

import (
	"testing"

	"echopoint-cli/internal/api"
)

func requestNode(id string) api.FlowNode {
	var n api.FlowNode
	n.FromRequestFlowNode(api.RequestFlowNode{Id: id, Type: nodeTypeRequest})
	return n
}

func TestFlowNodeID(t *testing.T) {
	got, err := flowNodeID(requestNode("create-product"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "create-product" {
		t.Errorf("expected create-product, got %q", got)
	}
}

func TestNodeIDExists(t *testing.T) {
	nodes := []api.FlowNode{requestNode("create-product"), requestNode("get-product")}

	if exists, err := nodeIDExists(nodes, "create-product"); err != nil || !exists {
		t.Errorf("expected existing id to be found (exists=%v err=%v)", exists, err)
	}
	if exists, err := nodeIDExists(nodes, "delete-product"); err != nil || exists {
		t.Errorf("expected absent id to be missing (exists=%v err=%v)", exists, err)
	}
	if exists, err := nodeIDExists(nil, "anything"); err != nil || exists {
		t.Errorf("expected no ids in an empty flow (exists=%v err=%v)", exists, err)
	}
}

func TestParseKeyVals(t *testing.T) {
	t.Run("parses pairs", func(t *testing.T) {
		got, err := parseKeyVals([]string{"email={{userEmail}}", "token=authToken"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got["email"] != "{{userEmail}}" || got["token"] != "authToken" {
			t.Errorf("unexpected map: %#v", got)
		}
	})

	t.Run("keeps = in value", func(t *testing.T) {
		got, err := parseKeyVals([]string{"q=a=b=c"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got["q"] != "a=b=c" {
			t.Errorf("got %q, want %q", got["q"], "a=b=c")
		}
	})

	t.Run("allows empty value", func(t *testing.T) {
		got, err := parseKeyVals([]string{"k="})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if v, ok := got["k"]; !ok || v != "" {
			t.Errorf("got %#v, want empty value for k", got)
		}
	})

	t.Run("rejects missing separator", func(t *testing.T) {
		if _, err := parseKeyVals([]string{"noequals"}); err == nil {
			t.Error("expected error for missing =")
		}
	})

	t.Run("rejects empty key", func(t *testing.T) {
		if _, err := parseKeyVals([]string{"=value"}); err == nil {
			t.Error("expected error for empty key")
		}
	})
}
