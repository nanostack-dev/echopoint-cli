package commands

import (
	"context"
	"fmt"

	"echopoint-cli/internal/api"

	"github.com/google/uuid"
)

func fetchFlowDefinition(ctx context.Context, state *AppState, flowID uuid.UUID) (api.FlowDefinition, error) {
	resp, err := state.Client.API().GetFlowWithResponse(ctx, flowID, nil)
	if err != nil {
		return api.FlowDefinition{}, fmt.Errorf("failed to get flow: %w", err)
	}
	if resp.JSON200 == nil {
		return api.FlowDefinition{}, formatAPIError(resp.HTTPResponse, resp.Body)
	}
	return resp.JSON200.FlowDefinition, nil
}

func saveFlowDefinition(ctx context.Context, state *AppState, flowID uuid.UUID, definition api.FlowDefinition) error {
	autoLayout := true
	request := api.UpdateFlowRequest{FlowDefinition: &definition, AutoLayout: &autoLayout}
	resp, err := state.Client.API().UpdateFlowWithResponse(ctx, flowID, nil, request)
	if err != nil {
		return fmt.Errorf("failed to update flow: %w", err)
	}
	if resp.JSON200 == nil {
		return formatAPIError(resp.HTTPResponse, resp.Body)
	}
	return nil
}

func updateFlowNode(node api.FlowNode, name, method, url string) (api.FlowNode, error) {
	value, err := node.ValueByDiscriminator()
	if err != nil {
		return node, fmt.Errorf("failed to read flow node: %w", err)
	}
	switch n := value.(type) {
	case api.RequestFlowNode:
		if name != "" {
			n.DisplayName = name
		}
		if method != "" {
			n.Data.Method = api.HttpMethod(method)
		}
		if url != "" {
			n.Data.Url = url
		}
		err = node.FromRequestFlowNode(n)
	case api.DelayFlowNode:
		if name != "" {
			n.DisplayName = name
		}
		err = node.FromDelayFlowNode(n)
	case api.ModuleFlowNode:
		if name != "" {
			n.DisplayName = name
		}
		err = node.FromModuleFlowNode(n)
	default:
		kind, readErr := node.Discriminator()
		if readErr != nil {
			return node, fmt.Errorf("failed to read flow node type: %w", readErr)
		}
		return node, fmt.Errorf("node update does not support editing node type %q", kind)
	}
	if err != nil {
		return node, fmt.Errorf("failed to encode flow node: %w", err)
	}
	return node, nil
}
