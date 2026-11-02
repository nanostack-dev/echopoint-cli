package flowdsl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	"echopoint-cli/internal/api"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// Decompile renders a stored flow as a flow file. Compiling that file produces
// the same create request the API would accept for this flow. Edge ids and
// pinned borders are left out: compile lays the graph out itself.
func Decompile(flow api.Flow) ([]byte, error) {
	root, err := flowFile(flow)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func flowFile(flow api.Flow) (*yaml.Node, error) {
	nodes, err := nodesMap(flow.FlowDefinition.Nodes, flow.FlowDefinition.Edges)
	if err != nil {
		return nil, err
	}
	file := newMap()
	file.str("name", flow.Name)
	if text := flowDescription(flow); text != "" {
		file.str("description", text)
	}
	if flow.Role != "" {
		file.str("role", string(flow.Role))
	}
	if len(flow.Tags) > 0 {
		file.raw("tags", stringSeq(flow.Tags))
	}
	version := flow.Version
	if version == "" {
		version = defaultFlowVersion
	}
	file.str("version", version)
	if flow.FolderId != nil && *flow.FolderId != uuid.Nil {
		file.str("folder", flow.FolderId.String())
	}
	if flow.Id != uuid.Nil {
		file.str("id", flow.Id.String())
	}
	file.raw("nodes", nodes)
	return file.node, nil
}

// flowDescription prefers the catalog description. A definition that carries
// the only description is kept, so a file still has one description field.
func flowDescription(flow api.Flow) string {
	if flow.Description != nil && *flow.Description != "" {
		return *flow.Description
	}
	if flow.FlowDefinition.Description != nil {
		return *flow.FlowDefinition.Description
	}
	return ""
}

func nodesMap(nodes []api.FlowNode, edges []api.FlowEdge) (*yaml.Node, error) {
	if len(nodes) == 0 {
		return nil, fmt.Errorf("flow has no nodes")
	}
	out := newMap()
	for _, node := range nodes {
		id, body, err := oneNode(node, edges)
		if err != nil {
			return nil, err
		}
		out.raw(id, body)
	}
	return out.node, nil
}

type nodeShell struct {
	id      string
	display string
	runWhen string
	kind    string
	checks  []api.CompositeAssertion
	outputs []api.Output
}

func oneNode(node api.FlowNode, edges []api.FlowEdge) (string, *yaml.Node, error) {
	raw, err := json.Marshal(node)
	if err != nil {
		return "", nil, err
	}
	shell, err := readShell(raw)
	if err != nil {
		return "", nil, err
	}
	body := newMap()
	if shell.display != "" && shell.display != shell.id {
		body.str("display_name", shell.display)
	}
	if shell.runWhen != "" && shell.runWhen != string(api.OnSuccess) {
		body.str("run_when", shell.runWhen)
	}
	if err = addKind(body, shell, node); err != nil {
		return "", nil, fmt.Errorf("node %q: %w", shell.id, err)
	}
	if err = addShellOutputs(body, shell); err != nil {
		return "", nil, fmt.Errorf("node %q: %w", shell.id, err)
	}
	checks, err := expectNode(shell.checks)
	if err != nil {
		return "", nil, fmt.Errorf("node %q: %w", shell.id, err)
	}
	body.raw("expect", checks)
	next, fail, err := edgeNames(shell.id, edges)
	if err != nil {
		return "", nil, fmt.Errorf("node %q: %w", shell.id, err)
	}
	body.raw("next", namesNode(next))
	body.raw("on_failure", namesNode(fail))
	return shell.id, body.node, nil
}

func readShell(raw []byte) (nodeShell, error) {
	var head struct {
		ID          string                   `json:"id"`
		DisplayName string                   `json:"display_name"`
		RunWhen     string                   `json:"run_when"`
		Type        string                   `json:"type"`
		Assertions  []api.CompositeAssertion `json:"assertions"`
		Outputs     []api.Output             `json:"outputs"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return nodeShell{}, err
	}
	if head.ID == "" || head.Type == "" {
		return nodeShell{}, fmt.Errorf("a node is missing an id or a type")
	}
	return nodeShell{
		id: head.ID, display: head.DisplayName, runWhen: head.RunWhen,
		kind: head.Type, checks: head.Assertions, outputs: head.Outputs,
	}, nil
}

func addKind(body *mapBuilder, shell nodeShell, node api.FlowNode) error {
	switch shell.kind {
	case kindRequest:
		return addRequest(body, node)
	case kindModule:
		return addModule(body, node)
	case kindDelay:
		return addDelay(body, node)
	case kindSet:
		return addSet(body, shell, node)
	case kindAssert:
		return addAssert(body, node)
	case kindBranch:
		return addBranch(body, node)
	case kindPoll:
		return addPoll(body, node)
	case kindLoop:
		return addLoop(body, node)
	case kindWebhook:
		return addWebhook(body, node)
	case kindSSE:
		return addSSE(body, node)
	default:
		return fmt.Errorf("unknown kind %q", shell.kind)
	}
}

func addShellOutputs(body *mapBuilder, shell nodeShell) error {
	if shell.kind == kindModule {
		if len(shell.outputs) > 0 {
			return fmt.Errorf("module outputs belong in output_bindings")
		}
		return nil
	}
	if shell.kind == kindSet {
		return nil
	}
	if len(shell.outputs) == 0 {
		return nil
	}
	if !outputsAllowed(shell.kind) {
		return fmt.Errorf("kind %s cannot declare outputs", shell.kind)
	}
	rendered, err := outputsNode(shell.outputs)
	if err != nil {
		return err
	}
	body.raw("outputs", rendered)
	return nil
}

func addRequest(body *mapBuilder, node api.FlowNode) error {
	spec, err := node.AsRequestFlowNode()
	if err != nil {
		return err
	}
	req := newMap()
	req.str("method", string(spec.Data.Method))
	req.str("url", spec.Data.Url)
	if spec.Data.Headers != nil {
		req.raw("headers", stringMapNode(*spec.Data.Headers))
	}
	if spec.Data.QueryParams != nil {
		query, err := anyMapNode(*spec.Data.QueryParams)
		if err != nil {
			return err
		}
		req.raw("query", query)
	}
	if spec.Data.Body != nil {
		rendered, err := valueNode(spec.Data.Body)
		if err != nil {
			return err
		}
		req.raw("body", rendered)
	}
	addInt(req, "timeout_ms", spec.Data.Timeout)
	body.raw(kindRequest, req.node)
	return nil
}

func addModule(body *mapBuilder, node api.FlowNode) error {
	spec, err := node.AsModuleFlowNode()
	if err != nil {
		return err
	}
	body.str(kindModule, spec.Data.FlowId.String())
	if spec.Data.InputBindings != nil && len(*spec.Data.InputBindings) > 0 {
		inputs, err := anyMapNode(*spec.Data.InputBindings)
		if err != nil {
			return err
		}
		body.raw("inputs", inputs)
	}
	if spec.Data.OutputBindings != nil && len(*spec.Data.OutputBindings) > 0 {
		body.raw("outputs", bindingNode(*spec.Data.OutputBindings))
	}
	return nil
}

func addDelay(body *mapBuilder, node api.FlowNode) error {
	spec, err := node.AsDelayFlowNode()
	if err != nil {
		return err
	}
	body.raw(kindDelay, intNode(spec.Data.Duration))
	return nil
}

func addSet(body *mapBuilder, shell nodeShell, node api.FlowNode) error {
	spec, err := node.AsSetVariableFlowNode()
	if err != nil {
		return err
	}
	if err = setOutputsMirror(shell.outputs, spec.Data.Variables); err != nil {
		return err
	}
	if len(spec.Data.Variables) == 0 {
		return fmt.Errorf("set needs at least one variable")
	}
	rendered, err := anyMapNode(spec.Data.Variables)
	if err != nil {
		return err
	}
	body.raw("set", rendered)
	return nil
}

func setOutputsMirror(outputs []api.Output, vars map[string]any) error {
	if len(outputs) == 0 {
		return nil
	}
	names := map[string]bool{}
	for _, output := range outputs {
		names[output.Name] = true
	}
	if len(names) != len(vars) {
		return fmt.Errorf("set outputs do not match its variables")
	}
	for name := range vars {
		if !names[name] {
			return fmt.Errorf("set outputs do not match its variables")
		}
	}
	return nil
}

func addAssert(body *mapBuilder, node api.FlowNode) error {
	spec, err := node.AsAssertFlowNode()
	if err != nil {
		return err
	}
	assert := newMap()
	if spec.Data.Target != nil {
		target, err := valueNode(spec.Data.Target)
		if err != nil {
			return err
		}
		assert.raw("target", target)
	}
	body.raw(kindAssert, assert.node)
	return nil
}

func addBranch(body *mapBuilder, node api.FlowNode) error {
	spec, err := node.AsBranchFlowNode()
	if err != nil {
		return err
	}
	branch := newMap()
	if spec.Data.Target != nil {
		target, err := valueNode(spec.Data.Target)
		if err != nil {
			return err
		}
		branch.raw("target", target)
	}
	if len(spec.Data.Cases) > 0 {
		cases, err := branchCases(spec.Data.Cases)
		if err != nil {
			return err
		}
		branch.raw("cases", cases)
	}
	if spec.Data.Default != nil {
		branch.str("default", *spec.Data.Default)
	}
	body.raw(kindBranch, branch.node)
	return nil
}

func branchCases(cases []api.BranchCase) (*yaml.Node, error) {
	items := make([]*yaml.Node, 0, len(cases))
	for _, item := range cases {
		line, err := checkLine(item.When)
		if err != nil {
			return nil, err
		}
		one := newMap()
		one.str("when", line)
		one.str("target", item.Target)
		items = append(items, one.node)
	}
	return seqNode(items), nil
}

func addPoll(body *mapBuilder, node api.FlowNode) error {
	spec, err := node.AsPollFlowNode()
	if err != nil {
		return err
	}
	poll := newMap()
	addInt(poll, "max_attempts", spec.Data.MaxAttempts)
	addInt(poll, "interval_ms", spec.Data.IntervalMs)
	addInt(poll, "timeout_ms", spec.Data.TimeoutMs)
	inline, err := inlineBody(spec.Data.Body)
	if err != nil {
		return err
	}
	poll.raw("body", inline)
	body.raw(kindPoll, poll.node)
	return nil
}

func addLoop(body *mapBuilder, node api.FlowNode) error {
	spec, err := node.AsLoopFlowNode()
	if err != nil {
		return err
	}
	loop := newMap()
	items, err := valueNode(spec.Data.Items)
	if err != nil {
		return err
	}
	loop.raw("items", items)
	if spec.Data.ItemVar != nil {
		loop.str("item_var", *spec.Data.ItemVar)
	}
	if spec.Data.IndexVar != nil {
		loop.str("index_var", *spec.Data.IndexVar)
	}
	addInt(loop, "max_iterations", spec.Data.MaxIterations)
	if spec.Data.ContinueOnError != nil {
		loop.bool("continue_on_error", *spec.Data.ContinueOnError)
	}
	inline, err := inlineBody(spec.Data.Body)
	if err != nil {
		return err
	}
	loop.raw("body", inline)
	body.raw(kindLoop, loop.node)
	return nil
}

func addWebhook(body *mapBuilder, node api.FlowNode) error {
	spec, err := node.AsWebhookWaitFlowNode()
	if err != nil {
		return err
	}
	hook := newMap()
	addInt(hook, "timeout_ms", spec.Data.TimeoutMs)
	addInt(hook, "settle_ms", spec.Data.SettleMs)
	if spec.Data.Expect != nil {
		groups, err := hookExpect(*spec.Data.Expect)
		if err != nil {
			return err
		}
		hook.raw("expect", groups)
	}
	body.raw(kindWebhook, hook.node)
	return nil
}

func addSSE(body *mapBuilder, node api.FlowNode) error {
	spec, err := node.AsSseFlowNode()
	if err != nil {
		return err
	}
	sse := newMap()
	sse.str("url", spec.Data.Url)
	if spec.Data.Method != nil {
		sse.str("method", string(*spec.Data.Method))
	}
	if spec.Data.Headers != nil {
		sse.raw("headers", stringMapNode(*spec.Data.Headers))
	}
	addInt(sse, "timeout_ms", spec.Data.TimeoutMs)
	addInt(sse, "max_events", spec.Data.MaxEvents)
	if spec.Data.CompletionEvent != nil {
		sse.str("completion_event", *spec.Data.CompletionEvent)
	}
	if spec.Data.StopOnAssertionFailure != nil {
		sse.bool("stop_on_assertion_failure", *spec.Data.StopOnAssertionFailure)
	}
	body.raw(kindSSE, sse.node)
	return nil
}

func inlineBody(def api.InlineFlowDefinition) (*yaml.Node, error) {
	var edges []api.FlowEdge
	if def.Edges != nil {
		edges = *def.Edges
	}
	nodes, err := nodesMap(def.Nodes, edges)
	if err != nil {
		return nil, err
	}
	body := newMap()
	body.raw("nodes", nodes)
	return body.node, nil
}

func hookExpect(groups []api.WebhookExpectation) (*yaml.Node, error) {
	if len(groups) == 0 {
		return nil, nil
	}
	items := make([]*yaml.Node, 0, len(groups))
	for _, group := range groups {
		one := newMap()
		one.str("name", group.Name)
		addInt(one, "min", group.Min)
		addInt(one, "max", group.Max)
		checks, err := expectNode(group.Assertions)
		if err != nil {
			return nil, err
		}
		one.raw("checks", checks)
		items = append(items, one.node)
	}
	return seqNode(items), nil
}

func edgeNames(id string, edges []api.FlowEdge) ([]string, []string, error) {
	var next, fail []string
	for _, edge := range edges {
		if edge.Source != id {
			continue
		}
		switch edge.Type {
		case api.Success:
			next = append(next, edge.Target)
		case api.Failure:
			fail = append(fail, edge.Target)
		default:
			return nil, nil, fmt.Errorf("edge %q has type %q", edge.Id, edge.Type)
		}
	}
	return next, fail, nil
}

func expectNode(checks []api.CompositeAssertion) (*yaml.Node, error) {
	if len(checks) == 0 {
		return nil, nil
	}
	lines := make([]*yaml.Node, 0, len(checks))
	for _, check := range checks {
		line, err := checkLine(check)
		if err != nil {
			return nil, err
		}
		lines = append(lines, strNode(line))
	}
	if len(lines) == 1 {
		return lines[0], nil
	}
	return seqNode(lines), nil
}

func checkLine(check api.CompositeAssertion) (string, error) {
	target, err := checkTarget(check)
	if err != nil {
		return "", err
	}
	op := string(check.OperatorType)
	target = quoteTarget(target)
	if op == string(api.OperatorTypeBetween) {
		low, high, boundErr := betweenBounds(check.OperatorData["value"])
		if boundErr != nil {
			return "", boundErr
		}
		return target + " between " + low + " " + high, nil
	}
	if slices.Contains(valuelessOperators, op) {
		return target + " " + op, nil
	}
	text, err := expectText(check.OperatorData["value"])
	if err != nil {
		return "", err
	}
	if op == string(api.OperatorTypeEquals) && target == statusWord && statusNumber(text) {
		return "status: " + text, nil
	}
	if text == "" {
		return target + " " + op + " ", nil
	}
	return target + " " + op + " " + text, nil
}

func checkTarget(check api.CompositeAssertion) (string, error) {
	switch check.ExtractorType {
	case api.ExtractorTypeStatusCode:
		return statusWord, nil
	case api.ExtractorTypeJsonPath:
		path, _ := check.ExtractorData["path"].(string)
		if path == "" {
			return "", fmt.Errorf("json_path check has no usable path")
		}
		return path, nil
	case api.ExtractorTypeHeader:
		name, _ := check.ExtractorData["header_name"].(string)
		if name == "" {
			return "", fmt.Errorf("header check has no header name")
		}
		return "header:" + name, nil
	case api.ExtractorTypeQueryParam:
		name, _ := check.ExtractorData["param_name"].(string)
		if name == "" {
			return "", fmt.Errorf("query check has no parameter name")
		}
		return "query:" + name, nil
	case api.ExtractorTypeBody:
		return string(api.ExtractorTypeBody), nil
	default:
		return "", fmt.Errorf("extractor %q", check.ExtractorType)
	}
}

// quoteTarget quotes a JSONPath that contains a space, such as a filter
// expression. The check line otherwise splits on the first space.
func quoteTarget(target string) string {
	if strings.ContainsAny(target, " \t\r\n") {
		return strconv.Quote(target)
	}
	return target
}

func betweenBounds(value any) (string, string, error) {
	list, ok := value.([]any)
	if !ok || len(list) != 2 {
		return "", "", fmt.Errorf("between needs two bounds")
	}
	low, err := expectText(list[0])
	if err != nil {
		return "", "", err
	}
	high, err := expectText(list[1])
	if err != nil {
		return "", "", err
	}
	if strings.ContainsAny(low, " \t\n") || strings.ContainsAny(high, " \t\n") {
		return "", "", fmt.Errorf("between bounds must be single tokens")
	}
	return low, high, nil
}

func expectText(value any) (string, error) {
	switch typed := value.(type) {
	case string:
		return typed, nil
	case bool:
		return strconv.FormatBool(typed), nil
	case float64:
		return formatFloat(typed), nil
	case int:
		return strconv.Itoa(typed), nil
	default:
		return "", fmt.Errorf("check value %T is not text", value)
	}
}

func statusNumber(text string) bool {
	_, err := strconv.Atoi(text)
	return err == nil
}

func outputsNode(outputs []api.Output) (*yaml.Node, error) {
	out := newMap()
	for _, output := range outputs {
		rendered, err := oneOutput(output)
		if err != nil {
			return nil, err
		}
		out.raw(output.Name, rendered)
	}
	return out.node, nil
}

func oneOutput(output api.Output) (*yaml.Node, error) {
	secret := output.Secret != nil && *output.Secret
	switch output.Extractor.Type {
	case api.ExtractorTypeJsonPath:
		return pathOutput(output.Name, output.Extractor.Path, secret)
	case api.ExtractorTypeHeader:
		return headerOutput(output.Name, output.Extractor.HeaderName, secret)
	case api.ExtractorTypeStatusCode:
		if !secret {
			return strNode(statusWord), nil
		}
		out := newMap()
		out.bool(statusWord, true)
		out.bool("secret", true)
		return out.node, nil
	case api.ExtractorTypeBody:
		if secret {
			return nil, fmt.Errorf("output %q body cannot be secret", output.Name)
		}
		return strNode(string(api.ExtractorTypeBody)), nil
	default:
		return nil, fmt.Errorf("output %q extractor %q", output.Name, output.Extractor.Type)
	}
}

func pathOutput(name string, path *string, secret bool) (*yaml.Node, error) {
	if path == nil || *path == "" {
		return nil, fmt.Errorf("output %q has no path", name)
	}
	if !secret && strings.HasPrefix(*path, "$") {
		return strNode(*path), nil
	}
	out := newMap()
	out.str("path", *path)
	if secret {
		out.bool("secret", true)
	}
	return out.node, nil
}

func headerOutput(name string, header *string, secret bool) (*yaml.Node, error) {
	if header == nil || *header == "" {
		return nil, fmt.Errorf("output %q has no header", name)
	}
	if !secret {
		return strNode("header:" + *header), nil
	}
	out := newMap()
	out.str("header", *header)
	out.bool("secret", true)
	return out.node, nil
}

func bindingNode(bindings map[string]string) *yaml.Node {
	out := newMap()
	for _, key := range slices.Sorted(maps.Keys(bindings)) {
		out.str(key, bindings[key])
	}
	return out.node
}

func valueNode(value any) (*yaml.Node, error) {
	switch typed := value.(type) {
	case nil:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
	case string:
		return strNode(typed), nil
	case bool:
		return boolNode(typed), nil
	case float64:
		return floatNode(typed), nil
	case int:
		return intNode(typed), nil
	case json.Number:
		return numberNode(string(typed))
	case map[string]any:
		return anyMapNode(typed)
	case []any:
		return anySeqNode(typed)
	default:
		return nil, fmt.Errorf("cannot render %T", value)
	}
}

func anyMapNode(values map[string]any) (*yaml.Node, error) {
	// An empty object is a value. {} and a missing body are different requests.
	out := newMap()
	for _, key := range slices.Sorted(maps.Keys(values)) {
		child, err := valueNode(values[key])
		if err != nil {
			return nil, err
		}
		out.raw(key, child)
	}
	return out.node, nil
}

func anySeqNode(values []any) (*yaml.Node, error) {
	items := make([]*yaml.Node, 0, len(values))
	for _, value := range values {
		child, err := valueNode(value)
		if err != nil {
			return nil, err
		}
		items = append(items, child)
	}
	return seqNode(items), nil
}

func stringMapNode(values map[string]string) *yaml.Node {
	if len(values) == 0 {
		return nil
	}
	out := newMap()
	for _, key := range slices.Sorted(maps.Keys(values)) {
		out.str(key, values[key])
	}
	return out.node
}

func namesNode(names []string) *yaml.Node {
	if len(names) == 0 {
		return nil
	}
	if len(names) == 1 {
		return strNode(names[0])
	}
	return stringSeq(names)
}

func stringSeq(values []string) *yaml.Node {
	items := make([]*yaml.Node, len(values))
	for i, value := range values {
		items[i] = strNode(value)
	}
	return seqNode(items)
}

func numberNode(text string) (*yaml.Node, error) {
	if value, err := strconv.Atoi(text); err == nil {
		return intNode(value), nil
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return nil, err
	}
	return floatNode(value), nil
}

func formatFloat(value float64) string {
	if !math.IsInf(value, 0) && !math.IsNaN(value) && value == math.Trunc(value) &&
		value >= math.MinInt64 && value <= math.MaxInt64 {
		return strconv.FormatInt(int64(value), 10)
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func floatNode(value float64) *yaml.Node {
	if text := formatFloat(value); statusNumber(text) || !strings.Contains(text, ".") {
		if whole, err := strconv.Atoi(text); err == nil {
			return intNode(whole)
		}
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: formatFloat(value)}
}

type mapBuilder struct {
	node *yaml.Node
}

func newMap() *mapBuilder {
	return &mapBuilder{node: &yaml.Node{Kind: yaml.MappingNode}}
}

func (m *mapBuilder) raw(key string, value *yaml.Node) {
	if value == nil {
		return
	}
	m.node.Content = append(m.node.Content, strNode(key), value)
}

func (m *mapBuilder) str(key, value string) {
	m.raw(key, strNode(value))
}

func (m *mapBuilder) bool(key string, value bool) {
	m.raw(key, boolNode(value))
}

func addInt(m *mapBuilder, key string, value *int) {
	if value == nil {
		return
	}
	m.raw(key, intNode(*value))
}

func strNode(value string) *yaml.Node {
	node := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
	// A plain scalar drops trailing spaces and turns "200" or "true" into a
	// number or a bool. Quote whenever that could happen.
	if quoteString(value) {
		node.Style = yaml.DoubleQuotedStyle
	}
	return node
}

func quoteString(value string) bool {
	if value == "" || strings.TrimSpace(value) != value {
		return true
	}
	if strings.ContainsAny(value, "\n\r:#&*!%@`,{}[]|>'\"") {
		return true
	}
	switch strings.ToLower(value) {
	case "true", "false", "null", "yes", "no", "on", "off", "~":
		return true
	}
	if _, err := strconv.ParseFloat(value, 64); err == nil {
		return true
	}
	return false
}

func boolNode(value bool) *yaml.Node {
	text := "false"
	if value {
		text = "true"
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: text}
}

func intNode(value int) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(value)}
}

func seqNode(items []*yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.SequenceNode, Content: items}
}
