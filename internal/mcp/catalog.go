// Package mcp exposes echopoint API operations as Model Context Protocol tools.
//
// The tool catalog is derived entirely from the embedded OpenAPI spec: any
// operation annotated with `x-ai-tool: true` (and not `x-ai-danger: true`)
// becomes a tool. The operation's parameters and JSON request body are merged
// into a single flat input schema. There is no per-operation Go glue — adding a
// tool is a spec annotation, nothing more.
package mcp

import (
	"encoding/json"
	"fmt"
	"maps"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// paramLoc records where a tool argument is sent in the HTTP request.
type paramLoc string

const (
	locPath  paramLoc = "path"
	locQuery paramLoc = "query"
	locBody  paramLoc = "body"
)

const (
	extAITool   = "x-ai-tool"
	extAIDesc   = "x-ai-description"
	extAIDanger = "x-ai-danger"
)

// schemaTypeObject is the JSON Schema object type, repeated across generated
// tool schemas.
const schemaTypeObject = "object"

// toolDef is a single MCP tool derived from an annotated OpenAPI operation.
type toolDef struct {
	Name         string              // snake_case operationId
	Description  string              // x-ai-description, falling back to summary
	Method       string              // GET, POST, ...
	PathTemplate string              // e.g. /flows/{id}/launch
	InputSchema  json.RawMessage     // JSON Schema handed to the MCP client
	Locations    map[string]paramLoc // argument name -> request location
}

// buildCatalog parses the spec and returns every AI-exposed tool, sorted by name.
func buildCatalog(spec []byte) ([]toolDef, error) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(spec)
	if err != nil {
		return nil, fmt.Errorf("load openapi: %w", err)
	}

	var tools []toolDef
	for _, path := range doc.Paths.InMatchingOrder() {
		item := doc.Paths.Find(path)
		for method, op := range item.Operations() {
			if !truthy(op.Extensions[extAITool]) || present(op.Extensions[extAIDanger]) {
				continue
			}
			td, err := buildTool(method, path, item.Parameters, op)
			if err != nil {
				return nil, fmt.Errorf("%s %s: %w", method, path, err)
			}
			tools = append(tools, td)
		}
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools, nil
}

// buildTool merges an operation's path/query params and JSON body into one flat
// tool. Header and cookie params are intentionally ignored for v1. A name shared
// between a parameter and a body field is a hard error: the flat schema cannot
// represent it unambiguously.
func buildTool(method, path string, pathParams openapi3.Parameters, op *openapi3.Operation) (toolDef, error) {
	if op.OperationID == "" {
		return toolDef{}, fmt.Errorf("operation is missing operationId")
	}

	desc := stringExt(op.Extensions[extAIDesc])
	if desc == "" {
		desc = op.Summary
	}

	props := map[string]any{}
	required := []string{}
	locs := map[string]paramLoc{}

	// Path-item-level parameters apply to every operation; operation-level
	// parameters with the same name override them (OpenAPI §4.8.9.1).
	for _, ref := range mergeParams(pathParams, op.Parameters) {
		p := ref.Value
		if p == nil {
			continue
		}
		var loc paramLoc
		switch p.In {
		case "path":
			loc = locPath
		case "query":
			loc = locQuery
		default:
			continue // skip header/cookie params in v1
		}
		props[p.Name] = schemaToMap(p.Schema)
		locs[p.Name] = loc
		if p.Required {
			required = append(required, p.Name)
		}
	}

	if body := jsonBodySchema(op.RequestBody); body != nil {
		bodyProps, bodyRequired := effectiveObject(body)
		for name, ref := range bodyProps {
			if _, clash := props[name]; clash {
				return toolDef{}, fmt.Errorf("body field %q collides with a parameter", name)
			}
			props[name] = schemaToMap(ref)
			locs[name] = locBody
		}
		if op.RequestBody.Value.Required {
			required = append(required, bodyRequired...)
		}
	}

	schema := map[string]any{
		"type":       schemaTypeObject,
		"properties": props,
	}
	if len(required) > 0 {
		sort.Strings(required)
		schema["required"] = required
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		return toolDef{}, fmt.Errorf("marshal input schema: %w", err)
	}

	return toolDef{
		Name:         toSnake(op.OperationID),
		Description:  desc,
		Method:       method,
		PathTemplate: path,
		InputSchema:  raw,
		Locations:    locs,
	}, nil
}

// jsonBodySchema returns the resolved application/json request body schema, or
// nil when the operation has no JSON body.
func jsonBodySchema(rb *openapi3.RequestBodyRef) *openapi3.Schema {
	if rb == nil || rb.Value == nil {
		return nil
	}
	mt := rb.Value.Content.Get("application/json")
	if mt == nil || mt.Schema == nil {
		return nil
	}
	return mt.Schema.Value
}

// schemaToMap converts a resolved OpenAPI schema into a plain JSON Schema map,
// inlining $refs by always reading the resolved .Value. It covers the field set
// the annotated operations actually use; exotic constructs degrade gracefully to
// an unconstrained object.
func schemaToMap(ref *openapi3.SchemaRef) map[string]any {
	if ref == nil || ref.Value == nil {
		return map[string]any{}
	}
	s := ref.Value
	m := map[string]any{}

	if s.Type != nil {
		if ts := s.Type.Slice(); len(ts) == 1 {
			m["type"] = ts[0]
		} else if len(ts) > 1 {
			m["type"] = ts
		}
	}
	if s.Format != "" {
		m["format"] = s.Format
	}
	if s.Description != "" {
		m["description"] = s.Description
	}
	if len(s.Enum) > 0 {
		m["enum"] = s.Enum
	}
	if s.Default != nil {
		m["default"] = s.Default
	}
	if s.Items != nil {
		m["items"] = schemaToMap(s.Items)
	}
	props, required := effectiveObject(s)
	if len(props) > 0 {
		p := map[string]any{}
		for name, pref := range props {
			p[name] = schemaToMap(pref)
		}
		m["properties"] = p
	}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

// effectiveObject returns a schema's properties and required fields with any
// allOf members merged in (recursively). OpenAPI composes request/response
// objects with allOf (e.g. FlowSearchRequest = SearchRequest + extras); without
// flattening, the inherited fields would be invisible to the tool schema. Later
// members and the schema's own properties win on name conflicts.
func effectiveObject(s *openapi3.Schema) (openapi3.Schemas, []string) {
	props := openapi3.Schemas{}
	var required []string

	var merge func(sub *openapi3.Schema)
	merge = func(sub *openapi3.Schema) {
		if sub == nil {
			return
		}
		for _, ref := range sub.AllOf {
			if ref != nil {
				merge(ref.Value)
			}
		}
		maps.Copy(props, sub.Properties)
		required = append(required, sub.Required...)
	}
	merge(s)

	return props, required
}

// mergeParams returns base parameters overlaid with override; an override
// parameter sharing a name replaces the base one. Order is base-then-new.
func mergeParams(base, override openapi3.Parameters) openapi3.Parameters {
	idx := map[string]int{}
	merged := openapi3.Parameters{}
	add := func(ref *openapi3.ParameterRef) {
		if ref == nil || ref.Value == nil {
			return
		}
		if i, ok := idx[ref.Value.Name]; ok {
			merged[i] = ref
			return
		}
		idx[ref.Value.Name] = len(merged)
		merged = append(merged, ref)
	}
	for _, r := range base {
		add(r)
	}
	for _, r := range override {
		add(r)
	}
	return merged
}

func truthy(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x == "true"
	}
	return false
}

// present reports whether an extension is set in a way that marks it active:
// a bool true or any non-empty string. Used for x-ai-danger, whose value is a
// reason string explaining why an operation is excluded from the tool surface.
func present(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x != ""
	}
	return false
}

func stringExt(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// toSnake converts a camelCase operationId to snake_case, keeping acronym runs
// together (listAPIKeys -> list_api_keys, getMe -> get_me).
func toSnake(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		if r < 'A' || r > 'Z' {
			b.WriteRune(r)
			continue
		}
		if i > 0 && boundaryBefore(runes, i) {
			b.WriteByte('_')
		}
		b.WriteRune(r - 'A' + 'a')
	}
	return b.String()
}

// boundaryBefore reports whether an underscore belongs before the uppercase
// rune at index i: after a lowercase/digit, or at the end of an acronym run
// (the last capital before a lowercase, e.g. the "I" in "APIKey").
func boundaryBefore(runes []rune, i int) bool {
	prev := runes[i-1]
	var next rune
	if i+1 < len(runes) {
		next = runes[i+1]
	}
	switch {
	case prev >= 'a' && prev <= 'z', prev >= '0' && prev <= '9':
		return true
	case prev >= 'A' && prev <= 'Z' && next >= 'a' && next <= 'z':
		return true
	default:
		return false
	}
}
