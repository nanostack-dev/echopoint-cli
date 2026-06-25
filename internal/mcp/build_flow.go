package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"echopoint-cli/internal/client"
)

// buildFlowToolName is the single ergonomic flow-authoring tool. It exists
// alongside the OpenAPI-derived create_flow: that one mirrors the raw nested
// flow document (data wrapper, doubly-nested assertions, edge handles) and is
// easy to get wrong; build_flow takes a flat node list and maps it to the full
// document server-side, validating {{node.output}} references and edge
// endpoints before anything is persisted.
const buildFlowToolName = "build_flow"

const flowsPath = "/flows"

const (
	moduleKind  = "module"
	requestKind = "request"
)

// Wire-format vocabulary for the flow document, hoisted so the builder reads as
// one contract and repeated keys stay consistent.
const (
	keyType             = "type"
	keyName             = "name"
	extractorJSONPath   = "jsonPath"
	extractorStatusCode = "statusCode"
	operatorEquals      = "equals"
	edgeTypeSuccess     = "success"
)

// flatAssert is one assertion in the ergonomic form. Either status (statusCode
// equals) or path (jsonPath + op) is set.
type flatAssert struct {
	Status int    `json:"status,omitempty"`
	Path   string `json:"path,omitempty"`
	Op     string `json:"op,omitempty"`
	Value  string `json:"value,omitempty"`
}

// flatNode is a request or module node without the data wrapper.
type flatNode struct {
	ID      string            `json:"id"`
	Kind    string            `json:"kind,omitempty"` // "request" (default) | "module"
	RunWhen string            `json:"run_when,omitempty"`
	Method  string            `json:"method,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
	Outputs map[string]string `json:"outputs,omitempty"` // request: name->jsonPath; module: name->childNode.key
	Asserts []flatAssert      `json:"asserts,omitempty"`
	FlowID  string            `json:"flow_id,omitempty"` // module only
	Inputs  map[string]string `json:"inputs,omitempty"`  // module only
}

func (n flatNode) kind() string {
	if n.Kind == moduleKind {
		return moduleKind
	}
	return requestKind
}

// flatFlow is the build_flow tool input.
type flatFlow struct {
	Name  string     `json:"name"`
	Tags  []string   `json:"tags,omitempty"`
	Nodes []flatNode `json:"nodes"`
	Edges []string   `json:"edges"` // "sourceId>targetId"
}

// refPattern matches a templated cross-node reference {{nodeId.output}}. Bare
// {{var}} (no dot) is an env/initial input and is intentionally not matched.
var refPattern = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_-]+)\.([A-Za-z0-9_$.\[\]-]+)\s*\}\}`)

// addBuildFlowTool registers build_flow on the server.
func addBuildFlowTool(srv *mcpsdk.Server, cli *client.Client) error {
	schema := &jsonschema.Schema{}
	if err := json.Unmarshal(buildFlowInputSchema, schema); err != nil {
		return fmt.Errorf("build_flow: invalid input schema: %w", err)
	}
	srv.AddTool(
		&mcpsdk.Tool{
			Name:        buildFlowToolName,
			Description: buildFlowDescription,
			InputSchema: schema,
		},
		buildFlowHandler(cli),
	)
	return nil
}

func buildFlowHandler(cli *client.Client) mcpsdk.ToolHandler {
	return func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		var flow flatFlow
		if len(req.Params.Arguments) > 0 {
			if err := json.Unmarshal(req.Params.Arguments, &flow); err != nil {
				return errResult(fmt.Sprintf("invalid arguments: %v", err)), nil
			}
		}

		if problems := validateFlatFlow(flow); len(problems) > 0 {
			return errResult("flow is not valid; fix and retry:\n  - " +
				strings.Join(problems, "\n  - ")), nil
		}

		body, err := json.Marshal(buildFlowDoc(flow))
		if err != nil {
			return errResult(fmt.Sprintf("encode flow document: %v", err)), nil
		}

		status, respBody, err := cli.Do(ctx, "POST", flowsPath, nil, body)
		if err != nil {
			return errResult(fmt.Sprintf("request failed: %v", err)), nil
		}
		text := formatJSON(respBody)
		if status >= 400 {
			return errResult(fmt.Sprintf("HTTP %d\n%s", status, text)), nil
		}
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: text}},
		}, nil
	}
}

// validateFlatFlow returns human-readable problems with the flat flow: missing
// ids, malformed edges, request/module field gaps, and — most usefully —
// {{node.output}} references whose node or output does not exist. Returning
// these to the caller closes the author→error→fix loop before the flow is
// persisted, where the failure would otherwise only surface at run time.
func validateFlatFlow(f flatFlow) []string {
	var problems []string
	if strings.TrimSpace(f.Name) == "" {
		problems = append(problems, "flow name is required")
	}

	ids, declaredOutputs, nodeProblems := validateNodes(f.Nodes)
	problems = append(problems, nodeProblems...)
	problems = append(problems, validateEdges(f.Edges, ids)...)
	problems = append(problems, validateRefs(f, ids, declaredOutputs)...)
	return problems
}

// validateNodes checks each node's required fields and returns the id set and
// the per-node declared outputs used by edge and reference validation.
func validateNodes(nodes []flatNode) (map[string]bool, map[string]map[string]bool, []string) {
	var problems []string
	ids := map[string]bool{}
	declaredOutputs := map[string]map[string]bool{}

	for i, n := range nodes {
		if n.ID == "" {
			problems = append(problems, fmt.Sprintf("node #%d has no id", i))
			continue
		}
		if ids[n.ID] {
			problems = append(problems, fmt.Sprintf("duplicate node id %q", n.ID))
		}
		ids[n.ID] = true

		outs := map[string]bool{}
		for k := range n.Outputs {
			outs[k] = true
		}
		declaredOutputs[n.ID] = outs

		problems = append(problems, validateNodeFields(n)...)
	}
	return ids, declaredOutputs, problems
}

// validateNodeFields validates the request/module-specific fields of one node.
func validateNodeFields(n flatNode) []string {
	var problems []string
	if n.kind() == moduleKind {
		if n.FlowID == "" {
			problems = append(problems, fmt.Sprintf("module node %q has no flow_id", n.ID))
		}
		return problems
	}
	if n.Method == "" {
		problems = append(problems, fmt.Sprintf("request node %q has no method", n.ID))
	}
	if n.URL == "" {
		problems = append(problems, fmt.Sprintf("request node %q has no url", n.ID))
	}
	for j, a := range n.Asserts {
		if a.Status == 0 && a.Path == "" {
			problems = append(problems,
				fmt.Sprintf("node %q assert #%d needs status or path", n.ID, j))
		}
	}
	return problems
}

// validateEdges checks every "sourceId>targetId" edge against the node id set.
func validateEdges(edges []string, ids map[string]bool) []string {
	var problems []string
	for _, e := range edges {
		src, dst, ok := strings.Cut(e, ">")
		src, dst = strings.TrimSpace(src), strings.TrimSpace(dst)
		if !ok || src == "" || dst == "" {
			problems = append(problems, fmt.Sprintf("edge %q must be \"sourceId>targetId\"", e))
			continue
		}
		if !ids[src] {
			problems = append(problems, fmt.Sprintf("edge %q: source node %q not found", e, src))
		}
		if !ids[dst] {
			problems = append(problems, fmt.Sprintf("edge %q: target node %q not found", e, dst))
		}
	}
	return problems
}

// validateRefs checks every {{node.output}} reference reachable from a node's
// request fields (url/body/headers/assert values) or a module's input bindings.
func validateRefs(f flatFlow, ids map[string]bool, declared map[string]map[string]bool) []string {
	var problems []string
	check := func(owner, text string) {
		for _, m := range refPattern.FindAllStringSubmatch(text, -1) {
			node, key := m[1], m[2]
			switch {
			case !ids[node]:
				problems = append(problems, fmt.Sprintf(
					"node %q refs {{%s.%s}} but node %q does not exist", owner, node, key, node))
			case !declared[node][key]:
				problems = append(problems, fmt.Sprintf(
					"node %q refs {{%s.%s}} but node %q declares no output %q", owner, node, key, node, key))
			}
		}
	}

	for _, n := range f.Nodes {
		switch n.kind() {
		case moduleKind:
			for _, v := range n.Inputs {
				check(n.ID, v)
			}
		default:
			check(n.ID, n.URL)
			check(n.ID, n.Body)
			for _, v := range n.Headers {
				check(n.ID, v)
			}
			for _, a := range n.Asserts {
				check(n.ID, a.Value)
			}
		}
	}
	return problems
}

// buildFlowDoc maps the flat flow onto the full create-flow request body: the
// per-node data wrapper, doubly-nested assertions, output extractors, success
// edges, and auto_layout so the backend computes node positions and handles.
func buildFlowDoc(f flatFlow) map[string]any {
	nodes := make([]map[string]any, 0, len(f.Nodes))
	for _, n := range f.Nodes {
		runWhen := n.RunWhen
		if runWhen == "" {
			runWhen = "on_success"
		}
		node := map[string]any{
			"id":           n.ID,
			"run_when":     runWhen,
			"display_name": n.ID,
		}

		switch n.kind() {
		case moduleKind:
			data := map[string]any{"flow_id": n.FlowID}
			if len(n.Inputs) > 0 {
				data["input_bindings"] = n.Inputs
			}
			if len(n.Outputs) > 0 {
				data["output_bindings"] = n.Outputs
			}
			node[keyType] = moduleKind
			node["data"] = data
		default:
			data := map[string]any{"url": n.URL, "method": n.Method}
			if n.Body != "" {
				data["body"] = n.Body
			}
			if len(n.Headers) > 0 {
				data["headers"] = n.Headers
			}
			node[keyType] = requestKind
			node["data"] = data
			if outs := buildOutputs(n.Outputs); len(outs) > 0 {
				node["outputs"] = outs
			}
			if len(n.Asserts) > 0 {
				asserts := make([]map[string]any, 0, len(n.Asserts))
				for _, a := range n.Asserts {
					asserts = append(asserts, buildAssertion(a))
				}
				node["assertions"] = asserts
			}
		}
		nodes = append(nodes, node)
	}

	edges := make([]map[string]any, 0, len(f.Edges))
	for i, e := range f.Edges {
		src, dst, _ := strings.Cut(e, ">")
		edges = append(edges, map[string]any{
			"id":     fmt.Sprintf("e%d", i+1),
			"source": strings.TrimSpace(src),
			"target": strings.TrimSpace(dst),
			keyType:  edgeTypeSuccess,
		})
	}

	doc := map[string]any{
		keyName:       f.Name,
		"auto_layout": true,
		"flow_definition": map[string]any{
			keyName:   "",
			"version": "1.0",
			"nodes":   nodes,
			"edges":   edges,
		},
	}
	if len(f.Tags) > 0 {
		doc["tags"] = f.Tags
	}
	return doc
}

// buildOutputs converts name->jsonPath into the request node output shape, in
// stable key order so the document is deterministic.
func buildOutputs(m map[string]string) []map[string]any {
	if len(m) == 0 {
		return nil
	}
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	out := make([]map[string]any, 0, len(names))
	for _, name := range names {
		out = append(out, map[string]any{
			keyName:     name,
			"extractor": map[string]any{keyType: extractorJSONPath, "path": m[name]},
		})
	}
	return out
}

// buildAssertion expands the shorthand into the full nested assertion. status
// becomes a statusCode-equals check; otherwise it is a jsonPath check with the
// given operator (default equals).
func buildAssertion(a flatAssert) map[string]any {
	if a.Status != 0 {
		return map[string]any{
			"extractor_type": extractorStatusCode,
			"extractor_data": map[string]any{},
			"operator_type":  operatorEquals,
			"operator_data":  map[string]any{"value": strconv.Itoa(a.Status)},
		}
	}
	op := a.Op
	if op == "" {
		op = operatorEquals
	}
	extractorData := map[string]any{}
	if a.Path != "" {
		extractorData["path"] = a.Path
	}
	operatorData := map[string]any{}
	if a.Value != "" {
		operatorData["value"] = a.Value
	}
	return map[string]any{
		"extractor_type": extractorJSONPath,
		"extractor_data": extractorData,
		"operator_type":  op,
		"operator_data":  operatorData,
	}
}
