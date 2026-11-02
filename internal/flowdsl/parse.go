package flowdsl

import (
	"strings"

	"gopkg.in/yaml.v3"
)

func parseSource(path string, src []byte) *graph {
	g := &graph{path: path, version: defaultFlowVersion}
	var doc yaml.Node
	if err := yaml.Unmarshal(src, &doc); err != nil {
		g.add(Error, 0, "YAML: %s", cleanYAML(err))
		return g
	}
	if len(doc.Content) == 0 {
		g.add(Error, 1, "file is empty")
		return g
	}
	return parseGraph(path, doc.Content[0], false)
}

func parseGraph(path string, n *yaml.Node, inline bool) *graph {
	g := &graph{path: path, inline: inline, version: defaultFlowVersion, line: lineOf(n)}
	m, err := mappingOf(n)
	if err != nil {
		g.add(Error, lineOf(n), "%s", err.Error())
		return g
	}
	if inline {
		g.readBody(m)
	} else {
		g.readFile(m)
	}
	g.rejectUnknown(m)
	if len(g.nodes) == 0 {
		g.add(Error, g.line, "flow has no nodes")
	}
	g.rejectDuplicateIDs()
	if g.fromSteps {
		linkSteps(g.nodes)
	}
	return g
}

func (g *graph) readFile(m ymap) {
	g.name = g.takeString(m, "name")
	g.description = g.takeString(m, "description")
	g.role = g.takeString(m, "role")
	if version := g.takeString(m, "version"); version != "" {
		g.version = version
	}
	g.tags = g.takeStringList(m, "tags")
	g.id = strings.TrimSpace(g.takeString(m, "id"))
	g.folderLine = m.keyLine("folder")
	g.folder = strings.TrimSpace(g.takeString(m, "folder"))
	g.inputs = g.takeStringList(m, "inputs")
	g.public = g.takeStringList(m, "outputs")
	if g.name == "" {
		g.add(Error, orLine(m, "name"), "name is required")
	}
	if g.role != "" && g.role != "standalone" && g.role != kindModule {
		g.add(Error, m.keyLine("role"), "role must be standalone or module")
	}
	if g.id != "" && !isFlowID(g.id) {
		g.add(Error, m.keyLine("id"), "id %q is not a flow id", g.id)
	}
	g.readTopology(m)
}

func (g *graph) readBody(m ymap) {
	for _, key := range []string{"name", "description", "role", "tags", "id", "folder", "inputs", "outputs", "version"} {
		if _, ok := m.val[key]; ok {
			g.add(Error, m.keyLine(key), "a poll or loop body only has nodes or steps")
			delete(m.val, key)
		}
	}
	g.readTopology(m)
}

func (g *graph) readTopology(m ymap) {
	_, hasNodes := m.val["nodes"]
	_, hasSteps := m.val["steps"]
	if hasNodes && hasSteps {
		g.add(Error, m.keyLine("steps"), "use nodes or steps, not both")
		delete(m.val, "nodes")
		delete(m.val, "steps")
		return
	}
	if node, ok := m.take("nodes"); ok {
		g.nodes = g.parseNodes(node)
	}
	if node, ok := m.take("steps"); ok {
		g.fromSteps = true
		g.nodes = g.parseSteps(node)
	}
}

func (g *graph) parseNodes(n *yaml.Node) []*dslNode {
	m, err := mappingOf(n)
	if err != nil {
		g.add(Error, lineOf(n), "nodes: %s", err.Error())
		return nil
	}
	nodes := make([]*dslNode, 0, len(m.order))
	for _, id := range m.order {
		body, err := mappingOf(m.val[id])
		if err != nil {
			g.add(Error, m.lines[id], "node %q: %s", id, err.Error())
			continue
		}
		nodes = append(nodes, g.parseNode(id, body))
	}
	return nodes
}

func (g *graph) parseSteps(n *yaml.Node) []*dslNode {
	if n.Kind != yaml.SequenceNode {
		g.add(Error, n.Line, "steps must be a list")
		return nil
	}
	nodes := make([]*dslNode, 0, len(n.Content))
	for _, item := range n.Content {
		body, err := mappingOf(item)
		if err != nil {
			g.add(Error, lineOf(item), "step: %s", err.Error())
			continue
		}
		idNode, ok := body.take("id")
		id, idOK := scalar(idNode)
		id = strings.TrimSpace(id)
		if !ok || !idOK || id == "" {
			g.add(Error, item.Line, "a step needs an id")
			continue
		}
		nodes = append(nodes, g.parseNode(id, body))
	}
	return nodes
}

func (g *graph) parseNode(id string, m ymap) *dslNode {
	node := &dslNode{id: id, line: m.line, display: id, runWhen: "on_success"}
	if !validID(id) {
		g.add(Error, m.line, "node id %q must be letters, digits, _ or -", id)
	}
	if text := g.takeString(m, "display_name"); text != "" {
		node.display = text
	}
	if text := g.takeString(m, "run_when"); text != "" {
		if text != "on_success" && text != "always" {
			g.add(Error, m.keyLine("run_when"), "node %q run_when must be on_success or always", id)
		} else {
			node.runWhen = text
		}
	}
	node.next, node.nextSet = g.takeNames(m, "next")
	node.fail, _ = g.takeNames(m, "on_failure")
	kind, kindNode, kinds := takenKind(m)
	switch kinds {
	case 0:
		g.add(Error, m.line, "node %q needs a kind: %s", id, strings.Join(kindKeys, ", "))
	case 1:
		node.kind = canonicalKind(kind)
		g.parseKind(node, kind, kindNode)
	default:
		g.add(Error, m.line, "node %q has more than one kind", id)
	}
	if out, ok := m.take("outputs"); ok {
		node.outputs = g.parseOutputs(node, out)
	}
	_, hadExpect := m.val["expect"]
	if exp, ok := m.take("expect"); ok {
		checks, err := parseExpectNode(exp)
		if err != nil {
			g.add(Error, exp.Line, "node %q: %s", id, err.Error())
		}
		node.expects = checks
	}
	if node.kind == kindAssert && !hadExpect {
		g.add(Error, node.line, "node %q assert needs an expect", id)
	}
	if in, ok := m.take("inputs"); ok {
		g.applyInputs(node, in)
	}
	for _, key := range m.leftover() {
		g.add(Error, m.keyLine(key), "node %q: unknown field %q", id, key)
		delete(m.val, key)
	}
	return node
}

func (g *graph) applyInputs(node *dslNode, raw *yaml.Node) {
	switch {
	case node.kind != kindModule:
		g.add(Error, raw.Line, "node %q inputs are only for a module", node.id)
	case node.module == nil:
		return
	case len(node.module.inputs) > 0:
		g.add(Error, raw.Line, "node %q sets inputs twice", node.id)
	default:
		node.module.inputs = g.stringPairs(raw)
	}
}

func takenKind(m ymap) (string, *yaml.Node, int) {
	var found string
	var node *yaml.Node
	count := 0
	for _, key := range kindKeys {
		body, ok := m.val[key]
		if !ok {
			continue
		}
		count++
		found, node = key, body
	}
	if count == 1 {
		delete(m.val, found)
		return found, node, 1
	}
	for _, key := range kindKeys {
		delete(m.val, key)
	}
	return "", nil, count
}
