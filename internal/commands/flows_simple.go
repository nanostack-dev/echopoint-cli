package commands

import (
	"context"
	"fmt"

	"echopoint-cli/internal/api"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// newFlowInteractiveCmd creates a simplified interactive flow builder
func newFlowInteractiveCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create-interactive",
		Short: "Create a flow interactively (simplified)",
		Long: `Create a new flow through interactive prompts.

This command will guide you through creating a basic flow.
For advanced features, use the TUI: echopoint tui`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			// Get flow name from flag or use default
			name, _ := cmd.Flags().GetString("name")
			if name == "" {
				name = "New Flow"
			}

			// Create a simple flow with empty definition
			req := api.CreateFlowRequest{
				Name: name,
				FlowDefinition: api.FlowDefinition{
					Nodes: []api.FlowNode{},
					Edges: []api.FlowEdge{},
				},
			}

			resp, err := state.Client.API().CreateFlowWithResponse(context.Background(), req)
			if err != nil {
				return fmt.Errorf("failed to create flow: %w", err)
			}

			if resp.JSON201 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}

			flow := resp.JSON201
			fmt.Printf("✓ Flow created: %s\n", flow.Name)
			fmt.Printf("  ID: %s\n", flow.Id)
			fmt.Println("\nNext steps:")
			fmt.Printf("  View flow:   echopoint flows get %s\n", flow.Id)
			fmt.Printf("  Open TUI:    echopoint tui\n")
			fmt.Println("\nNote: Use the TUI (echopoint tui) for interactive flow editing")

			return nil
		},
	}

	cmd.Flags().String("name", "", "Flow name")

	return cmd
}

// newFlowShowCmd displays flow information
func newFlowShowCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   "show <flow-id>",
		Short: "Display flow details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			flowID, err := uuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid flow ID: %w", err)
			}

			resp, err := state.Client.API().GetFlowWithResponse(context.Background(), flowID)
			if err != nil {
				return fmt.Errorf("failed to get flow: %w", err)
			}

			if resp.JSON200 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}

			flow := resp.JSON200

			fmt.Printf("\nFlow: %s\n", flow.Name)
			fmt.Printf("ID: %s\n", flow.Id)
			if flow.Description != nil {
				fmt.Printf("Description: %s\n", *flow.Description)
			}
			fmt.Printf("Version: %s\n", flow.Version)
			fmt.Printf("Created: %s\n", flow.CreatedAt)
			fmt.Printf("Updated: %s\n", flow.UpdatedAt)

			// Count nodes and edges
			fmt.Printf("\nStructure:\n")
			fmt.Printf("  Nodes: %d\n", len(flow.FlowDefinition.Nodes))
			fmt.Printf("  Edges: %d\n", len(flow.FlowDefinition.Edges))

			if len(flow.FlowDefinition.Nodes) > 0 {
				fmt.Printf("\nNodes: %d (view in TUI for details)\n", len(flow.FlowDefinition.Nodes))
			}

			fmt.Println()

			return nil
		},
	}
}
