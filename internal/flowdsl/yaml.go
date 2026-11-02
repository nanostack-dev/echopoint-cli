package flowdsl

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// ymap is one YAML mapping, with the order the author wrote.
type ymap struct {
	line  int
	order []string
	lines map[string]int
	val   map[string]*yaml.Node
}

func mappingOf(n *yaml.Node) (ymap, error) {
	if n == nil {
		return ymap{}, fmt.Errorf("expected a mapping")
	}
	if n.Kind != yaml.MappingNode {
		return ymap{}, fmt.Errorf("line %d: expected a mapping", n.Line)
	}
	m := ymap{line: n.Line, lines: map[string]int{}, val: map[string]*yaml.Node{}}
	for i := 0; i+1 < len(n.Content); i += 2 {
		key := n.Content[i]
		if _, ok := m.val[key.Value]; ok {
			return ymap{}, fmt.Errorf("line %d: duplicate key %q", key.Line, key.Value)
		}
		m.order = append(m.order, key.Value)
		m.lines[key.Value] = key.Line
		m.val[key.Value] = n.Content[i+1]
	}
	return m, nil
}

func (m ymap) take(key string) (*yaml.Node, bool) {
	n, ok := m.val[key]
	if ok {
		delete(m.val, key)
	}
	return n, ok
}

func (m ymap) leftover() []string {
	var keys []string
	for _, key := range m.order {
		if _, ok := m.val[key]; ok {
			keys = append(keys, key)
		}
	}
	return keys
}

func (m ymap) keyLine(key string) int {
	if line, ok := m.lines[key]; ok {
		return line
	}
	return m.line
}

func scalar(n *yaml.Node) (string, bool) {
	if n == nil || n.Kind != yaml.ScalarNode {
		return "", false
	}
	return n.Value, true
}

func decodeAny(n *yaml.Node) (any, error) {
	if n == nil {
		return nil, fmt.Errorf("missing value")
	}
	var v any
	if err := n.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

func asInt(n *yaml.Node) (int, bool) {
	var v int
	if n == nil || n.Decode(&v) != nil {
		return 0, false
	}
	return v, true
}

func asBool(n *yaml.Node) (bool, bool) {
	var v bool
	if n == nil || n.Decode(&v) != nil {
		return false, false
	}
	return v, true
}

func stringList(n *yaml.Node) ([]string, error) {
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("line %d: expected a list", lineOf(n))
	}
	out := make([]string, 0, len(n.Content))
	for _, item := range n.Content {
		text, ok := scalar(item)
		if !ok {
			return nil, fmt.Errorf("line %d: expected a string", item.Line)
		}
		out = append(out, text)
	}
	return out, nil
}

func oneOrList(n *yaml.Node) ([]string, error) {
	if n == nil {
		return nil, fmt.Errorf("expected a name or a list")
	}
	if text, ok := scalar(n); ok {
		if text == "" {
			return nil, fmt.Errorf("line %d: expected a name", n.Line)
		}
		return []string{text}, nil
	}
	return stringList(n)
}

func lineOf(n *yaml.Node) int {
	if n == nil {
		return 0
	}
	return n.Line
}
