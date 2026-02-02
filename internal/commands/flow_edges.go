package commands

import (
	"context"
	"fmt"

	"echopoint-cli/internal/api"

	"github.com/gofrs/uuid/v5"
	googleuuid "github.com/google/uuid"
	"github.com/spf13/cobra"
)

// newFlowEdgeCmd creates the edge subcommand for flows
func newFlowEdgeCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edge",
		Short: "Manage flow edges",
	}

	cmd.AddCommand(
		newFlowEdgeAddCmd(state),
		newFlowEdgeRemoveCmd(state),
	)

	return cmd
}

// newFlowEdgeAddCmd adds an edge between nodes
func newFlowEdgeAddCmd(state *AppState) *cobra.Command {
	var fromNode, toNode, edgeType string

	cmd := &cobra.Command{
		Use:   "add <flow-id>",
		Short: "Add an edge between nodes",
		Args:  cobra.ExactArgs(1),
		Long: `Add a connection (edge) between two nodes.

Examples:
  # Add a success edge
  echopoint flows edge add <flow-id> --from <node1-id> --to <node2-id> --type success

  # Add a failure edge
  echopoint flows edge add <flow-id> --from <node1-id> --to <node2-id> --type failure`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			flowID, err := googleuuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid flow ID: %w", err)
			}

			// Validate edge type
			validTypes := []string{"success", "failure"}
			if !containsString(validTypes, edgeType) {
				return fmt.Errorf("invalid edge type: %s (must be 'success' or 'failure')", edgeType)
			}

			// Get current flow
			resp, err := state.Client.API().GetFlowWithResponse(context.Background(), flowID)
			if err != nil {
				return fmt.Errorf("failed to get flow: %w", err)
			}
			if resp.JSON200 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}

			flow := resp.JSON200
			definition := flow.FlowDefinition

			// Validate that source and target nodes exist
			sourceExists := false
			targetExists := false
			for _, node := range definition.Nodes {
				nodeData, _ := node.ValueByDiscriminator()
				switch n := nodeData.(type) {
				case api.RequestFlowNode:
					if n.Id == fromNode {
						sourceExists = true
					}
					if n.Id == toNode {
						targetExists = true
					}
				case api.DelayFlowNode:
					if n.Id == fromNode {
						sourceExists = true
					}
					if n.Id == toNode {
						targetExists = true
					}
				}
			}

			if !sourceExists {
				return fmt.Errorf("source node not found: %s", fromNode)
			}
			if !targetExists {
				return fmt.Errorf("target node not found: %s", toNode)
			}

			// Check if edge already exists
			for _, edge := range definition.Edges {
				if edge.Source == fromNode && edge.Target == toNode {
					return fmt.Errorf("edge already exists from %s to %s", fromNode, toNode)
				}
			}

			// Generate edge ID (UUIDv7)
			edgeUUID, err := uuid.NewV7()
			if err != nil {
				return fmt.Errorf("failed to generate edge ID: %w", err)
			}
			edgeID := edgeUUID.String()

			// Create new edge
			newEdge := api.FlowEdge{
				Id:     edgeID,
				Source: fromNode,
				Target: toNode,
				Type:   api.FlowEdgeType(edgeType),
			}

			// Add edge to definition
			definition.Edges = append(definition.Edges, newEdge)

			// Update flow with auto-layout enabled
			autoLayout := true
			updateReq := api.UpdateFlowRequest{
				FlowDefinition: &definition,
				AutoLayout:     &autoLayout,
			}

			updateResp, err := state.Client.API().UpdateFlowWithResponse(context.Background(), flowID, updateReq)
			if err != nil {
				return fmt.Errorf("failed to update flow: %w", err)
			}
			if updateResp.JSON200 == nil {
				return formatAPIError(updateResp.HTTPResponse, updateResp.Body)
			}

			fmt.Printf("✓ Edge added: %s\n", edgeID)
			fmt.Printf("  From: %s\n", fromNode)
			fmt.Printf("  To: %s\n", toNode)
			fmt.Printf("  Type: %s\n", edgeType)

			return nil
		},
	}

	cmd.Flags().StringVar(
		&fromNode, "from", "", "Source node ID")
	cmd.Flags().StringVar(
		&toNode, "to", "", "Target node ID")
	cmd.Flags().StringVar(
		&edgeType, "type", "success", "Edge type (success or failure)")

	_ = cmd.MarkFlagRequired("from")
	_ = cmd.MarkFlagRequired("to")

	return cmd
}

// newFlowEdgeRemoveCmd removes an edge from a flow
func newFlowEdgeRemoveCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <flow-id> <edge-id>",
		Short: "Remove an edge from the flow",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			flowID, err := googleuuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid flow ID: %w", err)
			}

			edgeID := args[1]

			// Get current flow
			resp, err := state.Client.API().GetFlowWithResponse(context.Background(), flowID)
			if err != nil {
				return fmt.Errorf("failed to get flow: %w", err)
			}
			if resp.JSON200 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}

			flow := resp.JSON200
			definition := flow.FlowDefinition

			// Find and remove edge
			found := false
			newEdges := make([]api.FlowEdge, 0, len(definition.Edges))
			for _, edge := range definition.Edges {
				if edge.Id != edgeID {
					newEdges = append(newEdges, edge)
				} else {
					found = true
				}
			}

			if !found {
				return fmt.Errorf("edge not found: %s", edgeID)
			}

			definition.Edges = newEdges

			// Update flow with auto-layout enabled
			autoLayout := true
			updateReq := api.UpdateFlowRequest{
				FlowDefinition: &definition,
				AutoLayout:     &autoLayout,
			}

			updateResp, err := state.Client.API().UpdateFlowWithResponse(context.Background(), flowID, updateReq)
			if err != nil {
				return fmt.Errorf("failed to update flow: %w", err)
			}
			if updateResp.JSON200 == nil {
				return formatAPIError(updateResp.HTTPResponse, updateResp.Body)
			}

			fmt.Printf("✓ Edge removed: %s\n", edgeID)

			return nil
		},
	}
}
