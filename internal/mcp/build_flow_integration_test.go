package mcp

import (
	"encoding/json"
	"testing"
)

func testCatalog() integrationCatalog {
	return integrationCatalog{
		"stripe": {
			"operation:customer.create": {Kind: itemOperation, Key: "customer.create", Operation: &catalogOperation{
				Method: "POST", Path: "/v1/customers",
				Params:  []catalogName{{Name: "email"}},
				Outputs: []catalogName{{Name: "id"}},
			}},
			"operation:account.delete": {Kind: itemOperation, Key: "account.delete", Choice: choiceRefused,
				Operation: &catalogOperation{Method: "DELETE", Path: "/v1/accounts/{account}"}},
			"event:checkout.session.completed": {Kind: itemEvent, Key: "checkout.session.completed"},
		},
		"ghost": nil,
	}
}

func stripeFlow(nodes ...flatNode) flatFlow {
	edges := []string{}
	for i := 1; i < len(nodes); i++ {
		edges = append(edges, nodes[i-1].ID+">"+nodes[i].ID)
	}
	return flatFlow{Name: "stripe", Nodes: nodes, Edges: edges}
}

func TestValidateFlatFlowIntegrationNodes(t *testing.T) {
	op := func(key string, inputs map[string]any) *flatIntegration {
		return &flatIntegration{ID: "stripe", Operation: key, Inputs: inputs}
	}
	tests := []struct {
		name     string
		flow     flatFlow
		contains string // empty: the flow is valid
	}{
		{"operation and event reference the generated output", stripeFlow(
			flatNode{ID: "customer", Integration: op("customer.create", map[string]any{"email": "{{email}}"}),
				Asserts: []flatAssert{{Status: 200}}},
			flatNode{ID: "paid", Integration: &flatIntegration{ID: "stripe", Event: "checkout.session.completed"},
				TimeoutMS: 60000, Asserts: []flatAssert{{Path: "$.data.object.customer", Value: "{{customer.id}}"}}},
		), ""},
		{
			"unknown integration",
			stripeFlow(flatNode{ID: "a", Integration: &flatIntegration{ID: "ghost", Operation: "x"}}),
			"search_integrations",
		},
		{"unknown item", stripeFlow(flatNode{ID: "a", Integration: op("customer.nope", nil)}),
			"search_integration_items"},
		{"refused item", stripeFlow(flatNode{ID: "a", Integration: op("account.delete", nil)}),
			"set_integration_choices"},
		{
			"unknown input",
			stripeFlow(flatNode{ID: "a", Integration: op("customer.create", map[string]any{"mail": "x"})}),
			`takes no input "mail"`,
		},
		{"both operation and event", stripeFlow(flatNode{
			ID: "a",
			Integration: &flatIntegration{
				ID:        "stripe",
				Operation: "customer.create",
				Event:     "checkout.session.completed",
			},
		}),
			"exactly one of"},
		{
			"method set by the author",
			stripeFlow(flatNode{ID: "a", Method: "POST", Integration: op("customer.create", nil)}),
			"remove method",
		},
		{"event with a status assert", stripeFlow(flatNode{
			ID: "a",
			Integration: &flatIntegration{
				ID:    "stripe",
				Event: "checkout.session.completed",
			},
			Asserts: []flatAssert{{Status: 200}},
		}),
			"an event has no status"},
		{"input refers to an undeclared output", stripeFlow(flatNode{ID: "a",
			Integration: op("customer.create", map[string]any{"email": "{{other.mail}}"})}),
			"other"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			problems := validateFlatFlow(tt.flow, testCatalog())
			if tt.contains == "" {
				if len(problems) > 0 {
					t.Fatalf("want valid, got %v", problems)
				}
				return
			}
			if !containsSubstr(problems, tt.contains) {
				t.Fatalf("want a problem containing %q, got %v", tt.contains, problems)
			}
		})
	}
}

func TestBuildFlowDocIntegrationNodes(t *testing.T) {
	doc := buildFlowDoc(stripeFlow(
		flatNode{ID: "customer", Integration: &flatIntegration{ID: "stripe", Operation: "customer.create",
			Inputs: map[string]any{"email": "{{email}}"}}, Asserts: []flatAssert{{Status: 200}}},
		flatNode{ID: "paid", Integration: &flatIntegration{ID: "stripe", Event: "checkout.session.completed"},
			TimeoutMS: 60000},
	), testCatalog())

	raw, _ := json.Marshal(doc)
	var got struct {
		FlowDef struct {
			Nodes []struct {
				Type       string         `json:"type"`
				Data       map[string]any `json:"data"`
				Assertions []any          `json:"assertions"`
			} `json:"nodes"`
		} `json:"flow_definition"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal doc: %v", err)
	}
	call, wait := got.FlowDef.Nodes[0], got.FlowDef.Nodes[1]
	if call.Type != requestKind || call.Data["method"] != "POST" || call.Data["url"] != "/v1/customers" {
		t.Fatalf("operation node: %+v", call)
	}
	ref, _ := call.Data["integration"].(map[string]any)
	if ref["id"] != "stripe" || ref["operation"] != "customer.create" ||
		ref["inputs"].(map[string]any)["email"] != "{{email}}" {
		t.Fatalf("operation ref: %+v", ref)
	}
	if len(call.Assertions) != 1 {
		t.Fatalf("operation asserts: %+v", call.Assertions)
	}
	if wait.Type != webhookWaitKind || wait.Data["timeout_ms"] != float64(60000) {
		t.Fatalf("event node: %+v", wait)
	}
	if ref, _ := wait.Data["integration"].(map[string]any); ref["event"] != "checkout.session.completed" {
		t.Fatalf("event ref: %+v", ref)
	}
}
