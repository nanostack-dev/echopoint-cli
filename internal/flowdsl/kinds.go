package flowdsl

import (
	"strings"

	"echopoint-cli/internal/api"

	"gopkg.in/yaml.v3"
)

func (g *graph) parseKind(node *dslNode, key string, body *yaml.Node) {
	switch canonicalKind(key) {
	case kindRequest:
		g.parseRequest(node, body)
	case kindModule:
		g.parseModule(node, body)
	case kindDelay:
		g.parseDelay(node, body)
	case kindSet:
		g.parseSet(node, body)
	case kindAssert:
		g.parseAssert(node, body)
	case kindBranch:
		g.parseBranch(node, body)
	case kindPoll:
		g.parsePoll(node, body)
	case kindLoop:
		g.parseLoop(node, body)
	case kindWebhook:
		g.parseWebhook(node, body)
	case kindSSE:
		g.parseSSE(node, body)
	default:
		g.add(Error, lineOf(body), "node %q: unknown kind %q", node.id, key)
	}
}

func (g *graph) kindMap(node *dslNode, body *yaml.Node) (ymap, bool) {
	m, err := mappingOf(body)
	if err != nil {
		g.add(Error, lineOf(body), "node %q: %s", node.id, err.Error())
		return ymap{}, false
	}
	return m, true
}

func (g *graph) parseRequest(node *dslNode, body *yaml.Node) {
	m, ok := g.kindMap(node, body)
	if !ok {
		return
	}
	method := g.takeMethod(node, m)
	g.checkMethod(node, method, m.keyLine("method"))
	spec := &reqSpec{method: method, url: requireString(g, node, m, "url")}
	spec.headers = g.takeHeaderPairs(node, m, "headers")
	if query, exists := m.take("query"); exists {
		spec.query = g.stringPairs(query)
	}
	if raw, exists := m.take("body"); exists {
		val, err := decodeAny(raw)
		if err != nil {
			g.add(Error, lineOf(raw), "node %q body: %s", node.id, err.Error())
		}
		spec.body = val
		spec.hasBody = true
	}
	spec.timeout = g.takePositive(node, m, "timeout_ms")
	g.rejectNodeFields(node.id, m)
	node.request = spec
}

func (g *graph) parseModule(node *dslNode, body *yaml.Node) {
	if text, ok := scalar(body); ok {
		ref := strings.TrimSpace(text)
		node.module = &modSpec{ref: ref, refLine: body.Line}
		if ref == "" {
			g.add(Error, body.Line, "node %q module needs a flow file or a flow id", node.id)
		}
		return
	}
	m, ok := g.kindMap(node, body)
	if !ok {
		return
	}
	refLine := m.keyLine("ref")
	ref := strings.TrimSpace(g.takeString(m, "ref"))
	node.module = &modSpec{ref: ref, refLine: refLine}
	if ref == "" {
		g.add(Error, refLine, "node %q module needs a ref", node.id)
	}
	g.rejectNodeFields(node.id, m)
}

func (g *graph) parseDelay(node *dslNode, body *yaml.Node) {
	if value, ok := asInt(body); ok {
		if value <= 0 {
			g.add(Error, body.Line, "node %q delay must be greater than 0", node.id)
			return
		}
		node.delayMS = value
		return
	}
	m, ok := g.kindMap(node, body)
	if !ok {
		return
	}
	_, present := m.val["duration_ms"]
	ms := g.takePositive(node, m, "duration_ms")
	if !present {
		g.add(Error, m.line, "node %q delay needs duration_ms", node.id)
	} else if ms != nil {
		node.delayMS = *ms
	}
	g.rejectNodeFields(node.id, m)
}

func (g *graph) parseSet(node *dslNode, body *yaml.Node) {
	if body.Kind != yaml.MappingNode {
		g.add(Error, lineOf(body), "node %q set must be a mapping", node.id)
		return
	}
	node.vars = g.stringPairs(body)
	if len(node.vars) == 0 {
		g.add(Error, lineOf(body), "node %q set needs at least one variable", node.id)
	}
}

func (g *graph) parseAssert(node *dslNode, body *yaml.Node) {
	m, ok := g.kindMap(node, body)
	if !ok {
		return
	}
	spec := &assertSpec{}
	if raw, exists := m.take("target"); exists {
		val, err := decodeAny(raw)
		if err != nil {
			g.add(Error, lineOf(raw), "node %q target: %s", node.id, err.Error())
		}
		spec.target = val
		spec.set = true
	}
	g.rejectNodeFields(node.id, m)
	node.assert = spec
}

func (g *graph) parseBranch(node *dslNode, body *yaml.Node) {
	m, ok := g.kindMap(node, body)
	if !ok {
		return
	}
	spec := &branchSpec{}
	if raw, exists := m.take("target"); exists {
		val, err := decodeAny(raw)
		if err != nil {
			g.add(Error, lineOf(raw), "node %q target: %s", node.id, err.Error())
		}
		spec.target = val
		spec.targetSet = true
	}
	if raw, exists := m.take("cases"); exists {
		spec.cases = g.parseCases(node, raw)
	}
	if raw, exists := m.take("default"); exists {
		text, isString := scalar(raw)
		text = strings.TrimSpace(text)
		spec.hasDefault = true
		spec.defaultTarget = text
		if !isString || text == "" {
			g.add(Error, lineOf(raw), "node %q default needs a node id", node.id)
		}
	}
	if len(spec.cases) == 0 && !spec.hasDefault {
		g.add(Error, m.line, "node %q branch needs a case or a default", node.id)
	}
	g.rejectNodeFields(node.id, m)
	node.branch = spec
}

func (g *graph) parseCases(node *dslNode, body *yaml.Node) []branchCase {
	if body.Kind != yaml.SequenceNode {
		g.add(Error, body.Line, "node %q cases must be a list", node.id)
		return nil
	}
	cases := make([]branchCase, 0, len(body.Content))
	for _, item := range body.Content {
		m, err := mappingOf(item)
		if err != nil {
			g.add(Error, lineOf(item), "node %q case: %s", node.id, err.Error())
			continue
		}
		when, ok := m.take("when")
		if !ok {
			g.add(Error, item.Line, "node %q case needs when", node.id)
			continue
		}
		check, err := parseExpectItem(when)
		if err != nil {
			g.add(Error, lineOf(when), "node %q case: %s", node.id, err.Error())
			continue
		}
		target := strings.TrimSpace(g.takeString(m, "target"))
		if target == "" {
			g.add(Error, m.keyLine("target"), "node %q case needs a target", node.id)
		}
		g.rejectNodeFields(node.id, m)
		cases = append(cases, branchCase{when: check, target: target, line: item.Line})
	}
	return cases
}

func (g *graph) parsePoll(node *dslNode, body *yaml.Node) {
	m, ok := g.kindMap(node, body)
	if !ok {
		return
	}
	spec := &pollSpec{
		maxAttempts: g.takePositive(node, m, "max_attempts"),
		interval:    g.takePositive(node, m, "interval_ms"),
		timeout:     g.takePositive(node, m, "timeout_ms"),
		body:        g.takeInline(node, m),
	}
	g.rejectNodeFields(node.id, m)
	node.poll = spec
}

func (g *graph) parseLoop(node *dslNode, body *yaml.Node) {
	m, ok := g.kindMap(node, body)
	if !ok {
		return
	}
	spec := &loopSpec{
		itemVar:  strings.TrimSpace(g.takeString(m, "item_var")),
		indexVar: strings.TrimSpace(g.takeString(m, "index_var")),
	}
	if raw, exists := m.take("items"); exists {
		val, err := decodeAny(raw)
		if err != nil {
			g.add(Error, lineOf(raw), "node %q items: %s", node.id, err.Error())
		}
		spec.items = val
	} else {
		g.add(Error, m.line, "node %q loop needs items", node.id)
	}
	spec.max = g.takeNonNegative(node, m, "max_iterations")
	if raw, exists := m.take("continue_on_error"); exists {
		value, good := asBool(raw)
		if !good {
			g.add(Error, lineOf(raw), "node %q continue_on_error must be true or false", node.id)
		}
		spec.continueOn = &value
	}
	spec.body = g.takeInline(node, m)
	if spec.body != nil {
		spec.body.injected = []string{orDefault(spec.itemVar, "item"), orDefault(spec.indexVar, "index")}
	}
	g.rejectNodeFields(node.id, m)
	node.loop = spec
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func (g *graph) takeNonNegative(node *dslNode, m ymap, key string) *int {
	n, ok := m.take(key)
	if !ok {
		return nil
	}
	value, good := asInt(n)
	if !good || value < 0 {
		g.add(Error, lineOf(n), "node %q %s must be zero or greater", node.id, key)
		return nil
	}
	return &value
}

func (g *graph) takeInline(node *dslNode, m ymap) *graph {
	if body, ok := m.take("body"); ok {
		return parseGraph(g.path, body, true)
	}
	_, hasNodes := m.val["nodes"]
	_, hasSteps := m.val["steps"]
	if !hasNodes && !hasSteps {
		g.add(Error, node.line, "node %q needs a body", node.id)
		return nil
	}
	child := &graph{path: g.path, inline: true, version: defaultFlowVersion, line: m.line}
	child.readTopology(m)
	if len(child.nodes) == 0 {
		child.add(Error, m.line, "flow has no nodes")
	}
	child.rejectDuplicateIDs()
	if child.fromSteps {
		linkSteps(child.nodes)
	}
	return child
}

func (g *graph) parseWebhook(node *dslNode, body *yaml.Node) {
	m, ok := g.kindMap(node, body)
	if !ok {
		return
	}
	spec := &hookSpec{
		timeout: g.takePositive(node, m, "timeout_ms"),
		settle:  g.takeNonNegative(node, m, "settle_ms"),
	}
	if raw, exists := m.take("expect"); exists {
		spec.groups = g.parseHookGroups(node, raw)
	}
	g.rejectNodeFields(node.id, m)
	node.hook = spec
}

func (g *graph) parseHookGroups(node *dslNode, body *yaml.Node) []hookGroup {
	if body.Kind != yaml.SequenceNode {
		g.add(Error, body.Line, "node %q expect must be a list of events", node.id)
		return nil
	}
	groups := make([]hookGroup, 0, len(body.Content))
	for _, item := range body.Content {
		m, err := mappingOf(item)
		if err != nil {
			g.add(Error, lineOf(item), "node %q expect: %s", node.id, err.Error())
			continue
		}
		group := hookGroup{name: strings.TrimSpace(g.takeString(m, "name"))}
		if group.name == "" {
			g.add(Error, m.line, "node %q expect needs a name", node.id)
		}
		group.min = g.takeNonNegative(node, m, "min")
		group.max = g.takeNonNegative(node, m, "max")
		if raw, exists := m.take("checks"); exists {
			checks, err := parseExpectNode(raw)
			if err != nil {
				g.add(Error, lineOf(raw), "node %q expect: %s", node.id, err.Error())
			}
			group.checks = checks
		}
		g.rejectNodeFields(node.id, m)
		groups = append(groups, group)
	}
	return groups
}

func (g *graph) parseSSE(node *dslNode, body *yaml.Node) {
	m, ok := g.kindMap(node, body)
	if !ok {
		return
	}
	method := g.optionalMethod(m)
	g.checkMethod(node, method, m.keyLine("method"))
	spec := &sseSpec{
		url:     requireString(g, node, m, "url"),
		method:  method,
		headers: g.takeHeaderPairs(node, m, "headers"),
		timeout: g.takePositive(node, m, "timeout_ms"),
	}
	spec.maxEvents = g.takePositive(node, m, "max_events")
	if raw, exists := m.take("completion_event"); exists {
		text, isString := scalar(raw)
		if !isString || strings.TrimSpace(text) == "" {
			g.add(Error, lineOf(raw), "node %q completion_event must be a string", node.id)
		}
		text = strings.TrimSpace(text)
		spec.completion = &text
	}
	if raw, exists := m.take("stop_on_assertion_failure"); exists {
		value, good := asBool(raw)
		if !good {
			g.add(Error, lineOf(raw), "node %q stop_on_assertion_failure must be true or false", node.id)
		}
		spec.stop = &value
	}
	g.rejectNodeFields(node.id, m)
	node.sse = spec
}

func (g *graph) optionalMethod(m ymap) string {
	if _, ok := m.val["method"]; !ok {
		return ""
	}
	return strings.TrimSpace(g.takeString(m, "method"))
}

func (g *graph) checkMethod(node *dslNode, method string, line int) {
	if method == "" {
		return
	}
	if !api.HttpMethod(strings.ToUpper(method)).Valid() {
		g.add(Error, line, "node %q method %q is not an HTTP method", node.id, method)
	}
}

func countTrue(flags ...bool) int {
	n := 0
	for _, on := range flags {
		if on {
			n++
		}
	}
	return n
}

func outputsAllowed(kind string) bool {
	switch kind {
	case kindRequest, kindModule, kindAssert, kindWebhook, kindSSE:
		return true
	default:
		return false
	}
}

func (g *graph) parseOutputs(node *dslNode, body *yaml.Node) []outDecl {
	if !outputsAllowed(node.kind) {
		g.add(Error, lineOf(body), "node %q does not declare outputs", node.id)
		return nil
	}
	m, err := mappingOf(body)
	if err != nil {
		g.add(Error, lineOf(body), "node %q outputs: %s", node.id, err.Error())
		return nil
	}
	out := make([]outDecl, 0, len(m.order))
	seen := map[string]bool{}
	for _, name := range m.order {
		if !validID(name) {
			g.add(Error, m.lines[name], "node %q output %q must be letters, digits, _ or -", node.id, name)
			continue
		}
		if seen[name] {
			g.add(Error, m.lines[name], "node %q output %q is declared twice", node.id, name)
			continue
		}
		seen[name] = true
		out = append(out, g.parseOneOutput(node, name, m.val[name]))
	}
	if node.kind == kindModule && node.module != nil {
		node.module.explicit = true
	}
	return out
}

func (g *graph) parseOneOutput(node *dslNode, name string, body *yaml.Node) outDecl {
	decl := outDecl{name: name, line: lineOf(body)}
	if node.kind == kindModule {
		return g.parseModuleOutput(node, decl, body)
	}
	if text, ok := scalar(body); ok {
		return g.parseOutputShorthand(node, decl, text)
	}
	m, err := mappingOf(body)
	if err != nil {
		g.add(Error, lineOf(body), "node %q output %q: %s", node.id, name, err.Error())
		return decl
	}
	if raw, ok := m.take("secret"); ok {
		value, good := asBool(raw)
		if !good {
			g.add(Error, lineOf(raw), "node %q output %q secret must be true or false", node.id, name)
		}
		decl.secret = value
	}
	path, hasPath := m.take("path")
	header, hasHeader := m.take("header")
	status, hasStatus := m.take(statusWord)
	switch {
	case countTrue(hasPath, hasHeader, hasStatus) > 1:
		g.add(Error, decl.line, "node %q output %q sets more than one of path, header, status", node.id, name)
	case hasPath:
		decl.extractor = string(api.ExtractorTypeJsonPath)
		decl.path = scalarText(g, node, name, path)
	case hasHeader:
		decl.extractor = string(api.ExtractorTypeHeader)
		decl.header = scalarText(g, node, name, header)
	case hasStatus:
		on, good := asBool(status)
		if !good || !on {
			g.add(Error, lineOf(status), "node %q output %q status must be true", node.id, name)
		}
		decl.extractor = string(api.ExtractorTypeStatusCode)
	default:
		g.add(Error, decl.line, "node %q output %q needs a path, a header, or status: true", node.id, name)
	}
	g.rejectNodeFields(node.id, m)
	return decl
}

func (g *graph) parseModuleOutput(node *dslNode, decl outDecl, body *yaml.Node) outDecl {
	text, ok := scalar(body)
	if !ok {
		g.add(
			Error,
			lineOf(body),
			"node %q output %q must be a child output like request.accessToken",
			node.id,
			decl.name,
		)
		return decl
	}
	decl.binding = strings.TrimSpace(text)
	return decl
}

func (g *graph) parseOutputShorthand(node *dslNode, decl outDecl, text string) outDecl {
	switch {
	case strings.HasPrefix(text, "$"):
		decl.extractor = string(api.ExtractorTypeJsonPath)
		decl.path = text
	case strings.HasPrefix(text, "header:") && len(text) > len("header:"):
		decl.extractor = string(api.ExtractorTypeHeader)
		decl.header = strings.TrimPrefix(text, "header:")
	case text == string(api.ExtractorTypeBody):
		decl.extractor = string(api.ExtractorTypeBody)
	case text == statusWord:
		decl.extractor = string(api.ExtractorTypeStatusCode)
	default:
		g.add(
			Error,
			decl.line,
			"node %q output %q needs a path ($.field), header:<name>, body, or status",
			node.id,
			decl.name,
		)
	}
	return decl
}

func scalarText(g *graph, node *dslNode, name string, body *yaml.Node) string {
	text, ok := scalar(body)
	if !ok {
		g.add(Error, lineOf(body), "node %q output %q must be a string", node.id, name)
		return ""
	}
	return text
}
