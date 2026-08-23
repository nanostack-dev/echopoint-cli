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
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/spf13/cobra"
)

// nodeTypeRequest is the "request" flow node type.
const nodeTypeRequest = "request"

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
	var nodeType, name, method, url, headers, body, moduleFlowID, customID, runWhen string
	var duration int
	var inputBindings, outputBindings, afterNodes []string

	cmd := &cobra.Command{
		Use:   "add <flow-id>",
		Short: "Add a node to the flow",
		Args:  cobra.ExactArgs(1),
		Long: `Add a new node to the flow.

Examples:
  # Add a request node
  echopoint flows node add <flow-id> --type request --name "API Call" --method POST --url "https://api.example.com"

  # Add a delay node
  echopoint flows node add <flow-id> --type delay --name "Wait" --duration 5000

  # Add a module node that runs another flow (reuse a flow inside this one)
  echopoint flows node add <flow-id> --type module --name "Login" \
    --flow-id <child-flow-id> \
    --input email={{userEmail}} --input password={{userPassword}} \
    --output token=authToken

  # Logical id + auto-wire an edge from an existing node (no separate 'edge add')
  echopoint flows node add <flow-id> --id get-product --after create-product \
    --type request --name "Get Product" --method GET --url ".../products/{{create-product.productId}}"

  # A cleanup node that runs even when an upstream branch has failed
  echopoint flows node add <flow-id> --id cleanup --run-when always --after get-product \
    --type request --name "Cleanup" --method DELETE --url ".../products/{{create-product.productId}}"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}

			flowID, err := googleuuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid flow ID: %w", err)
			}

			// Get current flow
			resp, err := state.Client.API().GetFlowWithResponse(context.Background(), flowID, nil)
			if err != nil {
				return fmt.Errorf("failed to get flow: %w", err)
			}
			if resp.JSON200 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}

			flow := resp.JSON200
			definition := flow.FlowDefinition

			// Node ID: a caller-provided logical id (unique within the flow) or
			// an auto-generated UUIDv7 when --id is omitted.
			var nodeID string
			if customID != "" {
				exists, existsErr := nodeIDExists(definition.Nodes, customID)
				if existsErr != nil {
					return existsErr
				}
				if exists {
					return fmt.Errorf("node ID %q already exists in this flow", customID)
				}
				nodeID = customID
			} else {
				nodeUUID, err := uuid.NewV7()
				if err != nil {
					return fmt.Errorf("failed to generate node ID: %w", err)
				}
				nodeID = nodeUUID.String()
			}

			// Optional run-when (on_success default, or always so cleanup nodes
			// still run after an upstream branch has failed).
			var runWhenPtr *api.FlowNodeRunWhen
			if runWhen != "" {
				if runWhen != string(api.OnSuccess) && runWhen != string(api.Always) {
					return fmt.Errorf("invalid --run-when %q (must be on_success or always)", runWhen)
				}
				rw := api.FlowNodeRunWhen(runWhen)
				runWhenPtr = &rw
			}

			newNode, buildErr := buildFlowNode(nodeBuildInput{
				id:             nodeID,
				nodeType:       nodeType,
				name:           name,
				method:         method,
				url:            url,
				headers:        headers,
				body:           body,
				duration:       duration,
				moduleFlowID:   moduleFlowID,
				inputBindings:  inputBindings,
				outputBindings: outputBindings,
				runWhen:        runWhenPtr,
			})
			if buildErr != nil {
				return buildErr
			}

			// Add node to definition
			definition.Nodes = append(definition.Nodes, newNode)

			// --after: auto-wire a success edge from each named node to this one.
			for _, src := range afterNodes {
				exists, existsErr := nodeIDExists(definition.Nodes, src)
				if existsErr != nil {
					return existsErr
				}
				if !exists {
					return fmt.Errorf("--after references unknown node %q", src)
				}
				edgeUUID, edgeErr := uuid.NewV7()
				if edgeErr != nil {
					return fmt.Errorf("failed to generate edge ID: %w", edgeErr)
				}
				definition.Edges = append(definition.Edges, api.FlowEdge{
					Id:     edgeUUID.String(),
					Source: src,
					Target: nodeID,
					Type:   api.FlowEdgeType(edgeTypeSuccess),
				})
			}

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

			updateResp, err := state.Client.API().UpdateFlowWithResponse(context.Background(), flowID, nil, updateReq)
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

	cmd.Flags().StringVar(&nodeType, "type", "", "Node type (request, delay or module)")
	cmd.Flags().StringVar(&customID, "id", "",
		"Custom node ID, unique within the flow (auto-generated UUID if omitted)")
	cmd.Flags().StringVar(&runWhen, "run-when", "",
		"When the node runs: on_success (default) or always (runs even after an upstream failure, e.g. cleanup)")
	cmd.Flags().StringArrayVar(&afterNodes, "after", nil,
		"Add a success edge from this node id to the new node (repeatable); auto-wires the graph")
	cmd.Flags().StringVar(&name, "name", "", "Node display name")
	cmd.Flags().StringVar(&method, "method", "", "HTTP method (for request nodes)")
	cmd.Flags().StringVar(&url, "url", "", "Request URL (for request nodes)")
	cmd.Flags().StringVar(&headers, "headers", "", "HTTP headers as JSON (for request nodes)")
	cmd.Flags().StringVar(&body, "body", "", "Request body (for request nodes)")
	cmd.Flags().IntVar(&duration, "duration", 0, "Delay duration in milliseconds (for delay nodes)")
	cmd.Flags().StringVar(&moduleFlowID, "flow-id", "", "Referenced child flow ID (for module nodes)")
	cmd.Flags().StringArrayVar(&inputBindings, "input", nil,
		"Module input binding key=value (for module nodes; repeatable)")
	cmd.Flags().StringArrayVar(&outputBindings, "output", nil,
		"Module output binding parentName=childKey (for module nodes; repeatable)")

	_ = cmd.MarkFlagRequired("type")
	_ = cmd.MarkFlagRequired("name")

	return cmd
}

// parseKeyVals parses repeated "key=value" flag entries into a map. The value
// may itself contain "=" (only the first separator splits the pair).
func parseKeyVals(pairs []string) (map[string]string, error) {
	result := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		key, value, found := strings.Cut(pair, "=")
		if !found || key == "" {
			return nil, fmt.Errorf("expected key=value, got %q", pair)
		}
		result[key] = value
	}
	return result, nil
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
			resp, err := state.Client.API().GetFlowWithResponse(context.Background(), flowID, nil)
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
				case api.ModuleFlowNode:
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

			updateResp, err := state.Client.API().UpdateFlowWithResponse(context.Background(), flowID, nil, updateReq)
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
			resp, err := state.Client.API().GetFlowWithResponse(context.Background(), flowID, nil)
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
							n.Data.Method = api.HttpMethod(method)
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
				case api.ModuleFlowNode:
					if n.Id == nodeID {
						if name != "" {
							n.DisplayName = name
						}
						definition.Nodes[i].FromModuleFlowNode(n)
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

			updateResp, err := state.Client.API().UpdateFlowWithResponse(context.Background(), flowID, nil, updateReq)
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
			resp, err := state.Client.API().GetFlowWithResponse(context.Background(), flowID, nil)
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

			updateResp, err := state.Client.API().UpdateFlowWithResponse(context.Background(), flowID, nil, updateReq)
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
			resp, err := state.Client.API().GetFlowWithResponse(context.Background(), flowID, nil)
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

			updateResp, err := state.Client.API().UpdateFlowWithResponse(context.Background(), flowID, nil, updateReq)
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

Available operators: equals, notEquals, contains, notContains, greaterThan, lessThan,
greaterThanOrEqual, lessThanOrEqual, empty, notEmpty, startsWith, endsWith, regex`,
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
			resp, err := state.Client.API().GetFlowWithResponse(context.Background(), flowID, nil)
			if err != nil {
				return fmt.Errorf("failed to get flow: %w", err)
			}
			if resp.JSON200 == nil {
				return formatAPIError(resp.HTTPResponse, resp.Body)
			}

			flow := resp.JSON200
			definition := flow.FlowDefinition

			// Build extractor data
			extractorData := make(map[string]any)
			if path != "" {
				extractorData["path"] = path
			}

			// Build operator data
			operatorData := make(map[string]any)
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

			updateResp, err := state.Client.API().UpdateFlowWithResponse(context.Background(), flowID, nil, updateReq)
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
			resp, err := state.Client.API().GetFlowWithResponse(context.Background(), flowID, nil)
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

			updateResp, err := state.Client.API().UpdateFlowWithResponse(context.Background(), flowID, nil, updateReq)
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
// nodeBuildInput carries the fields needed to construct a flow node.
type nodeBuildInput struct {
	id, nodeType, name, method, url, headers, body, moduleFlowID string
	duration                                                     int
	inputBindings, outputBindings                                []string
	runWhen                                                      *api.FlowNodeRunWhen
}

// buildFlowNode constructs a FlowNode of the requested type from CLI input.
func buildFlowNode(in nodeBuildInput) (api.FlowNode, error) {
	var node api.FlowNode
	switch in.nodeType {
	case nodeTypeRequest:
		if in.method == "" || in.url == "" {
			return node, fmt.Errorf("--method and --url are required for request nodes")
		}
		reqNode := api.RequestFlowNode{
			Id:          in.id,
			Type:        nodeTypeRequest,
			DisplayName: in.name,
			RunWhen:     in.runWhen,
			Data: api.RequestNodeData{
				Method:  api.HttpMethod(in.method),
				Url:     in.url,
				Headers: parseHeaders(in.headers),
			},
		}
		if in.body != "" {
			reqNode.Data.Body = &in.body
		}
		node.FromRequestFlowNode(reqNode)
	case "delay":
		if in.duration <= 0 {
			return node, fmt.Errorf("--duration is required for delay nodes (in milliseconds)")
		}
		node.FromDelayFlowNode(api.DelayFlowNode{
			Id:          in.id,
			Type:        "delay",
			DisplayName: in.name,
			RunWhen:     in.runWhen,
			Data:        api.DelayNodeData{Duration: in.duration},
		})
	case "module":
		moduleData, err := buildModuleData(in.moduleFlowID, in.inputBindings, in.outputBindings)
		if err != nil {
			return node, err
		}
		node.FromModuleFlowNode(api.ModuleFlowNode{
			Id:          in.id,
			Type:        "module",
			DisplayName: in.name,
			RunWhen:     in.runWhen,
			Data:        moduleData,
		})
	default:
		return node, fmt.Errorf("invalid node type: %s (must be 'request', 'delay' or 'module')", in.nodeType)
	}
	return node, nil
}

// buildModuleData assembles a module node's data from the child flow id and bindings.
func buildModuleData(moduleFlowID string, inputBindings, outputBindings []string) (api.ModuleNodeData, error) {
	var data api.ModuleNodeData
	if moduleFlowID == "" {
		return data, fmt.Errorf("--flow-id is required for module nodes")
	}
	childID, err := googleuuid.Parse(moduleFlowID)
	if err != nil {
		return data, fmt.Errorf("invalid --flow-id: %w", err)
	}
	data.FlowId = openapi_types.UUID(childID)

	if len(inputBindings) > 0 {
		inputs, ierr := parseKeyVals(inputBindings)
		if ierr != nil {
			return data, fmt.Errorf("invalid --input: %w", ierr)
		}
		bindings := make(map[string]any, len(inputs))
		for k, v := range inputs {
			bindings[k] = v
		}
		data.InputBindings = &bindings
	}
	if len(outputBindings) > 0 {
		outputs, oerr := parseKeyVals(outputBindings)
		if oerr != nil {
			return data, fmt.Errorf("invalid --output: %w", oerr)
		}
		data.OutputBindings = &outputs
	}
	return data, nil
}

// flowNodeID returns a node's ID. Every flow node carries an "id" field
// regardless of its type, so it is read generically rather than per type.
func flowNodeID(node api.FlowNode) (string, error) {
	raw, err := node.MarshalJSON()
	if err != nil {
		return "", fmt.Errorf("failed to read flow node: %w", err)
	}
	var probe struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return "", fmt.Errorf("failed to read flow node id: %w", err)
	}
	return probe.ID, nil
}

// nodeIDExists reports whether any node in the flow already uses the given ID
// (across all node types).
func nodeIDExists(nodes []api.FlowNode, id string) (bool, error) {
	for _, node := range nodes {
		existingID, err := flowNodeID(node)
		if err != nil {
			return false, err
		}
		if existingID == id {
			return true, nil
		}
	}
	return false, nil
}

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
