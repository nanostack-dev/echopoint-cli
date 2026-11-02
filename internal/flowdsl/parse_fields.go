package flowdsl

import (
	"regexp"
	"strings"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

var nodeIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func validID(id string) bool {
	return nodeIDPattern.MatchString(id)
}

func isFlowID(id string) bool {
	_, err := uuid.Parse(id)
	return err == nil
}

func cleanYAML(err error) string {
	msg := strings.TrimSpace(err.Error())
	msg = strings.TrimPrefix(msg, "yaml: unmarshal errors:\n")
	msg = strings.TrimPrefix(msg, "yaml: ")
	return strings.TrimSpace(msg)
}

func orLine(m ymap, key string) int {
	if line, ok := m.lines[key]; ok {
		return line
	}
	return m.line
}

func (g *graph) takeString(m ymap, key string) string {
	n, ok := m.take(key)
	if !ok {
		return ""
	}
	text, ok := scalar(n)
	if !ok {
		g.add(Error, lineOf(n), "%s must be a string", key)
		return ""
	}
	return text
}

func (g *graph) takeStringList(m ymap, key string) []string {
	n, ok := m.take(key)
	if !ok {
		return nil
	}
	items, err := stringList(n)
	if err != nil {
		g.add(Error, lineOf(n), "%s: %s", key, err.Error())
		return nil
	}
	return items
}

// takeNames reads one name or a list. The bool is false when the key was absent,
// so a step chain can tell "next was set, even to nothing" from "next was omitted".
func (g *graph) takeNames(m ymap, key string) ([]string, bool) {
	n, ok := m.take(key)
	if !ok {
		return nil, false
	}
	names, err := oneOrList(n)
	if err != nil {
		g.add(Error, lineOf(n), "%s: %s", key, err.Error())
		return nil, true
	}
	for _, name := range names {
		if !validID(name) {
			g.add(Error, lineOf(n), "%s %q must be letters, digits, _ or -", key, name)
		}
	}
	return names, true
}

func (g *graph) rejectUnknown(m ymap) {
	for _, key := range m.leftover() {
		g.add(Error, m.keyLine(key), "unknown field %q", key)
	}
}

func (g *graph) rejectNodeFields(id string, m ymap) {
	for _, key := range m.leftover() {
		g.add(Error, m.keyLine(key), "node %q: unknown field %q", id, key)
	}
}

func (g *graph) rejectDuplicateIDs() {
	seen := map[string]int{}
	for _, n := range g.nodes {
		if prev, ok := seen[n.id]; ok {
			g.add(Error, n.line, "duplicate node id %q (also on line %d)", n.id, prev)
			continue
		}
		seen[n.id] = n.line
	}
}

// linkSteps adds a success edge from each step to the next one. A step that
// sets next, and a branch, keep the edges the author wrote.
func linkSteps(nodes []*dslNode) {
	for i := 0; i+1 < len(nodes); i++ {
		step := nodes[i]
		if step.kind == kindBranch || step.nextSet {
			continue
		}
		step.next = []string{nodes[i+1].id}
		step.nextSet = true
	}
}

func (g *graph) stringPairs(n *yaml.Node) []pair {
	m, err := mappingOf(n)
	if err != nil {
		g.add(Error, lineOf(n), "%s", err.Error())
		return nil
	}
	return g.pairsFrom(m)
}

func (g *graph) pairsFrom(m ymap) []pair {
	out := make([]pair, 0, len(m.order))
	for _, key := range m.order {
		val, err := decodeAny(m.val[key])
		if err != nil {
			g.add(Error, m.lines[key], "%s: %s", key, err.Error())
			continue
		}
		out = append(out, pair{key: key, val: val})
	}
	return out
}

func (g *graph) takeHeaderPairs(node *dslNode, m ymap, key string) []pair {
	n, ok := m.take(key)
	if !ok {
		return nil
	}
	pairs := g.stringPairs(n)
	for _, item := range pairs {
		if _, isString := item.val.(string); !isString {
			g.add(Error, lineOf(n), "node %q header %q must be a string", node.id, item.key)
		}
	}
	return pairs
}

func (g *graph) takePositive(node *dslNode, m ymap, key string) *int {
	n, ok := m.take(key)
	if !ok {
		return nil
	}
	value, good := asInt(n)
	if !good || value <= 0 {
		g.add(Error, lineOf(n), "node %q %s must be a number greater than 0", node.id, key)
		return nil
	}
	return &value
}

func (g *graph) takeMethod(node *dslNode, m ymap) string {
	if _, ok := m.val["method"]; !ok {
		g.add(Error, m.line, "node %q needs a method", node.id)
		return ""
	}
	return strings.TrimSpace(g.takeString(m, "method"))
}

func requireString(g *graph, node *dslNode, m ymap, key string) string {
	line := m.keyLine(key)
	text := strings.TrimSpace(g.takeString(m, key))
	if text == "" {
		g.add(Error, line, "node %q needs %s", node.id, key)
	}
	return text
}
