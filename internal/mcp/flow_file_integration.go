package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"echopoint-cli/internal/client"

	runnerflow "github.com/nanostack-dev/echopoint-runner/pkg/flow"
	"github.com/nanostack-dev/echopoint-runner/pkg/flowdsl"
	"github.com/nanostack-dev/echopoint-runner/pkg/flowdsl/wire"
	"github.com/nanostack-dev/echopoint-runner/pkg/operators"
)

var cliHints = itemHints{
	integrations: "see echopoint integration list",
	items:        "see echopoint integration items",
	accept:       "accept it with echopoint integration accept",
}

// FlowFileIntegrations checks the integration nodes of a compiled flow file
// against the workspace catalog, with the build_flow rules, and stores each
// operation's method and path. The flow compiler works offline and leaves
// them empty; the API requires both.
func FlowFileIntegrations(ctx context.Context, cli *client.Client, req *wire.CreateFlowRequest) error {
	_, err := fillFlowFile(ctx, cli, req)
	return err
}

func fillFlowFile(ctx context.Context, cli *client.Client, req *wire.CreateFlowRequest) (integrationCatalog, error) {
	catalog := integrationCatalog{}
	unread := map[string]bool{}
	return catalog, flowdsl.FillIntegrations(req, func(node flowdsl.IntegrationNode) (flowdsl.Target, error) {
		if unread[node.Ref.Id] {
			// The first node of this integration already reported why.
			return flowdsl.Target{}, nil
		}
		ref := flatIntegration{ID: node.Ref.Id}
		if node.Ref.Operation != nil {
			ref.Operation = *node.Ref.Operation
		}
		if node.Ref.Event != nil {
			ref.Event = *node.Ref.Event
		}
		if node.Ref.Inputs != nil {
			ref.Inputs = *node.Ref.Inputs
		}
		if _, fetched := catalog[ref.ID]; !fetched {
			items, err := fetchIntegrationItems(ctx, cli, ref.ID)
			if err != nil {
				unread[ref.ID] = true
				return flowdsl.Target{}, err
			}
			catalog[ref.ID] = items
		}
		item, problems := checkCatalogItem(ref, catalog, cliHints)
		if len(problems) > 0 {
			errs := make([]error, 0, len(problems))
			for _, p := range problems {
				errs = append(errs, fmt.Errorf("integration node %q: %s", node.NodeID, p))
			}
			return flowdsl.Target{}, errors.Join(errs...)
		}
		if item.Operation == nil {
			return flowdsl.Target{}, nil
		}
		return flowdsl.Target{Method: item.Operation.Method, URL: item.Operation.Path}, nil
	})
}

// CheckFlowFileReferences checks a compiled flow file with the rule the API
// applies when it saves the flow. The compiler works offline, so it cannot
// tell which outputs an integration operation has. This check fills a copy of
// the flow from the workspace catalog, declares each operation's catalog
// outputs as launch will, and lets the runner check every node reference. A
// flow without integration nodes is left to the compiler and makes no request.
func CheckFlowFileReferences(ctx context.Context, cli *client.Client, req wire.CreateFlowRequest) error {
	encoded, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if !bytes.Contains(encoded, []byte(`"integration"`)) {
		return nil
	}
	var filled wire.CreateFlowRequest
	if err = json.Unmarshal(encoded, &filled); err != nil {
		return err
	}
	catalog, err := fillFlowFile(ctx, cli, &filled)
	if err != nil {
		return err
	}
	definition, err := json.Marshal(filled.FlowDefinition)
	if err != nil {
		return err
	}
	var tree any
	if err = json.Unmarshal(definition, &tree); err != nil {
		return err
	}
	declareOperationOutputs(tree, catalog)
	if definition, err = json.Marshal(tree); err != nil {
		return err
	}
	_, err = runnerflow.ParseFromJSONWithOptions(definition, runnerflow.ParseOptions{AllowUnknownInitialInputs: true})
	return err
}

// extractorPathKey is the json_path extractor field that holds the path.
const extractorPathKey = "path"

// declareOperationOutputs adds to every operation node, poll and loop bodies
// included, the catalog outputs it does not declare yet.
func declareOperationOutputs(value any, catalog integrationCatalog) {
	switch typed := value.(type) {
	case map[string]any:
		if typed[keyType] == requestKind {
			declareNodeOutputs(typed, catalog)
		}
		for _, child := range typed {
			declareOperationOutputs(child, catalog)
		}
	case []any:
		for _, child := range typed {
			declareOperationOutputs(child, catalog)
		}
	}
}

func declareNodeOutputs(node map[string]any, catalog integrationCatalog) {
	data, _ := node["data"].(map[string]any)
	ref, _ := data["integration"].(map[string]any)
	id, _ := ref["id"].(string)
	operation, _ := ref[itemOperation].(string)
	item, ok := catalog.item(flatIntegration{ID: id, Operation: operation})
	if !ok || item.Operation == nil {
		return
	}
	outputs, _ := node["outputs"].([]any)
	if !acceptsSuccess(node["assertions"]) {
		return
	}
	taken := map[string]bool{}
	for _, output := range outputs {
		if declared, isMap := output.(map[string]any); isMap {
			taken[fmt.Sprint(declared["name"])] = true
		}
	}
	for _, output := range item.Operation.Outputs {
		if taken[output.Name] {
			continue
		}
		path := output.Path
		if path == "" {
			path = "$"
		}
		outputs = append(outputs, map[string]any{
			"name":      output.Name,
			"extractor": map[string]any{keyType: extractorJSONPath, extractorPathKey: path},
		})
	}
	node["outputs"] = outputs
}

// acceptsSuccess mirrors launch: a node whose status assertions accept no 2xx
// expects an error body, such as Stripe's 402 for a declined card, and gets no
// catalog outputs. An assertion it cannot evaluate, such as a templated value,
// counts as passing.
func acceptsSuccess(assertions any) bool {
	checks, _ := assertions.([]any)
	for status := 200; status < 300; status++ {
		if !slices.ContainsFunc(checks, func(check any) bool { return !statusPasses(check, status) }) {
			return true
		}
	}
	return false
}

func statusPasses(check any, status int) bool {
	assertion, _ := check.(map[string]any)
	if assertion["extractor_type"] != "status_code" {
		return true
	}
	data, _ := assertion["operator_data"].(map[string]any)
	expected := data["value"]
	if text, isText := expected.(string); isText && strings.Contains(text, "{{") {
		return true
	}
	passes, err := operators.Compare(operators.OperatorType(fmt.Sprint(assertion["operator_type"])), status, expected)
	return passes || err != nil
}
