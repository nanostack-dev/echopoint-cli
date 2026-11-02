package flowdsl_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/flowdsl"
)

// libraryJSON is a small stored flow with the shapes the production library
// actually uses: a status output, a between pair, a bool equals, a multiline
// value, a trailing space, fan-out, a delay, a set, a webhook group, a module
// binding, a poll body, and an SSE call.
const libraryJSON = `{
  "id": "33333333-3333-4333-8333-333333333333",
  "name": "library shapes",
  "description": "round trip",
  "role": "standalone",
  "tags": ["echopoint"],
  "version": "1.0",
  "folder_id": "22222222-2222-4222-8222-222222222222",
  "flow_definition": {
    "name": "old name",
    "version": "",
    "edges": [
      {"id": "e1", "source": "login", "target": "list", "type": "success"},
      {"id": "e2", "source": "list", "target": "pause", "type": "success", "source_handle": "right", "target_handle": "left"},
      {"id": "e3", "source": "list", "target": "mark", "type": "success"}
    ],
    "nodes": [
      {
        "id": "login", "type": "module", "display_name": "Login", "run_when": "on_success",
        "data": {
          "flow_id": "11111111-1111-4111-8111-111111111111",
          "input_bindings": {"email": "{{email}}"},
          "output_bindings": {"accessToken": "request.accessToken"}
        }
      },
      {
        "id": "list", "type": "request", "display_name": "List", "run_when": "on_success",
        "data": {
          "method": "GET",
          "url": "{{base}}/licenses",
          "headers": {"Authorization": "Bearer {{login.accessToken}}"},
          "query_params": {"view": "full"},
          "timeout": 1500
        },
        "assertions": [
          {"extractor_type": "status_code", "extractor_data": {}, "operator_type": "equals", "operator_data": {"value": "200"}},
          {"extractor_type": "status_code", "extractor_data": {}, "operator_type": "between", "operator_data": {"value": [200, 299]}},
          {"extractor_type": "json_path", "extractor_data": {"path": "$.custom"}, "operator_type": "equals", "operator_data": {"value": true}},
          {"extractor_type": "json_path", "extractor_data": {"path": "$.note"}, "operator_type": "equals", "operator_data": {"value": "line1\nline2"}},
          {"extractor_type": "json_path", "extractor_data": {"path": "$.name"}, "operator_type": "starts_with", "operator_data": {"value": "Ada "}}
        ],
        "outputs": [
          {"name": "status", "secret": false, "extractor": {"type": "status_code"}},
          {"name": "state", "secret": false, "extractor": {"type": "json_path", "path": "$.state"}},
          {"name": "trace", "secret": true, "extractor": {"type": "header", "header_name": "X-Trace"}}
        ]
      },
      {
        "id": "pause", "type": "delay", "display_name": "Pause", "run_when": "on_success",
        "data": {"duration": 2000}
      },
      {
        "id": "mark", "type": "set_variable", "display_name": "Mark", "run_when": "always",
        "data": {"variables": {"failed": false, "note": "x"}},
        "outputs": [{"name": "failed"}, {"name": "note"}]
      },
      {
        "id": "events", "type": "webhook_wait", "display_name": "Events", "run_when": "on_success",
        "data": {
          "timeout_ms": 1000,
          "settle_ms": 0,
          "expect": [{
            "name": "never",
            "min": 0,
            "max": 0,
            "assertions": [{
              "extractor_type": "json_path", "extractor_data": {"path": "$.type"},
              "operator_type": "equals", "operator_data": {"value": "ping"}
            }]
          }]
        }
      },
      {
        "id": "stream", "type": "sse", "display_name": "Stream", "run_when": "on_success",
        "data": {
          "url": "{{base}}/stream",
          "method": "GET",
          "max_events": 3,
          "timeout_ms": 1000,
          "stop_on_assertion_failure": true,
          "completion_event": "done"
        }
      },
      {
        "id": "wait", "type": "poll", "display_name": "Wait", "run_when": "on_success",
        "assertions": [{
          "extractor_type": "json_path", "extractor_data": {"path": "$[\"probe.state\"]"},
          "operator_type": "equals", "operator_data": {"value": "ready"}
        }],
        "data": {
          "max_attempts": 5, "interval_ms": 1000, "timeout_ms": 20000,
          "body": {
            "nodes": [{
              "id": "probe", "type": "request", "display_name": "Probe", "run_when": "on_success",
              "data": {"method": "GET", "url": "{{base}}/licenses/{{licenseId}}"},
              "outputs": [{"name": "state", "extractor": {"type": "json_path", "path": "$.state"}}]
            }],
            "edges": []
          }
        }
      },
      {
        "id": "each", "type": "loop", "display_name": "Each", "run_when": "on_success",
        "data": {
          "items": ["a", "b"],
          "item_var": "item",
          "index_var": "index",
          "max_iterations": 2,
          "continue_on_error": true,
          "body": {
            "nodes": [{
              "id": "one", "type": "request", "display_name": "One", "run_when": "on_success",
              "data": {"method": "POST", "url": "{{base}}/items", "body": {"id": "{{item}}"}},
              "assertions": []
            }]
          }
        }
      }
    ]
  }
}`

func TestDecompileRoundTripsLibraryShapes(t *testing.T) {
	assertRoundTrip(t, []byte(libraryJSON))
}

func TestBetweenCompilesToTheRunnerPair(t *testing.T) {
	root := t.TempDir()
	writeFlow(t, root, "range.flow.yaml", `
name: range
nodes:
  call:
    request:
      method: GET
      url: "{{base}}/x"
    expect:
      - "status between 200 299"
      - "$.n between 1 5"
    outputs:
      status: status
      code:
        status: true
        secret: true
`)
	out, err := flowdsl.CompileDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Errors()) != 0 {
		t.Fatal(joinDiags(out.Errors()))
	}
	raw, err := json.Marshal(out.Flows[0].Request)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	node := findByID(t, array(t, object(t, doc, "flow_definition"), "nodes"), "call")
	checks := node["assertions"].([]any)
	between := checks[0].(map[string]any)
	if between["operator_data"].(map[string]any)["value"] == nil {
		t.Fatalf("between: %#v", between)
	}
	got, _ := json.Marshal(between["operator_data"])
	if string(got) != `{"value":[200,299]}` {
		t.Fatalf("operator_data: %s", got)
	}
	outputs := array(t, node, "outputs")
	if outputs[0]["extractor"].(map[string]any)["type"] != "status_code" {
		t.Fatalf("status output: %#v", outputs[0])
	}
	if outputs[1]["secret"] != true {
		t.Fatalf("secret status: %#v", outputs[1])
	}
}

func TestDecompileRoundTripsAFilterAndAnInlineBody(t *testing.T) {
	assertRoundTrip(t, []byte(`{
	  "name": "filter",
	  "role": "standalone",
	  "version": "1.0",
	  "flow_definition": {
	    "name": "filter",
	    "version": "1.0",
	    "nodes": [
	      {
	        "id": "get", "type": "request", "display_name": "Get", "run_when": "on_success",
	        "data": {"method": "GET", "url": "https://example.test/items"},
	        "assertions": [{
	          "extractor_type": "json_path",
	          "extractor_data": {"path": "$.items[?(@.name == 'a b')].id"},
	          "operator_type": "equals",
	          "operator_data": {"value": "1"}
	        }]
	      },
	      {
	        "id": "other", "type": "request", "display_name": "Other", "run_when": "on_success",
	        "data": {"method": "GET", "url": "https://example.test/other"}
	      }
	    ],
	    "edges": [
	      {"id": "late", "source": "other", "target": "get", "type": "success"},
	      {"id": "early", "source": "get", "target": "other", "type": "success"}
	    ]
	  }
	}`))
	assertRoundTrip(t, []byte(`{
	  "name": "wait",
	  "role": "standalone",
	  "version": "1.0",
	  "flow_definition": {
	    "name": "wait",
	    "version": "1.0",
	    "nodes": [{
	      "id": "poll", "type": "poll", "display_name": "Poll", "run_when": "on_success",
	      "data": {
	        "max_attempts": 2,
	        "interval_ms": 10,
	        "timeout_ms": 100,
	        "body": {
	          "name": "inner name",
	          "version": "9.9",
	          "nodes": [{
	            "id": "probe", "type": "request", "display_name": "Probe", "run_when": "on_success",
	            "data": {"method": "GET", "url": "https://example.test/probe"}
	          }],
	          "edges": []
	        }
	      }
	    }],
	    "edges": []
	  }
	}`))
	assertRoundTrip(t, []byte(`{
	  "name": "empty object",
	  "role": "standalone",
	  "version": "1.0",
	  "flow_definition": {
	    "name": "empty object",
	    "version": "1.0",
	    "nodes": [
	      {
	        "id": "post", "type": "request", "display_name": "Post", "run_when": "on_success",
	        "data": {"method": "POST", "url": "https://example.test/x", "body": {"positions": {}}},
	        "assertions": [{
	          "extractor_type": "json_path",
	          "extractor_data": {"path": "$.id"},
	          "operator_type": "not_empty",
	          "operator_data": {"value": ""}
	        }]
	      },
	      {
	        "id": "bare", "type": "request", "display_name": "Bare", "run_when": "on_success",
	        "data": {"method": "POST", "url": "https://example.test/y", "body": {}}
	      },
	      {
	        "id": "call", "type": "module", "display_name": "Call", "run_when": "on_success",
	        "data": {
	          "flow_id": "11111111-1111-4111-8111-111111111111",
	          "output_bindings": {}
	        }
	      }
	    ],
	    "edges": []
	  }
	}`))
}

func TestCorpusRoundTripsWhenPresent(t *testing.T) {
	root := os.Getenv("ECHOPOINT_FLOW_CORPUS")
	if root == "" {
		t.Skip("set ECHOPOINT_FLOW_CORPUS to a directory of stored flow JSON")
	}
	var failed int
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: %v", path, err)
			failed++
			return nil
		}
		if problems := roundTripProblems(t, raw); len(problems) > 0 {
			failed++
			t.Errorf("%s\n%s", filepath.Base(path), strings.Join(problems, "\n"))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if failed > 0 {
		t.Fatalf("%d flows did not round-trip", failed)
	}
}

func assertRoundTrip(t *testing.T, raw []byte) {
	t.Helper()
	if problems := roundTripProblems(t, raw); len(problems) > 0 {
		t.Fatal(strings.Join(problems, "\n"))
	}
}

func roundTripProblems(t *testing.T, raw []byte) []string {
	t.Helper()
	flow, err := storedFlow(raw)
	if err != nil {
		return []string{err.Error()}
	}
	file, err := flowdsl.Decompile(flow)
	if err != nil {
		return []string{"decompile: " + err.Error()}
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "flow.flow.yaml")
	if err := os.WriteFile(path, file, 0o644); err != nil {
		return []string{err.Error()}
	}
	out, err := flowdsl.CompileFile(path, dir)
	if err != nil {
		return []string{"compile: " + err.Error()}
	}
	if len(out.Errors()) > 0 {
		return []string{"compile: " + joinDiags(out.Errors())}
	}
	compiled, err := json.Marshal(out.Flows[0].Request)
	if err != nil {
		return []string{err.Error()}
	}
	return diffFlows(raw, compiled)
}

func storedFlow(raw []byte) (api.Flow, error) {
	var item api.FlowSearchItem
	if err := json.Unmarshal(raw, &item); err != nil {
		return api.Flow{}, err
	}
	if item.FlowDefinition != nil && item.Name != "" {
		return api.Flow{
			Name:           item.Name,
			Description:    item.Description,
			FlowDefinition: *item.FlowDefinition,
			FolderId:       item.FolderId,
			Id:             item.Id,
			Role:           item.Role,
			Tags:           item.Tags,
			Version:        item.Version,
		}, nil
	}
	var flow api.Flow
	if err := json.Unmarshal(raw, &flow); err != nil {
		return api.Flow{}, err
	}
	return flow, nil
}

func diffFlows(stored, compiled []byte) []string {
	var want, got map[string]any
	if err := json.Unmarshal(stored, &want); err != nil {
		return []string{err.Error()}
	}
	if err := json.Unmarshal(compiled, &got); err != nil {
		return []string{err.Error()}
	}
	alignCatalog(want)
	alignCatalog(got)
	normalizeDefinition(want["flow_definition"])
	normalizeDefinition(got["flow_definition"])
	var problems []string
	diffValue("flow", projectFlow(want), projectFlow(got), &problems)
	if len(problems) > 12 {
		problems = append(problems[:12], "... more differences")
	}
	return problems
}

// alignCatalog makes the comparison follow the file's one name and one
// description. Compile writes both into the catalog and the definition.
// Empty definition versions are the file default.
func alignCatalog(doc map[string]any) {
	def, _ := doc["flow_definition"].(map[string]any)
	if def == nil {
		return
	}
	name, _ := doc["name"].(string)
	if name != "" {
		def["name"] = name
	}
	description, has := doc["description"].(string)
	if !has || description == "" {
		if text, ok := def["description"].(string); ok && text != "" {
			doc["description"] = text
			description = text
		}
	}
	if description == "" {
		delete(def, "description")
	} else {
		def["description"] = description
	}
	version, _ := doc["version"].(string)
	if version == "" {
		version = "1.0"
		doc["version"] = version
	}
	if def["version"] == nil || def["version"] == "" {
		def["version"] = version
	}
	delete(doc, "auto_layout")
}

func projectFlow(doc map[string]any) map[string]any {
	out := map[string]any{
		"name":            doc["name"],
		"role":            doc["role"],
		"version":         doc["version"],
		"flow_definition": doc["flow_definition"],
	}
	if text, ok := doc["description"].(string); ok && text != "" {
		out["description"] = text
	}
	if tags, ok := doc["tags"].([]any); ok && len(tags) > 0 {
		out["tags"] = tags
	}
	if folder, ok := doc["folder_id"].(string); ok && folder != "" {
		out["folder_id"] = folder
	}
	return out
}

func normalizeDefinition(value any) {
	def, ok := value.(map[string]any)
	if !ok {
		return
	}
	normalizeEdges(def)
	nodes, _ := def["nodes"].([]any)
	for _, node := range nodes {
		normalizeNode(node)
	}
}

func normalizeNode(value any) {
	node, ok := value.(map[string]any)
	if !ok {
		return
	}
	if node["run_when"] == nil || node["run_when"] == "" {
		node["run_when"] = "on_success"
	}
	dropFalseSecret(node["outputs"])
	if node["type"] == "set_variable" && setOutputsMirror(node) {
		delete(node, "outputs")
	}
	dropEmpty(node, "assertions")
	dropEmpty(node, "outputs")
	stringifyChecks(node["assertions"])
	data, _ := node["data"].(map[string]any)
	if data == nil {
		return
	}
	dropEmpty(data, "headers")
	dropEmpty(data, "query_params")
	dropEmpty(data, "input_bindings")
	dropEmpty(data, "output_bindings")
	stringifyChecks(groupsOf(data["expect"]))
	if body, ok := data["body"].(map[string]any); ok && (node["type"] == "poll" || node["type"] == "loop") {
		// The file keeps the inline nodes. A name or version stored beside
		// them is not part of the create request.
		for key := range body {
			if key != "nodes" && key != "edges" {
				delete(body, key)
			}
		}
		normalizeDefinition(body)
	}
}

func groupsOf(value any) any {
	groups, ok := value.([]any)
	if !ok {
		return nil
	}
	var checks []any
	for _, group := range groups {
		item, _ := group.(map[string]any)
		if item == nil {
			continue
		}
		dropEmpty(item, "assertions")
		if list, ok := item["assertions"].([]any); ok {
			checks = append(checks, list...)
		}
	}
	return checks
}

func normalizeEdges(def map[string]any) {
	edges, _ := def["edges"].([]any)
	for _, edge := range edges {
		item, _ := edge.(map[string]any)
		if item == nil {
			continue
		}
		delete(item, "id")
		delete(item, "source_handle")
		delete(item, "target_handle")
	}
	sort.SliceStable(edges, func(i, j int) bool {
		return edgeKey(edges[i]) < edgeKey(edges[j])
	})
	dropEmpty(def, "edges")
}

func edgeKey(value any) string {
	item, _ := value.(map[string]any)
	if item == nil {
		return ""
	}
	source, _ := item["source"].(string)
	target, _ := item["target"].(string)
	kind, _ := item["type"].(string)
	return source + "\x00" + target + "\x00" + kind
}

func dropFalseSecret(value any) {
	list, ok := value.([]any)
	if !ok {
		return
	}
	for _, item := range list {
		output, _ := item.(map[string]any)
		if output != nil && output["secret"] == false {
			delete(output, "secret")
		}
	}
}

func setOutputsMirror(node map[string]any) bool {
	data, _ := node["data"].(map[string]any)
	vars, _ := data["variables"].(map[string]any)
	outputs, _ := node["outputs"].([]any)
	if len(outputs) == 0 {
		return false
	}
	names := map[string]bool{}
	for _, item := range outputs {
		output, _ := item.(map[string]any)
		if output == nil {
			return false
		}
		name, _ := output["name"].(string)
		names[name] = true
	}
	if len(names) != len(vars) {
		return false
	}
	for name := range vars {
		if !names[name] {
			return false
		}
	}
	return true
}

func dropEmpty(doc map[string]any, key string) {
	switch value := doc[key].(type) {
	case nil:
		delete(doc, key)
	case []any:
		if len(value) == 0 {
			delete(doc, key)
		}
	case map[string]any:
		if len(value) == 0 {
			delete(doc, key)
		}
	}
}

// stringifyChecks makes text-operator values text. The runner already compares
// those operators as text, and the one bool in the library is the only value
// whose JSON type changes.
func stringifyChecks(value any) {
	list, ok := value.([]any)
	if !ok {
		return
	}
	for _, item := range list {
		check, _ := item.(map[string]any)
		if check == nil {
			continue
		}
		data, _ := check["operator_data"].(map[string]any)
		if data != nil && valuelessOperator(check["operator_type"]) && data["value"] == "" {
			// empty and not_empty take no value. A stored "" is the same check.
			delete(data, "value")
		}
		if !textOperator(check["operator_type"]) || data == nil {
			continue
		}
		if _, ok := data["value"]; ok {
			data["value"] = textOf(data["value"])
		}
	}
}

func valuelessOperator(value any) bool {
	return value == "empty" || value == "not_empty"
}

func textOperator(value any) bool {
	switch value {
	case "equals", "not_equals", "contains", "not_contains", "starts_with", "ends_with", "regex":
		return true
	default:
		return false
	}
}

func textOf(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case bool:
		return strconv.FormatBool(typed)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		raw, _ := json.Marshal(typed)
		return string(raw)
	}
}

func diffValue(path string, want, got any, problems *[]string) {
	if len(*problems) > 12 {
		return
	}
	if reflect.DeepEqual(want, got) {
		return
	}
	wantMap, wantOK := want.(map[string]any)
	gotMap, gotOK := got.(map[string]any)
	if wantOK && gotOK {
		seen := map[string]bool{}
		for key := range wantMap {
			seen[key] = true
			diffValue(path+"."+key, wantMap[key], gotMap[key], problems)
		}
		for key := range gotMap {
			if !seen[key] {
				diffValue(path+"."+key, nil, gotMap[key], problems)
			}
		}
		return
	}
	wantList, wantListOK := want.([]any)
	gotList, gotListOK := got.([]any)
	if wantListOK && gotListOK {
		if len(wantList) != len(gotList) {
			*problems = append(
				*problems,
				path+": length "+strconv.Itoa(len(wantList))+" vs "+strconv.Itoa(len(gotList)),
			)
			return
		}
		for i := range wantList {
			diffValue(path+"["+strconv.Itoa(i)+"]", wantList[i], gotList[i], problems)
		}
		return
	}
	*problems = append(*problems, path+": "+preview(want)+" vs "+preview(got))
}

func preview(value any) string {
	switch typed := value.(type) {
	case nil:
		return "absent"
	case string:
		if len(typed) > 24 || strings.ContainsAny(typed, " \n\t") {
			return "string(len=" + strconv.Itoa(len(typed)) + ")"
		}
		return strconv.Quote(typed)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	case []any:
		return "list(len=" + strconv.Itoa(len(typed)) + ")"
	case map[string]any:
		return "object(" + strconv.Itoa(len(typed)) + " keys)"
	default:
		return reflect.TypeOf(value).String()
	}
}
