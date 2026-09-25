package commands

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"echopoint-cli/internal/api"
)

const webhookWaitFlowJSON = `{
  "name": "webhook wait",
  "version": "1.0",
  "nodes": [{
    "id": "produce",
    "display_name": "Produce event",
    "type": "request",
    "data": {"method": "POST", "url": "{{webhook.url}}", "headers": {"Content-Type": "application/json"},
             "body": "{\"event\":\"order.created\"}"},
    "assertions": [{"extractor_type": "statusCode", "extractor_data": {}, "operator_type": "equals",
                    "operator_data": {"value": "200"}}]
  }, {
    "id": "wait-event",
    "display_name": "Wait for event",
    "type": "webhook_wait",
    "data": {"timeout_ms": 2000},
    "assertions": [{
      "extractor_type": "jsonPath",
      "extractor_data": {"path": "$.event"},
      "operator_type": "equals",
      "operator_data": {"value": "order.created"}
    }]
  }],
  "edges": []
}`

// A request node posts to the launch-injected {{webhook.url}}, and the
// webhook wait reads its run's requests with the token of the claimed Job. The token reaches the node through the run context, not
// through the flow inputs.
func TestIntegration_EphemeralWebhookWaitReadsWithTheJobToken(t *testing.T) {
	var srv *httptest.Server
	var authorizedReads, produced atomic.Int32
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/launch") && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(launchResponse(false))
		case strings.Contains(r.URL.Path, "/claim"):
			claim := fakeClaimResponse()
			var definition api.FlowDefinition
			if err := json.Unmarshal([]byte(webhookWaitFlowJSON), &definition); err != nil {
				t.Errorf("decode flow: %v", err)
			}
			claim.Job.FlowDefinition = definition
			claim.Job.Inputs = api.RunnerInputs{
				"webhook.url":          srv.URL + "/webhook/run-webhook-id",
				"webhook.requests_url": srv.URL + "/runner/jobs/" + claim.Job.JobId.String() + "/webhook-requests",
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(claim)
		case r.URL.Path == "/webhook/run-webhook-id":
			produced.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"message":"received"}`))
		case strings.HasSuffix(r.URL.Path, "/webhook-requests"):
			if r.Header.Get("X-Job-Token") != fakeClaimResponse().JobToken {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			authorizedReads.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{{
					"id":           "req-1",
					"method":       http.MethodPost,
					"headers":      map[string]string{"Content-Type": "application/json"},
					"query_params": map[string]string{},
					"body":         `{"event":"order.created"}`,
					"received_at":  time.Now().UTC().Format(time.RFC3339),
				}},
				"count": 1,
				"total": 1,
			})
		default:
			if !serveFakeJobRequest(w, r) {
				w.WriteHeader(http.StatusNotFound)
			}
		}
	}))
	defer srv.Close()

	state := makeState(t, "test-api-key", "", srv.URL)
	state.OrganizationID = "org_test"
	results, exitCode := executeFlows(
		context.Background(), state, []string{flowUUID().String()}, "", "", "", 1, "",
	)

	if exitCode != exitSuccess || len(results) != 1 || results[0].ExitCode != exitSuccess {
		t.Fatalf("the wait must match through the job token: code=%d results=%+v", exitCode, results)
	}
	if produced.Load() == 0 {
		t.Fatal("the request node never posted to {{webhook.url}}")
	}
	if authorizedReads.Load() == 0 {
		t.Fatal("the wait node never read its run's requests with the job token")
	}
}
