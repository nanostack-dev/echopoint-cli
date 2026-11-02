package flowdsl

import (
	"fmt"
	"strings"

	"echopoint-cli/internal/api"

	"github.com/google/uuid"
)

func (g *graph) lower() (api.CreateFlowRequest, error) {
	nodes := make([]api.FlowNode, 0, len(g.nodes))
	for _, n := range g.nodes {
		one, err := lowerNode(n)
		if err != nil {
			return api.CreateFlowRequest{}, err
		}
		nodes = append(nodes, one)
	}
	auto := true
	version := g.version
	definition := api.FlowDefinition{
		Name:    g.name,
		Version: version,
		Nodes:   nodes,
		Edges:   lowerEdges(g.nodes),
	}
	request := api.CreateFlowRequest{
		Name:           g.name,
		AutoLayout:     &auto,
		FlowDefinition: definition,
		Version:        &version,
	}
	if g.description != "" {
		description := g.description
		request.Description = &description
		request.FlowDefinition.Description = &description
	}
	if g.role != "" {
		role := api.FlowRole(g.role)
		request.Role = &role
	}
	if len(g.tags) > 0 {
		tags := append([]string{}, g.tags...)
		request.Tags = &tags
	}
	if id := folderUUID(g.folder); id != nil {
		request.FolderId = id
	}
	return request, nil
}

func folderUUID(value string) *uuid.UUID {
	parsed, err := uuid.Parse(value)
	if err != nil {
		return nil
	}
	return &parsed
}

func lowerEdges(nodes []*dslNode) []api.FlowEdge {
	edges := make([]api.FlowEdge, 0)
	seen := map[string]bool{}
	for _, n := range nodes {
		for _, target := range n.next {
			addEdge(&edges, seen, n.id, target, api.Success)
		}
		addBranchEdges(&edges, seen, n)
		for _, target := range n.fail {
			addEdge(&edges, seen, n.id, target, api.Failure)
		}
	}
	return edges
}

func addBranchEdges(edges *[]api.FlowEdge, seen map[string]bool, n *dslNode) {
	if n.branch == nil {
		return
	}
	for _, item := range n.branch.cases {
		addEdge(edges, seen, n.id, item.target, api.Success)
	}
	if n.branch.hasDefault {
		addEdge(edges, seen, n.id, n.branch.defaultTarget, api.Success)
	}
}

func addEdge(edges *[]api.FlowEdge, seen map[string]bool, source, target string, kind api.FlowEdgeType) {
	if target == "" {
		return
	}
	key := source + "\x00" + target + "\x00" + string(kind)
	if seen[key] {
		return
	}
	seen[key] = true
	id := source + "-" + target
	if kind == api.Failure {
		id += "-failure"
	}
	*edges = append(*edges, api.FlowEdge{Id: id, Source: source, Target: target, Type: kind})
}

func lowerInline(g *graph) (api.InlineFlowDefinition, error) {
	nodes := make([]api.FlowNode, 0, len(g.nodes))
	for _, n := range g.nodes {
		one, err := lowerNode(n)
		if err != nil {
			return api.InlineFlowDefinition{}, err
		}
		nodes = append(nodes, one)
	}
	def := api.InlineFlowDefinition{Nodes: nodes}
	if edges := lowerEdges(g.nodes); len(edges) > 0 {
		def.Edges = &edges
	}
	return def, nil
}

func lowerNode(n *dslNode) (api.FlowNode, error) {
	switch n.kind {
	case kindRequest:
		return lowerRequest(n)
	case kindModule:
		return lowerModule(n)
	case kindDelay:
		return lowerDelay(n)
	case kindSet:
		return lowerSet(n)
	case kindAssert:
		return lowerAssert(n)
	case kindBranch:
		return lowerBranch(n)
	case kindPoll:
		return lowerPoll(n)
	case kindLoop:
		return lowerLoop(n)
	case kindWebhook:
		return lowerWebhook(n)
	case kindSSE:
		return lowerSSE(n)
	default:
		return api.FlowNode{}, fmt.Errorf("node %q has no kind", n.id)
	}
}

func baseAssertions(n *dslNode) *[]api.CompositeAssertion {
	if len(n.expects) == 0 {
		return nil
	}
	copied := append([]api.CompositeAssertion{}, n.expects...)
	return &copied
}

func runWhenOf(value string) *api.FlowNodeRunWhen {
	mode := api.OnSuccess
	if value == string(api.Always) {
		mode = api.Always
	}
	return &mode
}

func lowerRequest(n *dslNode) (api.FlowNode, error) {
	data := api.RequestNodeData{
		Method:      api.HttpMethod(strings.ToUpper(n.request.method)),
		Url:         n.request.url,
		Headers:     stringMap(n.request.headers),
		QueryParams: anyMap(n.request.query),
		Timeout:     n.request.timeout,
	}
	if n.request.hasBody {
		data.Body = n.request.body
	}
	var node api.FlowNode
	err := node.FromRequestFlowNode(api.RequestFlowNode{
		Id:          n.id,
		Type:        api.RequestFlowNodeTypeRequest,
		DisplayName: n.display,
		RunWhen:     runWhenOf(n.runWhen),
		Assertions:  baseAssertions(n),
		Outputs:     extractorOutputs(n.outputs),
		Data:        data,
	})
	return node, err
}

func lowerModule(n *dslNode) (api.FlowNode, error) {
	parsed, err := uuid.Parse(n.module.flowID)
	if err != nil {
		return api.FlowNode{}, fmt.Errorf("node %q has no flow id", n.id)
	}
	data := api.ModuleNodeData{FlowId: parsed}
	if inputs := anyMap(n.module.inputs); inputs != nil {
		data.InputBindings = inputs
	}
	if bindings := bindingMap(n.outputs); bindings != nil {
		data.OutputBindings = bindings
	}
	var node api.FlowNode
	err = node.FromModuleFlowNode(api.ModuleFlowNode{
		Id:          n.id,
		Type:        api.ModuleFlowNodeTypeModule,
		DisplayName: n.display,
		RunWhen:     runWhenOf(n.runWhen),
		Assertions:  baseAssertions(n),
		Data:        data,
	})
	return node, err
}

func lowerDelay(n *dslNode) (api.FlowNode, error) {
	var node api.FlowNode
	err := node.FromDelayFlowNode(api.DelayFlowNode{
		Id:          n.id,
		Type:        api.DelayFlowNodeTypeDelay,
		DisplayName: n.display,
		RunWhen:     runWhenOf(n.runWhen),
		Assertions:  baseAssertions(n),
		Data:        api.DelayNodeData{Duration: n.delayMS},
	})
	return node, err
}

func lowerSet(n *dslNode) (api.FlowNode, error) {
	var node api.FlowNode
	err := node.FromSetVariableFlowNode(api.SetVariableFlowNode{
		Id:          n.id,
		Type:        api.SetVariableFlowNodeTypeSetVariable,
		DisplayName: n.display,
		RunWhen:     runWhenOf(n.runWhen),
		Assertions:  baseAssertions(n),
		Data:        api.SetVariableNodeData{Variables: valueMap(n.vars)},
	})
	return node, err
}

func lowerAssert(n *dslNode) (api.FlowNode, error) {
	data := api.AssertNodeData{}
	if n.assert != nil && n.assert.set {
		data.Target = n.assert.target
	}
	var node api.FlowNode
	err := node.FromAssertFlowNode(api.AssertFlowNode{
		Id:          n.id,
		Type:        api.AssertFlowNodeTypeAssert,
		DisplayName: n.display,
		RunWhen:     runWhenOf(n.runWhen),
		Assertions:  baseAssertions(n),
		Outputs:     extractorOutputs(n.outputs),
		Data:        data,
	})
	return node, err
}

func lowerBranch(n *dslNode) (api.FlowNode, error) {
	data := api.BranchNodeData{Cases: make([]api.BranchCase, 0, len(n.branch.cases))}
	if n.branch.targetSet {
		data.Target = n.branch.target
	}
	for _, item := range n.branch.cases {
		data.Cases = append(data.Cases, api.BranchCase{Target: item.target, When: item.when})
	}
	if n.branch.hasDefault {
		target := n.branch.defaultTarget
		data.Default = &target
	}
	var node api.FlowNode
	err := node.FromBranchFlowNode(api.BranchFlowNode{
		Id:          n.id,
		Type:        api.BranchFlowNodeTypeBranch,
		DisplayName: n.display,
		RunWhen:     runWhenOf(n.runWhen),
		Assertions:  baseAssertions(n),
		Data:        data,
	})
	return node, err
}

func lowerPoll(n *dslNode) (api.FlowNode, error) {
	if n.poll.body == nil {
		return api.FlowNode{}, fmt.Errorf("node %q needs a body", n.id)
	}
	body, err := lowerInline(n.poll.body)
	if err != nil {
		return api.FlowNode{}, err
	}
	var node api.FlowNode
	err = node.FromPollFlowNode(api.PollFlowNode{
		Id:          n.id,
		Type:        api.PollFlowNodeTypePoll,
		DisplayName: n.display,
		RunWhen:     runWhenOf(n.runWhen),
		Assertions:  baseAssertions(n),
		Data: api.PollNodeData{
			Body:        body,
			MaxAttempts: n.poll.maxAttempts,
			IntervalMs:  n.poll.interval,
			TimeoutMs:   n.poll.timeout,
		},
	})
	return node, err
}

func lowerLoop(n *dslNode) (api.FlowNode, error) {
	if n.loop.body == nil {
		return api.FlowNode{}, fmt.Errorf("node %q needs a body", n.id)
	}
	body, err := lowerInline(n.loop.body)
	if err != nil {
		return api.FlowNode{}, err
	}
	var node api.FlowNode
	err = node.FromLoopFlowNode(api.LoopFlowNode{
		Id:          n.id,
		Type:        api.LoopFlowNodeTypeLoop,
		DisplayName: n.display,
		RunWhen:     runWhenOf(n.runWhen),
		Assertions:  baseAssertions(n),
		Data: api.LoopNodeData{
			Body:            body,
			Items:           n.loop.items,
			ItemVar:         strPtr(n.loop.itemVar),
			IndexVar:        strPtr(n.loop.indexVar),
			MaxIterations:   n.loop.max,
			ContinueOnError: n.loop.continueOn,
		},
	})
	return node, err
}

func lowerWebhook(n *dslNode) (api.FlowNode, error) {
	data := api.WebhookWaitNodeData{TimeoutMs: n.hook.timeout, SettleMs: n.hook.settle}
	if groups := hookGroups(n.hook.groups); groups != nil {
		data.Expect = groups
	}
	var node api.FlowNode
	err := node.FromWebhookWaitFlowNode(api.WebhookWaitFlowNode{
		Id:          n.id,
		Type:        api.WebhookWaitFlowNodeTypeWebhookWait,
		DisplayName: n.display,
		RunWhen:     runWhenOf(n.runWhen),
		Assertions:  baseAssertions(n),
		Outputs:     extractorOutputs(n.outputs),
		Data:        data,
	})
	return node, err
}

func hookGroups(groups []hookGroup) *[]api.WebhookExpectation {
	if len(groups) == 0 {
		return nil
	}
	out := make([]api.WebhookExpectation, 0, len(groups))
	for _, group := range groups {
		item := api.WebhookExpectation{Name: group.name, Min: group.min, Max: group.max}
		if len(group.checks) > 0 {
			item.Assertions = append([]api.CompositeAssertion{}, group.checks...)
		} else {
			item.Assertions = []api.CompositeAssertion{}
		}
		out = append(out, item)
	}
	return &out
}

func lowerSSE(n *dslNode) (api.FlowNode, error) {
	data := api.SseNodeData{
		Url:                    n.sse.url,
		Headers:                stringMap(n.sse.headers),
		TimeoutMs:              n.sse.timeout,
		MaxEvents:              n.sse.maxEvents,
		CompletionEvent:        n.sse.completion,
		StopOnAssertionFailure: n.sse.stop,
	}
	if n.sse.method != "" {
		method := api.HttpMethod(strings.ToUpper(n.sse.method))
		data.Method = &method
	}
	var node api.FlowNode
	err := node.FromSseFlowNode(api.SseFlowNode{
		Id:          n.id,
		Type:        api.SseFlowNodeTypeSse,
		DisplayName: n.display,
		RunWhen:     runWhenOf(n.runWhen),
		Assertions:  baseAssertions(n),
		Outputs:     extractorOutputs(n.outputs),
		Data:        data,
	})
	return node, err
}

func extractorOutputs(decls []outDecl) *[]api.Output {
	if len(decls) == 0 {
		return nil
	}
	out := make([]api.Output, 0, len(decls))
	for _, decl := range decls {
		item := api.Output{Name: decl.name, Secret: secretPtr(decl.secret)}
		switch decl.extractor {
		case string(api.ExtractorTypeHeader):
			header := decl.header
			item.Extractor.Type = api.ExtractorTypeHeader
			item.Extractor.HeaderName = &header
		case string(api.ExtractorTypeBody):
			item.Extractor.Type = api.ExtractorTypeBody
		case string(api.ExtractorTypeStatusCode):
			item.Extractor.Type = api.ExtractorTypeStatusCode
		default:
			item.Extractor.Type = api.ExtractorTypeJsonPath
			path := decl.path
			item.Extractor.Path = &path
		}
		out = append(out, item)
	}
	return &out
}

func secretPtr(on bool) *bool {
	if !on {
		return nil
	}
	value := true
	return &value
}

func strPtr(value string) *string {
	if value == "" {
		return nil
	}
	text := value
	return &text
}

func stringMap(pairs []pair) *map[string]string {
	if len(pairs) == 0 {
		return nil
	}
	out := make(map[string]string, len(pairs))
	for _, item := range pairs {
		text, ok := item.val.(string)
		if !ok {
			continue
		}
		out[item.key] = text
	}
	return &out
}

func anyMap(pairs []pair) *map[string]any {
	if len(pairs) == 0 {
		return nil
	}
	out := make(map[string]any, len(pairs))
	for _, item := range pairs {
		out[item.key] = item.val
	}
	return &out
}

func valueMap(pairs []pair) map[string]any {
	out := make(map[string]any, len(pairs))
	for _, item := range pairs {
		out[item.key] = item.val
	}
	return out
}

func bindingMap(decls []outDecl) *map[string]string {
	if len(decls) == 0 {
		return nil
	}
	out := make(map[string]string, len(decls))
	for _, decl := range decls {
		if decl.binding == "" {
			continue
		}
		out[decl.name] = decl.binding
	}
	if len(out) == 0 {
		return nil
	}
	return &out
}
