package commands

import (
	"testing"
)

const (
	tEquals     = "equals"
	tStatusCode = "statusCode"
	tJSONPath   = "jsonPath"
)

// runnerResultWith builds an ephemeralResult mirroring the runner payload shape
// (result.execution_results[nodeID].assertion_results).
func runnerResultWith(nodeID string, assertions []map[string]interface{}) *ephemeralResult {
	return &ephemeralResult{
		Result: map[string]interface{}{
			"execution_results": map[string]interface{}{
				nodeID: map[string]interface{}{
					"node_id":           nodeID,
					"assertion_results": toAnySlice(assertions),
				},
			},
		},
	}
}

func toAnySlice(items []map[string]interface{}) []interface{} {
	out := make([]interface{}, len(items))
	for i, it := range items {
		out[i] = it
	}
	return out
}

func TestExtractAssertionsByNode(t *testing.T) {
	result := runnerResultWith("ping", []map[string]interface{}{
		{"index": 0, "extractor": tStatusCode, "operator": tEquals, "expected": "200", "actual": 200, "passed": true},
		{"index": 1, "extractor": tJSONPath, "operator": tEquals, "expected": "a", "actual": "b", "passed": false},
	})

	byNode := extractAssertionsByNode(result.Result)
	got := byNode["ping"]
	if len(got) != 2 {
		t.Fatalf("expected 2 assertions for ping, got %d", len(got))
	}
	if got[0].Extractor != tStatusCode || !got[0].Passed {
		t.Errorf("assertion 0 parsed wrong: %+v", got[0])
	}
	if got[1].Passed || got[1].Operator != tEquals {
		t.Errorf("assertion 1 parsed wrong: %+v", got[1])
	}
}

func TestExtractAssertionsByNode_NoResults(t *testing.T) {
	if got := extractAssertionsByNode(map[string]interface{}{}); got != nil {
		t.Errorf("expected nil for a payload without execution_results, got %v", got)
	}
}

func TestAttachAssertions_MatchesByNodeID(t *testing.T) {
	nodes := []FlowRunNode{{NodeID: "ping"}, {NodeID: "other"}}
	result := runnerResultWith("ping", []map[string]interface{}{
		{"index": 0, "extractor": tStatusCode, "operator": tEquals, "expected": "200", "actual": 200, "passed": true},
	})

	attachAssertions(nodes, result)

	if len(nodes[0].Assertions) != 1 {
		t.Errorf("expected ping to receive 1 assertion, got %d", len(nodes[0].Assertions))
	}
	if nodes[1].Assertions != nil {
		t.Errorf("expected other to receive no assertions, got %+v", nodes[1].Assertions)
	}
}

func TestAttachAssertions_NilRunnerResultIsSafe(t *testing.T) {
	nodes := []FlowRunNode{{NodeID: "ping"}}
	attachAssertions(nodes, nil)
	if nodes[0].Assertions != nil {
		t.Error("nil runner result must not populate assertions")
	}
}
