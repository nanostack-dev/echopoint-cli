package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"echopoint-cli/internal/api"

	"github.com/gofrs/uuid/v5"
	googleuuid "github.com/google/uuid"
	"github.com/spf13/cobra"
)

// newFlowNodeCmd creates the node subcommand for flows
func newFlowNodeCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "node",
		Short: "Manage flow nodes",
	}

	cmd.AddCommand(
		newFlowNodeAddCmd(state),
		newFlowNodeRemoveCmd(state),
		newFlowNodeUpdateCmd(state),
		newFlowNodeOutputCmd(state),
		newFlowNodeAssertionCmd(state),
	)

	return cmd
}

// newFlowNodeAddCmd adds a new node to a flow
func newFlowNodeAddCmd(state *AppState) *cobra.Command {
	var nodeType, name, method, url, headers, body string
	var duration int

	cmd := &cobra.Command{
		Use:   "add <flow-id>",
		Short: "Add a node to the flow",
		Args:  cobra.ExactArgs(1),
		Long: `Add a new node to the flow.

Examples:
  # Add a request node
  echopoint flows node add <flow-id> --type request --name "API Call" --method POST --url "https://api.example.com"

  # Add a delay node
  echopoint flows node add <flow-id> --type delay --name "Wait" --duration 5000`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			flowID, err := googleuuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid flow ID: %w", err)
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

			// Generate new node ID (UUIDv7)
			nodeUUID, err := uuid.NewV7()
			if err != nil {
				return fmt.Errorf("failed to generate node ID: %w", err)
			}
			nodeID := nodeUUID.String()

			// Create node based on type
			var newNode api.FlowNode
			switch nodeType {
			case "request":
				if method == "" || url == "" {
					return fmt.Errorf("--method and --url are required for request nodes")
				}

				reqNode := api.RequestFlowNode{
					Id:          nodeID,
					Type:        "request",
					DisplayName: name,
					Data: api.RequestNodeData{
						Method:  api.RequestNodeDataMethod(method),
						Url:     url,
						Headers: parseHeaders(headers),
					},
				}

				if body != "" {
					reqNode.Data.Body = &body
				}

				newNode.FromRequestFlowNode(reqNode)

			case "delay":
				if duration <= 0 {
					return fmt.Errorf("--duration is required for delay nodes (in milliseconds)")
				}

				delayNode := api.DelayFlowNode{
					Id:          nodeID,
					Type:        "delay",
					DisplayName: name,
					Data: api.DelayNodeData{
						Duration: duration,
					},
				}
				newNode.FromDelayFlowNode(delayNode)

			default:
				return fmt.Errorf("invalid node type: %s (must be 'request' or 'delay')", nodeType)
			}

			// Add node to definition
			definition.Nodes = append(definition.Nodes, newNode)

			// Update flow with auto-layout enabled
			autoLayout := true
			updateReq := api.UpdateFlowRequest{
				FlowDefinition: &definition,
				AutoLayout:     &autoLayout,
			}

			// Debug: Print the request being sent
			if state.Debug {
				reqJSON, _ := json.MarshalIndent(updateReq, "", "  ")
				fmt.Fprintf(os.Stderr, "[DEBUG] UpdateFlowRequest: %s\n", string(reqJSON))
			}

			updateResp, err := state.Client.API().UpdateFlowWithResponse(context.Background(), flowID, updateReq)
			if err != nil {
				return fmt.Errorf("failed to update flow: %w", err)
			}
			if updateResp.JSON200 == nil {
				return formatAPIError(updateResp.HTTPResponse, updateResp.Body)
			}

			fmt.Printf("✓ Node added: %s\n", nodeID)
			fmt.Printf("  Type: %s\n", nodeType)
			fmt.Printf("  Name: %s\n", name)

			return nil
		},
	}

	cmd.Flags().StringVar(&nodeType, "type", "", "Node type (request or delay)")
	cmd.Flags().StringVar(&name, "name", "", "Node display name")
	cmd.Flags().StringVar(&method, "method", "", "HTTP method (for request nodes)")
	cmd.Flags().StringVar(&url, "url", "", "Request URL (for request nodes)")
	cmd.Flags().StringVar(&headers, "headers", "", "HTTP headers as JSON (for request nodes)")
	cmd.Flags().StringVar(&body, "body", "", "Request body (for request nodes)")
	cmd.Flags().IntVar(&duration, "duration", 0, "Delay duration in milliseconds (for delay nodes)")

	_ = cmd.MarkFlagRequired("type")
	_ = cmd.MarkFlagRequired("name")

	return cmd
}

// newFlowNodeRemoveCmd removes a node from a flow
func newFlowNodeRemoveCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <flow-id> <node-id>",
		Short: "Remove a node from the flow",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			flowID, err := googleuuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid flow ID: %w", err)
			}

			nodeID := args[1]

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

			// Find and remove node
			found := false
			newNodes := make([]api.FlowNode, 0, len(definition.Nodes))
			for _, node := range definition.Nodes {
				nodeData, _ := node.ValueByDiscriminator()
				switch n := nodeData.(type) {
				case api.RequestFlowNode:
					if n.Id != nodeID {
						newNodes = append(newNodes, node)
					} else {
						found = true
					}
				case api.DelayFlowNode:
					if n.Id != nodeID {
						newNodes = append(newNodes, node)
					} else {
						found = true
					}
				}
			}

			if !found {
				return fmt.Errorf("node not found: %s", nodeID)
			}

			definition.Nodes = newNodes

			// Also remove edges connected to this node
			newEdges := make([]api.FlowEdge, 0, len(definition.Edges))
			for _, edge := range definition.Edges {
				if edge.Source != nodeID && edge.Target != nodeID {
					newEdges = append(newEdges, edge)
				}
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

			fmt.Printf("✓ Node removed: %s\n", nodeID)

			return nil
		},
	}
}

// newFlowNodeUpdateCmd updates a node's properties
func newFlowNodeUpdateCmd(state *AppState) *cobra.Command {
	var name, method, url string

	cmd := &cobra.Command{
		Use:   "update <flow-id> <node-id>",
		Short: "Update a node's properties",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			flowID, err := googleuuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid flow ID: %w", err)
			}

			nodeID := args[1]

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

			// Find and update node
			found := false
			for i, node := range definition.Nodes {
				nodeData, _ := node.ValueByDiscriminator()
				switch n := nodeData.(type) {
				case api.RequestFlowNode:
					if n.Id == nodeID {
						if name != "" {
							n.DisplayName = name
						}
						if method != "" {
							n.Data.Method = api.RequestNodeDataMethod(method)
						}
						if url != "" {
							n.Data.Url = url
						}
						definition.Nodes[i].FromRequestFlowNode(n)
						found = true
					}
				case api.DelayFlowNode:
					if n.Id == nodeID {
						if name != "" {
							n.DisplayName = name
						}
						definition.Nodes[i].FromDelayFlowNode(n)
						found = true
					}
				}
			}

			if !found {
				return fmt.Errorf("node not found: %s", nodeID)
			}

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

			fmt.Printf("✓ Node updated: %s\n", nodeID)

			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "New display name")
	cmd.Flags().StringVar(&method, "method", "", "New HTTP method (request nodes only)")
	cmd.Flags().StringVar(&url, "url", "", "New URL (request nodes only)")

	return cmd
}

// newFlowNodeOutputCmd creates the output subcommand for nodes
func newFlowNodeOutputCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "output",
		Short: "Manage node outputs",
	}

	cmd.AddCommand(
		newFlowNodeOutputAddCmd(state),
		newFlowNodeOutputRemoveCmd(state),
	)

	return cmd
}

// newFlowNodeOutputAddCmd adds an output to a node
func newFlowNodeOutputAddCmd(state *AppState) *cobra.Command {
	var name, extractorType, path, headerName string

	cmd := &cobra.Command{
		Use:   "add <flow-id> <node-id>",
		Short: "Add an output to a node",
		Args:  cobra.ExactArgs(2),
		Long: `Add an output extractor to a node.

Examples:
  # Add a JSONPath extractor
  echopoint flows node output add <flow-id> <node-id> --name "token" --extractor jsonPath --path "$.token"

  # Add a status code extractor
  echopoint flows node output add <flow-id> <node-id> --name "status" --extractor statusCode

  # Add a body extractor
  echopoint flows node output add <flow-id> <node-id> --name "response" --extractor body

  # Add a header extractor
  echopoint flows node output add <flow-id> <node-id> --name "contentType" --extractor header --header-name "Content-Type"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			flowID, err := googleuuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid flow ID: %w", err)
			}

			nodeID := args[1]

			// Validate extractor type
			validExtractors := []string{"jsonPath", "statusCode", "body", "header"}
			if !containsString(validExtractors, extractorType) {
				return fmt.Errorf("invalid extractor type: %s (must be one of: %v)", extractorType, validExtractors)
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

			// Find node and add output
			found := false
			for i, node := range definition.Nodes {
				nodeData, _ := node.ValueByDiscriminator()
				switch n := nodeData.(type) {
				case api.RequestFlowNode:
					if n.Id == nodeID {
						newOutput := api.Output{
							Name: name,
							Extractor: struct {
								HeaderName *string           `json:"header_name,omitempty"`
								Path       *string           `json:"path,omitempty"`
								Type       api.ExtractorType `json:"type"`
							}{
								Type: api.ExtractorType(extractorType),
							},
						}

						if path != "" {
							newOutput.Extractor.Path = &path
						}
						if headerName != "" {
							newOutput.Extractor.HeaderName = &headerName
						}

						if n.Outputs == nil {
							outputs := []api.Output{newOutput}
							n.Outputs = &outputs
						} else {
							*n.Outputs = append(*n.Outputs, newOutput)
						}

						definition.Nodes[i].FromRequestFlowNode(n)
						found = true
					}
				case api.DelayFlowNode:
					if n.Id == nodeID {
						newOutput := api.Output{
							Name: name,
							Extractor: struct {
								HeaderName *string           `json:"header_name,omitempty"`
								Path       *string           `json:"path,omitempty"`
								Type       api.ExtractorType `json:"type"`
							}{
								Type: api.ExtractorType(extractorType),
							},
						}

						if path != "" {
							newOutput.Extractor.Path = &path
						}
						if headerName != "" {
							newOutput.Extractor.HeaderName = &headerName
						}

						if n.Outputs == nil {
							outputs := []api.Output{newOutput}
							n.Outputs = &outputs
						} else {
							*n.Outputs = append(*n.Outputs, newOutput)
						}

						definition.Nodes[i].FromDelayFlowNode(n)
						found = true
					}
				}
			}

			if !found {
				return fmt.Errorf("node not found: %s", nodeID)
			}

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

			fmt.Printf("✓ Output added: %s\n", name)
			fmt.Printf("  Extractor: %s\n", extractorType)

			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Output name")
	cmd.Flags().StringVar(&extractorType, "extractor", "", "Extractor type (jsonPath, statusCode, body, header)")
	cmd.Flags().StringVar(&path, "path", "", "Path for jsonPath extractor")
	cmd.Flags().StringVar(&headerName, "header-name", "", "Header name for header extractor")

	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("extractor")

	return cmd
}

// newFlowNodeOutputRemoveCmd removes an output from a node
func newFlowNodeOutputRemoveCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <flow-id> <node-id> <output-name>",
		Short: "Remove an output from a node",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			flowID, err := googleuuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid flow ID: %w", err)
			}

			nodeID := args[1]
			outputName := args[2]

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

			// Find node and remove output
			found := false
			for i, node := range definition.Nodes {
				nodeData, _ := node.ValueByDiscriminator()
				switch n := nodeData.(type) {
				case api.RequestFlowNode:
					if n.Id == nodeID && n.Outputs != nil {
						newOutputs := make([]api.Output, 0)
						for _, output := range *n.Outputs {
							if output.Name != outputName {
								newOutputs = append(newOutputs, output)
							} else {
								found = true
							}
						}
						if found {
							n.Outputs = &newOutputs
							definition.Nodes[i].FromRequestFlowNode(n)
						}
					}
				case api.DelayFlowNode:
					if n.Id == nodeID && n.Outputs != nil {
						newOutputs := make([]api.Output, 0)
						for _, output := range *n.Outputs {
							if output.Name != outputName {
								newOutputs = append(newOutputs, output)
							} else {
								found = true
							}
						}
						if found {
							n.Outputs = &newOutputs
							definition.Nodes[i].FromDelayFlowNode(n)
						}
					}
				}
			}

			if !found {
				return fmt.Errorf("output not found: %s", outputName)
			}

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

			fmt.Printf("✓ Output removed: %s\n", outputName)

			return nil
		},
	}
}

// newFlowNodeAssertionCmd creates the assertion subcommand for nodes
func newFlowNodeAssertionCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "assertion",
		Short: "Manage node assertions",
	}

	cmd.AddCommand(
		newFlowNodeAssertionAddCmd(state),
		newFlowNodeAssertionRemoveCmd(state),
	)

	return cmd
}

// newFlowNodeAssertionAddCmd adds an assertion to a node
func newFlowNodeAssertionAddCmd(state *AppState) *cobra.Command {
	var extractorType, path, operatorType, value string

	cmd := &cobra.Command{
		Use:   "add <flow-id> <node-id>",
		Short: "Add an assertion to a node",
		Args:  cobra.ExactArgs(2),
		Long: `Add an assertion to validate node execution.

Examples:
  # Assert status code equals 200
  echopoint flows node assertion add <flow-id> <node-id> --extractor statusCode --operator equals --value "200"

  # Assert JSONPath value equals expected
  echopoint flows node assertion add <flow-id> <node-id> --extractor jsonPath --path "$.name" --operator equals --value "test"

  # Assert response contains string
  echopoint flows node assertion add <flow-id> <node-id> --extractor body --operator contains --value "success"

Available operators: equals, notEquals, contains, notContains, greaterThan, lessThan, empty, notEmpty`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			flowID, err := googleuuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid flow ID: %w", err)
			}

			nodeID := args[1]

			// Validate extractor type
			validExtractors := []string{"statusCode", "jsonPath", "body", "header"}
			if !containsString(validExtractors, extractorType) {
				return fmt.Errorf("invalid extractor type: %s (must be one of: %v)", extractorType, validExtractors)
			}

			// Validate operator type
			validOperators := []string{
				"equals",
				"notEquals",
				"contains",
				"notContains",
				"greaterThan",
				"lessThan",
				"greaterThanOrEqual",
				"lessThanOrEqual",
				"empty",
				"notEmpty",
				"startsWith",
				"endsWith",
				"regex",
			}
			if !containsString(validOperators, operatorType) {
				return fmt.Errorf("invalid operator type: %s (must be one of: %v)", operatorType, validOperators)
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

			// Build extractor data
			extractorData := make(map[string]interface{})
			if path != "" {
				extractorData["path"] = path
			}

			// Build operator data
			operatorData := make(map[string]interface{})
			if value != "" {
				operatorData["value"] = value
			}

			// Find node and add assertion
			found := false
			for i, node := range definition.Nodes {
				nodeData, _ := node.ValueByDiscriminator()
				switch n := nodeData.(type) {
				case api.RequestFlowNode:
					if n.Id == nodeID {
						newAssertion := api.CompositeAssertion{
							ExtractorType: api.ExtractorType(extractorType),
							ExtractorData: extractorData,
							OperatorType:  api.OperatorType(operatorType),
							OperatorData:  operatorData,
						}

						if n.Assertions == nil {
							assertions := []api.CompositeAssertion{newAssertion}
							n.Assertions = &assertions
						} else {
							*n.Assertions = append(*n.Assertions, newAssertion)
						}

						definition.Nodes[i].FromRequestFlowNode(n)
						found = true
					}
				}
			}

			if !found {
				return fmt.Errorf("request node not found: %s", nodeID)
			}

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

			fmt.Printf("✓ Assertion added\n")
			fmt.Printf("  Extractor: %s\n", extractorType)
			fmt.Printf("  Operator: %s\n", operatorType)
			if value != "" {
				fmt.Printf("  Value: %s\n", value)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(
		&extractorType, "extractor", "", "Extractor type (statusCode, jsonPath, body, header)")
	cmd.Flags().StringVar(
		&path, "path", "", "Path for jsonPath extractor")
	cmd.Flags().StringVar(
		&operatorType, "operator", "", "Operator type (equals, notEquals, contains, etc.)")
	cmd.Flags().StringVar(
		&value, "value", "", "Expected value for comparison")

	_ = cmd.MarkFlagRequired("extractor")
	_ = cmd.MarkFlagRequired("operator")

	return cmd
}

// newFlowNodeAssertionRemoveCmd removes an assertion from a node
func newFlowNodeAssertionRemoveCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <flow-id> <node-id> <index>",
		Short: "Remove an assertion from a node by index",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			flowID, err := googleuuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid flow ID: %w", err)
			}

			nodeID := args[1]

			index, err := strconv.Atoi(args[2])
			if err != nil || index < 0 {
				return fmt.Errorf("invalid assertion index: %s", args[2])
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

			// Find node and remove assertion
			found := false
			for i, node := range definition.Nodes {
				nodeData, _ := node.ValueByDiscriminator()
				switch n := nodeData.(type) {
				case api.RequestFlowNode:
					if n.Id == nodeID && n.Assertions != nil {
						assertions := *n.Assertions
						if index >= len(assertions) {
							return fmt.Errorf(
								"assertion index out of range: %d (node has %d assertions)",
								index,
								len(assertions),
							)
						}

						newAssertions := append(assertions[:index], assertions[index+1:]...)
						n.Assertions = &newAssertions
						definition.Nodes[i].FromRequestFlowNode(n)
						found = true
					}
				}
			}

			if !found {
				return fmt.Errorf("node not found or has no assertions: %s", nodeID)
			}

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

			fmt.Printf("✓ Assertion removed at index: %d\n", index)

			return nil
		},
	}
}

// parseHeaders parses a JSON string into a map
func parseHeaders(headers string) *map[string]string {
	if headers == "" {
		return nil
	}

	var result map[string]string
	if err := json.Unmarshal([]byte(headers), &result); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to parse headers: %v\n", err)
		return nil
	}

	return &result
}

// Helper function to check if string is in slice
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if strings.EqualFold(item, s) {
			return true
		}
	}
	return false
}
