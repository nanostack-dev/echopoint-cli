package commands

import (
	"net/http"
	"strings"
	"testing"
)

const stripeFlowFile = `
name: Stripe customer
nodes:
  customer:
    integration:
      id: stripe
      operation: customer.create
      inputs:
        email: garbage+stripe@nanostack.dev
    expect: "status: 200"
    next: created
  created:
    integration:
      id: stripe
      event: customer.created
      timeout_ms: 60000
    expect: "$.data.object.email equals garbage+stripe@nanostack.dev"
`

func stripeItems(choice string) map[string]any {
	return map[string]any{
		"items": []any{
			map[string]any{
				"kind": "operation", "key": "customer.create", "choice": choice,
				"operation": map[string]any{
					"method": "post", "path": "/v1/customers",
					"params": []any{map[string]any{"name": "email"}},
				},
			},
			map[string]any{"kind": "event", "key": "customer.created", "choice": "accepted"},
		},
	}
}

func TestFlowCreateFillsIntegrationNodesFromTheCatalog(t *testing.T) {
	file := writeTemp(t, "stripe.flow.yaml", stripeFlowFile)
	stub := newAPIStub(t)
	stub.on(http.MethodPost, "/integrations/stripe/items/search", http.StatusOK, stripeItems("accepted"))
	stub.on(http.MethodPost, "/flows", http.StatusCreated, sampleFlow(idFor("1"), "Stripe customer", nil))

	_, stderr, err := execute(t, newFlowCmd(stub.tableState(t)), "", "create", "-f", file)
	if err != nil {
		t.Fatalf("err %v stderr %s", err, stderr)
	}
	if n := len(stub.requestsTo(http.MethodPost, "/integrations/stripe/items/search")); n != 1 {
		t.Fatalf("catalog fetched %d times", n)
	}
	sent := bodyOf(t, stub.requestsTo(http.MethodPost, "/flows")[0])
	definition, _ := sent["flow_definition"].(map[string]any)
	nodes, _ := definition["nodes"].([]any)
	if len(nodes) != 2 {
		t.Fatalf("nodes %v", nodes)
	}
	customer, _ := nodes[0].(map[string]any)
	data, _ := customer["data"].(map[string]any)
	integration, _ := data["integration"].(map[string]any)
	if customer["type"] != "request" || data["method"] != "POST" || data["url"] != "/v1/customers" ||
		integration["operation"] != "customer.create" {
		t.Fatalf("customer %v", customer)
	}
	created, _ := nodes[1].(map[string]any)
	data, _ = created["data"].(map[string]any)
	integration, _ = data["integration"].(map[string]any)
	if created["type"] != "webhook_wait" || integration["event"] != "customer.created" {
		t.Fatalf("created %v", created)
	}
}

func TestFlowCreateRefusesIntegrationNodesTheCatalogRefuses(t *testing.T) {
	file := writeTemp(t, "stripe.flow.yaml", strings.Replace(stripeFlowFile,
		"email: garbage+stripe@nanostack.dev", "email: garbage+stripe@nanostack.dev\n        colour: red", 1))
	stub := newAPIStub(t)
	stub.on(http.MethodPost, "/integrations/stripe/items/search", http.StatusOK, stripeItems("refused"))

	_, _, err := execute(t, newFlowCmd(stub.tableState(t)), "", "create", "-f", file)
	if err == nil {
		t.Fatal("create succeeded")
	}
	for _, want := range []string{
		`integration node "customer": operation "customer.create" is refused in this workspace: accept it with echopoint integration accept`,
		`integration node "customer": customer.create takes no input "colour"`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("want %q in %v", want, err)
		}
	}
	if len(stub.requestsTo(http.MethodPost, "/flows")) != 0 {
		t.Fatal("flow was sent")
	}
}

func TestFlowCreateRefusesAnUnknownIntegration(t *testing.T) {
	file := writeTemp(t, "stripe.flow.yaml", stripeFlowFile)
	stub := newAPIStub(t)
	stub.on(http.MethodPost, "/integrations/stripe/items/search", http.StatusNotFound, map[string]any{})

	_, _, err := execute(t, newFlowCmd(stub.tableState(t)), "", "create", "-f", file)
	if err == nil ||
		!strings.Contains(err.Error(), `integration "stripe" does not exist (see echopoint integration list)`) {
		t.Fatalf("err %v", err)
	}
}
