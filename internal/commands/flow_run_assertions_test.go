package commands

import (
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/jobrunner"
	"github.com/nanostack-dev/echopoint-runner/pkg/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/redact"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

func TestJobNodeReportUsesRedactedSerializedAssertionFields(t *testing.T) {
	const secret = "private-assertion-value"
	errorMessage := "Expected " + secret
	original := &node.RequestExecutionResult{
		BaseExecutionResult: spi.BaseExecutionResult{
			NodeID: "step", DisplayName: "Step", NodeType: spi.KindRequest,
			ErrorMsg: &errorMessage,
			AssertionResults: []spi.AssertionResult{{
				Expected: secret, Actual: secret, Error: errorMessage,
			}},
		},
		DurationMs: 123,
	}
	masked := redact.New(map[string]any{"token": secret}, []string{"token"}).FlowResult(&spi.FlowExecutionResult{
		ExecutionResults: map[string]spi.AnyResult{"step": original},
	})
	report := buildRunResultFromJob("flow", "execution", jobrunner.Result{Status: statusCompleted, Execution: masked})
	if len(report.Nodes) != 1 {
		t.Fatalf("missing masked node: %+v", report)
	}
	got := report.Nodes[0]
	if got.Status != statusFailed || got.ErrorMsg == nil || *got.ErrorMsg != "Expected ***" || got.DurationMs == nil ||
		*got.DurationMs != 123 {
		t.Fatalf("serialized masked result fields were lost: %+v", got)
	}
	if len(got.Assertions) != 1 || got.Assertions[0].Expected != "***" || got.Assertions[0].Actual != "***" ||
		got.Assertions[0].Error != "Expected ***" {
		t.Fatalf("serialized masked assertions were lost: %+v", got.Assertions)
	}
	if original.AssertionResults[0].Actual != secret || *original.ErrorMsg != errorMessage {
		t.Fatal("report mutated the original execution")
	}
}

const (
	tEquals     = "equals"
	tStatusCode = "status_code"
	tJSONPath   = "json_path"
)

// runnerResultWith builds a payload mirroring the runner result shape
// (result.execution_results[nodeID].assertion_results).
func runnerResultWith(nodeID string, assertions []map[string]any) map[string]any {
	return map[string]any{
		"execution_results": map[string]any{
			nodeID: map[string]any{
				"node_id":           nodeID,
				"assertion_results": toAnySlice(assertions),
			},
		},
	}
}

func toAnySlice(items []map[string]any) []any {
	out := make([]any, len(items))
	for i, it := range items {
		out[i] = it
	}
	return out
}

func TestJobNodeReportIncludesAssertionOutcomes(t *testing.T) {
	result := runnerResultWith("ping", []map[string]any{
		{"index": 0, "extractor": tStatusCode, "operator": tEquals, "expected": "200", "actual": 200, "passed": true},
		{"index": 1, "extractor": tJSONPath, "operator": tEquals, "expected": "a", "actual": "b", "passed": false},
	})

	nodes := buildJobNodeReport(result)
	if len(nodes) != 1 || nodes[0].NodeID != "ping" {
		t.Fatalf("unexpected nodes: %+v", nodes)
	}
	got := nodes[0].Assertions
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

func TestJobNodeReportMissingResultsIsEmpty(t *testing.T) {
	for _, payload := range []map[string]any{nil, {}, {"execution_results": nil}} {
		if got := buildJobNodeReport(payload); got == nil || len(got) != 0 {
			t.Errorf("expected non-nil empty nodes, got %v", got)
		}
	}
}

func TestJobNodeReportPreservesTolerantLegacyFields(t *testing.T) {
	payload := map[string]any{"execution_results": map[string]any{
		"z-skipped": map[string]any{"skip_reason": false, "error_message": "failed"},
		"b-error":   map[string]any{"error_message": 42, "duration_ms": 12.9},
		"a-ok":      map[string]any{"error_message": nil, "display_name": "OK", "node_type": "request"},
		"null":      nil,
		"scalar":    "invalid legacy node",
	}}
	nodes := buildJobNodeReport(payload)
	if len(nodes) != 3 || nodes[0].NodeID != "a-ok" || nodes[1].NodeID != "b-error" || nodes[2].NodeID != "z-skipped" {
		t.Fatalf("unexpected ordered nodes: %+v", nodes)
	}
	if nodes[0].Status != statusCompleted || nodes[0].DisplayName != "OK" || nodes[0].NodeType != "request" {
		t.Fatalf("unexpected complete node: %+v", nodes[0])
	}
	if nodes[1].Status != statusFailed || nodes[1].ErrorMsg != nil || nodes[1].DurationMs == nil ||
		*nodes[1].DurationMs != 12 {
		t.Fatalf("non-string error presence or duration changed: %+v", nodes[1])
	}
	if nodes[2].Status != "skipped" || nodes[2].ErrorMsg == nil || *nodes[2].ErrorMsg != "failed" {
		t.Fatalf("skip precedence changed: %+v", nodes[2])
	}
}

func TestJobNodeReportOmitsMalformedAssertionsWithoutLosingNodes(t *testing.T) {
	valid := map[string]any{"index": 0, "passed": true}
	for _, assertions := range []any{nil, []any{}, "invalid", []any{valid, map[string]any{"index": "bad"}}} {
		payload := map[string]any{"execution_results": map[string]any{
			"step": map[string]any{"assertion_results": assertions},
		}}
		nodes := buildJobNodeReport(payload)
		if len(nodes) != 1 || nodes[0].Assertions != nil {
			t.Fatalf("malformed assertions changed node projection: %+v", nodes)
		}
	}
}
