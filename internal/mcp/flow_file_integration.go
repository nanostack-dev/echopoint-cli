package mcp

import (
	"context"
	"errors"
	"fmt"

	"echopoint-cli/internal/client"

	"github.com/nanostack-dev/echopoint-runner/pkg/flowdsl"
	"github.com/nanostack-dev/echopoint-runner/pkg/flowdsl/wire"
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
	catalog := integrationCatalog{}
	return flowdsl.FillIntegrations(req, func(node flowdsl.IntegrationNode) (flowdsl.Target, error) {
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
